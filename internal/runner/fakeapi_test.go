package runner_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeAPI is the runner surface, a GitHub tarball endpoint and presigned
// storage, in one httptest server. Each field shapes one answer; every
// request is recorded.
type fakeAPI struct {
	t   *testing.T
	srv *httptest.Server

	mu sync.Mutex

	// status overrides by "METHOD path-suffix" (e.g. "GET /assignment").
	status map[string]int

	// source snapshot
	tarball      []byte
	tarballCode  int
	maxArchive   int
	uploadCode   int
	uploaded     []byte
	uploadHeader http.Header
	fetchReports []map[string]any

	// image build
	recipe        map[string]any
	secrets       map[string]string
	archive       []byte
	archiveDigest string
	deadline      time.Time
	grantTTL      time.Duration
	grantHost     string // default registry.musher.test
	grantPassword string // default push-pass-<n>, a new one per grant
	grants        int
	buildReports  []map[string]any
	logLines      []map[string]any
	logCalls      int
	logStatusAt   int // the log call (1-based) that answers logStatus
	logStatus     int

	tokens []string
}

const (
	snapshotID = "6b3f0f8e-5c1a-4d7e-9a5e-0d6c1c1f0a01"
	buildID    = "8c2d1e7a-3b4f-4a6e-8d9c-1e2f3a4b5c02"
	commit     = "0123456789abcdef0123456789abcdef01234567"
	// imageTag is platform-api's build-<uuid hex>; the runner pushes exactly
	// imageRepository:imageTag.
	imageTag = "build-8c2d1e7a3b4f4a6e8d9c1e2f3a4b5c02"
	attempt  = "mush_run_kid01.0123456789abcdef0123456789abcdef.1.4102444800.sig"
)

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()

	f := &fakeAPI{
		t:           t,
		status:      map[string]int{},
		tarballCode: http.StatusOK,
		maxArchive:  1 << 20,
		uploadCode:  http.StatusOK,
		recipe: map[string]any{
			"kind": "DOCKERFILE", "context": ".", "dockerfilePath": "Dockerfile",
			"builderImage": nil, "arguments": map[string]string{"A": "1"}, "secretIds": []string{"NPM_TOKEN"},
		},
		secrets:  map[string]string{"NPM_TOKEN": "s3cr3t-value"},
		deadline: time.Now().Add(10 * time.Minute),
		grantTTL: 10 * time.Minute,
	}

	f.tarball = tarGz(t, map[string]string{"repo-0123/Dockerfile": "FROM scratch\n", "repo-0123/app.txt": "hello"})
	f.setArchive(f.tarball)

	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)

	return f
}

func (f *fakeAPI) setArchive(b []byte) {
	sum := sha256.Sum256(b)
	f.archive = b
	f.archiveDigest = "sha256:" + hex.EncodeToString(sum[:])
}

func (f *fakeAPI) URL() string { return f.srv.URL }

func (f *fakeAPI) registryHost() string {
	if f.grantHost != "" {
		return f.grantHost
	}

	return "registry.musher.test"
}

