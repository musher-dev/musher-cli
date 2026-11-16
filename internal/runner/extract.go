package runner

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

// ExtractLimits bound what an archive may unpack to.
type ExtractLimits struct {
	// MaxBytes is the most file content written.
	MaxBytes int64
	// MaxEntries is the most entries read.
	MaxEntries int
}

// DefaultExtractLimits keep a decompression bomb from filling scratch.
var DefaultExtractLimits = ExtractLimits{MaxBytes: 8 << 30, MaxEntries: 500_000}

// UnsafeArchiveError is an archive entry that would write outside the
// destination, or exceed the limits. The source is at fault, not the runner.
type UnsafeArchiveError struct {
	Entry  string
	Reason string
}

// Error implements error.
func (e *UnsafeArchiveError) Error() string {
	return "source archive entry " + quote(e.Entry) + ": " + e.Reason
}

// ExtractTarGz unpacks a gzipped tar (a GitHub commit tarball) into dest,
// dropping the archive's single top-level directory.
//
// Every write goes through an os.Root opened on dest, so no entry, and no
// symlink an earlier entry made, can reach outside it. On top of that each
// entry is refused when its name is absolute or climbs out with "..", when a
// symlink's target is absolute or resolves outside dest, or when a hard link
// names a file outside it. Devices and FIFOs are skipped; ownership is never
// applied, and only the permission bits are kept, without setuid, setgid or
// sticky.
func ExtractTarGz(r io.Reader, dest string, limits ExtractLimits) error {
	unzipped, err := gzip.NewReader(r)
	if err != nil {
		return &UnsafeArchiveError{Entry: "", Reason: "not a gzip stream: " + err.Error()}
	}

	defer closeQuietly(unzipped)

	if mkErr := os.MkdirAll(dest, 0o750); mkErr != nil {
		return clierrors.Errorf("create %s: %w", dest, mkErr)
	}

	root, err := os.OpenRoot(dest)
	if err != nil {
		return clierrors.Errorf("open %s: %w", dest, err)
	}

	defer closeQuietly(root)

	unpacker := &extractor{root: root, limits: limits}
	reader := tar.NewReader(unzipped)

	for {
		hdr, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return &UnsafeArchiveError{Entry: "", Reason: "unreadable tar stream: " + err.Error()}
		}

		if err := unpacker.entry(hdr, reader); err != nil {
			return err
		}
	}
}

type extractor struct {
	root    *os.Root
	limits  ExtractLimits
	entries int
	written int64
}

func (x *extractor) entry(hdr *tar.Header, body io.Reader) error {
	switch hdr.Typeflag {
	case tar.TypeXGlobalHeader, tar.TypeXHeader, tar.TypeGNULongName, tar.TypeGNULongLink:
		return nil
	}

	x.entries++
	if x.entries > x.limits.MaxEntries {
		return &UnsafeArchiveError{Entry: hdr.Name, Reason: "the archive has more entries than the limit"}
	}

	name, err := stripTopLevel(hdr.Name)
	if err != nil {
		return err
	}

	if name == "" {
		return nil // the top-level directory itself
	}

	mode := fs.FileMode(hdr.Mode).Perm()

	switch hdr.Typeflag {
	case tar.TypeDir:
		if err := x.root.MkdirAll(name, mode|0o700); err != nil {
			return x.writeError(hdr.Name, err)
		}

		return nil
	case tar.TypeReg:
		return x.file(hdr, name, mode, body)
	case tar.TypeSymlink:
		return x.symlink(hdr, name)
	case tar.TypeLink:
		target, err := stripTopLevel(hdr.Linkname)
		if err != nil || target == "" {
			return &UnsafeArchiveError{Entry: hdr.Name, Reason: "a hard link to " + quote(hdr.Linkname) + ", outside the source"}
		}

		if err := x.parent(name); err != nil {
			return x.writeError(hdr.Name, err)
		}

		if err := x.root.Link(target, name); err != nil {
			return x.writeError(hdr.Name, err)
		}

		return nil
	default:
		// Devices, FIFOs and anything newer: never needed to build.
		return nil
	}
}

func (x *extractor) file(hdr *tar.Header, name string, mode fs.FileMode, body io.Reader) error {
	if hdr.Size > x.limits.MaxBytes-x.written {
		return &UnsafeArchiveError{Entry: hdr.Name, Reason: "the archive unpacks to more than the limit"}
	}

	if err := x.parent(name); err != nil {
		return x.writeError(hdr.Name, err)
	}

	f, err := x.root.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode|0o600)
	if err != nil {
		return x.writeError(hdr.Name, err)
	}

	n, copyErr := io.Copy(f, io.LimitReader(body, hdr.Size))
	closeErr := f.Close()
	x.written += n

	if copyErr != nil {
		return &UnsafeArchiveError{Entry: hdr.Name, Reason: "truncated: " + copyErr.Error()}
	}

	if closeErr != nil {
		return x.writeError(hdr.Name, closeErr)
	}

	return nil
}

func (x *extractor) symlink(hdr *tar.Header, name string) error {
	target := hdr.Linkname
	if target == "" || path.IsAbs(target) || strings.Contains(target, `\`) {
		return &UnsafeArchiveError{Entry: hdr.Name, Reason: "a symlink to " + quote(target) + ", outside the source"}
	}

	resolved := path.Join(path.Dir(name), target) //nolint:gosec // G305: resolved is checked to stay inside on the next line, and the write goes through os.Root
	if resolved == ".." || strings.HasPrefix(resolved, "../") {
		return &UnsafeArchiveError{Entry: hdr.Name, Reason: "a symlink to " + quote(target) + ", outside the source"}
	}

	if err := x.parent(name); err != nil {
		return x.writeError(hdr.Name, err)
	}

	if err := x.root.Symlink(target, name); err != nil {
		return x.writeError(hdr.Name, err)
	}

	return nil
}

func (x *extractor) parent(name string) error {
	dir := path.Dir(name)
	if dir == "." {
		return nil
	}

	return x.root.MkdirAll(dir, 0o750) //nolint:wrapcheck // wrapped by writeError
}

// writeError tells an escape the root refused from a plain I/O failure.
func (x *extractor) writeError(entry string, err error) error {
	if strings.Contains(err.Error(), "path escapes from parent") {
		return &UnsafeArchiveError{Entry: entry, Reason: "it resolves outside the source"}
	}

	return clierrors.Errorf("extract %s: %w", entry, err)
}

// stripTopLevel drops the first path element and refuses a name that is
// absolute or climbs out.
func stripTopLevel(name string) (string, error) {
	if path.IsAbs(name) || strings.Contains(name, `\`) {
		return "", &UnsafeArchiveError{Entry: name, Reason: "an absolute path"}
	}

	for elem := range strings.SplitSeq(name, "/") {
		if elem == ".." {
			return "", &UnsafeArchiveError{Entry: name, Reason: "a path that climbs out with .."}
		}
	}

	_, rest, _ := strings.Cut(strings.TrimPrefix(name, "./"), "/")
	rest = strings.Trim(path.Clean("/"+rest), "/")

	return rest, nil
}

func quote(s string) string {
	return `"` + s + `"`
}
