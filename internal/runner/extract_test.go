package runner_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/musher-dev/musher-cli/internal/runner"
)

type entry struct {
	name     string
	typeflag byte
	body     string
	link     string
	mode     int64
}

func archiveOf(t *testing.T, entries ...entry) *bytes.Reader {
	t.Helper()

	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for _, e := range entries {
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}

		hdr := &tar.Header{Name: e.name, Typeflag: e.typeflag, Linkname: e.link, Mode: mode, Size: int64(len(e.body))}
		if e.typeflag != tar.TypeReg {
			hdr.Size = 0
		}

		if e.typeflag == tar.TypeXGlobalHeader {
			// What GitHub writes: the commit, as a PAX comment.
			hdr = &tar.Header{Name: e.name, Typeflag: e.typeflag, PAXRecords: map[string]string{"comment": "0123abc"}}
		}

		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}

		if e.typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	return bytes.NewReader(buf.Bytes())
}

func TestExtractUnpacksAGitHubTarball(t *testing.T) {
	t.Parallel()

	dest := filepath.Join(t.TempDir(), "src")

	err := runner.ExtractTarGz(archiveOf(t,
		entry{name: "pax_global_header", typeflag: tar.TypeXGlobalHeader},
		entry{name: "acme-web-0123/", typeflag: tar.TypeDir, mode: 0o755},
		entry{name: "acme-web-0123/bin/run", typeflag: tar.TypeReg, body: "#!/bin/sh\n", mode: 0o4755},
		entry{name: "acme-web-0123/docs/link", typeflag: tar.TypeSymlink, link: "../bin/run"},
		entry{name: "acme-web-0123/hard", typeflag: tar.TypeLink, link: "acme-web-0123/bin/run"},
		entry{name: "acme-web-0123/dev/null", typeflag: tar.TypeChar},
	), dest, runner.DefaultExtractLimits)
	if err != nil {
		t.Fatalf("ExtractTarGz: %v", err)
	}

	info, err := os.Stat(filepath.Join(dest, "bin", "run"))
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode()&os.ModeSetuid != 0 || info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v, want 0755 without setuid", info.Mode())
	}

	if target, err := os.Readlink(filepath.Join(dest, "docs", "link")); err != nil || target != "../bin/run" {
		t.Fatalf("symlink = %q, %v", target, err)
	}

	if data, err := os.ReadFile(filepath.Join(dest, "hard")); err != nil || string(data) != "#!/bin/sh\n" {
		t.Fatalf("hard link = %q, %v", data, err)
	}

	if _, err := os.Lstat(filepath.Join(dest, "dev", "null")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a device node was created")
	}
}

func TestExtractRefusesEveryEscape(t *testing.T) {
	t.Parallel()

	cases := map[string][]entry{
		"dot-dot path":        {{name: "top/../../etc/passwd", typeflag: tar.TypeReg, body: "x"}},
		"absolute path":       {{name: "/etc/passwd", typeflag: tar.TypeReg, body: "x"}},
		"absolute symlink":    {{name: "top/l", typeflag: tar.TypeSymlink, link: "/etc"}},
		"climbing symlink":    {{name: "top/a/l", typeflag: tar.TypeSymlink, link: "../../.."}},
		"hard link outside":   {{name: "top/h", typeflag: tar.TypeLink, link: "../outside"}},
		"write through a dir": {{name: "top/d", typeflag: tar.TypeSymlink, link: "."}, {name: "top/d/../../x", typeflag: tar.TypeReg}},
	}

	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			base := t.TempDir()

			err := runner.ExtractTarGz(archiveOf(t, entries...), filepath.Join(base, "src"), runner.DefaultExtractLimits)

			var unsafe *runner.UnsafeArchiveError
			if !errors.As(err, &unsafe) {
				t.Fatalf("err = %v, want an UnsafeArchiveError", err)
			}

			if _, err := os.Stat(filepath.Join(base, "x")); err == nil {
				t.Fatal("an entry was written outside the destination")
			}
		})
	}
}

func TestExtractHoldsTheLimits(t *testing.T) {
	t.Parallel()

	big := archiveOf(t, entry{name: "top/a", typeflag: tar.TypeReg, body: "0123456789"})

	err := runner.ExtractTarGz(big, t.TempDir(), runner.ExtractLimits{MaxBytes: 5, MaxEntries: 10})

	var unsafe *runner.UnsafeArchiveError
	if !errors.As(err, &unsafe) {
		t.Fatalf("err = %v, want the byte limit", err)
	}

	many := archiveOf(t, entry{name: "top/a", typeflag: tar.TypeDir}, entry{name: "top/b", typeflag: tar.TypeDir})
	if err := runner.ExtractTarGz(many, t.TempDir(), runner.ExtractLimits{MaxBytes: 5, MaxEntries: 1}); !errors.As(err, &unsafe) {
		t.Fatalf("err = %v, want the entry limit", err)
	}
}