func (f *fakeAPI) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	path := r.URL.Path

	switch {
	case strings.HasPrefix(path, "/github/"):
		f.serveTarball(w, r)

		return
	case path == "/storage/upload":
		f.serveUpload(w, r)

		return
	case path == "/storage/archive":
		_, _ = w.Write(f.archive)

		return
	}

	if !strings.HasPrefix(path, "/runner/v1/") {
		f.t.Errorf("runner mode called %s %s, outside /runner/v1", r.Method, path)
		http.NotFound(w, r)

		return
	}

	f.tokens = append(f.tokens, r.Header.Get("Authorization"))

	for key, code := range f.status {
		method, suffix, _ := strings.Cut(key, " ")
		if r.Method == method && strings.HasSuffix(path, suffix) {
			problem(w, code)

			return
		}
	}

	switch {
	case strings.HasSuffix(path, "/source-snapshots/"+snapshotID+"/assignment"):
		writeJSON(w, http.StatusOK, map[string]any{
			"snapshotId": snapshotID,
			"repository": map[string]any{"repositoryId": 42, "fullName": "acme/web", "cloneUrl": "https://github.com/acme/web.git"},
			"commitSha":  commit, "fetchToken": "ghs_fetchtoken", "fetchTokenExpiresAt": time.Now().Add(time.Hour),
			"maxArchiveBytes": f.maxArchive,
		})
	case strings.HasSuffix(path, ":openArchiveUpload"):
		var body struct {
			Sha256    string `json:"sha256"`
			SizeBytes int    `json:"sizeBytes"`
		}

		decode(f.t, r, &body)

		raw, _ := hex.DecodeString(body.Sha256)
		writeJSON(w, http.StatusOK, map[string]any{
			"uploadUrl": f.srv.URL + "/storage/upload", "method": "PUT", "expiresAt": time.Now().Add(time.Hour),
			"headers": map[string]string{
				"x-amz-checksum-sha256": base64.StdEncoding.EncodeToString(raw),
				"content-length":        strconv.Itoa(body.SizeBytes),
			},
		})
	case strings.HasSuffix(path, ":reportFetch"):
		var body map[string]any

		decode(f.t, r, &body)
		f.fetchReports = append(f.fetchReports, body)

		writeJSON(w, http.StatusOK, map[string]any{"snapshotId": snapshotID, "status": "FETCHED", "commitSha": commit, "contentDigest": nil})
	case strings.HasSuffix(path, "/image-builds/"+buildID+"/assignment"):
		writeJSON(w, http.StatusOK, map[string]any{
			"buildId": buildID, "recipe": f.recipe, "recipeDigest": "sha256:" + strings.Repeat("a", 64),
			"source": map[string]any{
				"snapshotId": snapshotID, "commitSha": commit, "archiveUrl": f.srv.URL + "/storage/archive",
				"archiveDigest": f.archiveDigest, "archiveSizeBytes": len(f.archive),
			},
			"secrets": f.secrets, "imageRepository": f.registryHost() + "/org-1/src-42", "imageTag": imageTag,
			"deadlineAt": f.deadline,
		})
	case strings.HasSuffix(path, ":grantPushAccess"):
		f.grants++

		password := f.grantPassword
		if password == "" {
			password = "push-pass-" + strconv.Itoa(f.grants)
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"registryHost": f.registryHost(), "repositoryPath": "org-1/src-42",
			"username": "build", "password": password,
			"expiresAt": time.Now().Add(f.grantTTL),
		})
	case strings.HasSuffix(path, "/log-lines"):
		f.logCalls++
		if f.logStatusAt != 0 && f.logCalls >= f.logStatusAt {
			problem(w, f.logStatus)

			return
		}

		var body struct {
			Lines []map[string]any `json:"lines"`
		}

		decode(f.t, r, &body)
		f.logLines = append(f.logLines, body.Lines...)
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": len(body.Lines), "truncated": false})
	case strings.HasSuffix(path, ":reportResult"):
		var body map[string]any

		decode(f.t, r, &body)
		f.buildReports = append(f.buildReports, body)

		writeJSON(w, http.StatusOK, map[string]any{"buildId": buildID, "status": "ATTESTING"})
	default:
		problem(w, http.StatusNotFound)
	}
}

func (f *fakeAPI) serveTarball(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer ghs_fetchtoken" {
		f.t.Errorf("tarball request carried %q, want the fetch token", r.Header.Get("Authorization"))
	}

	if f.tarballCode != http.StatusOK {
		w.WriteHeader(f.tarballCode)

		return
	}

	_, _ = w.Write(f.tarball)
}

func (f *fakeAPI) serveUpload(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "" {
		f.t.Errorf("the storage upload carried an Authorization header")
	}

	body, _ := io.ReadAll(r.Body)
	f.uploaded = body
	f.uploadHeader = r.Header.Clone()
	f.uploadHeader.Set("Content-Length", strconv.FormatInt(r.ContentLength, 10))
	w.WriteHeader(f.uploadCode)
}

func (f *fakeAPI) snapshot() fakeAPI {
	f.mu.Lock()
	defer f.mu.Unlock()

	return fakeAPI{
		fetchReports: append([]map[string]any(nil), f.fetchReports...),
		buildReports: append([]map[string]any(nil), f.buildReports...),
		logLines:     append([]map[string]any(nil), f.logLines...),
		uploaded:     f.uploaded,
		uploadHeader: f.uploadHeader,
		grants:       f.grants,
		tokens:       append([]string(nil), f.tokens...),
	}
}

func problem(w http.ResponseWriter, code int) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(code)
	encode(w, map[string]any{
		"type":  "/errors/" + strings.ToLower(strings.ReplaceAll(http.StatusText(code), " ", "-")),
		"title": http.StatusText(code), "status": code, "detail": "fake refusal", "traceId": "trace-1",
	})
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	encode(w, body)
}

func encode(w http.ResponseWriter, body any) {
	if err := json.NewEncoder(w).Encode(body); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func decode(t *testing.T, r *http.Request, into any) {
	t.Helper()

	if err := json.NewDecoder(r.Body).Decode(into); err != nil {
		t.Errorf("decode %s body: %v", r.URL.Path, err)
	}
}

// tarGz builds a gzipped tar of regular files.
func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}

		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}
