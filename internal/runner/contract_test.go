package runner_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/musher-dev/musher-cli/internal/runner"
	"github.com/musher-dev/musher-cli/internal/runner/api"
)

const vendorRoot = "../../contracts/vendor/platform-api"

type specOperation struct {
	OperationID string                `json:"operationId"`
	Security    []map[string][]string `json:"security"`
	RequestBody *struct {
		Content map[string]json.RawMessage `json:"content"`
	} `json:"requestBody"`
}

func loadSpec(t *testing.T) map[string]map[string]specOperation {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(vendorRoot, "contracts", "openapi", "runner.json"))
	if err != nil {
		t.Fatal(err)
	}

	var spec struct {
		Paths map[string]map[string]specOperation `json:"paths"`
	}

	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}

	return spec.Paths
}

// TestVendoredSpecMatchesItsPin proves the vendored runner.json is the
// pinned release's file, byte for byte.
func TestVendoredSpecMatchesItsPin(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join(vendorRoot, "vendored.json"))
	if err != nil {
		t.Fatal(err)
	}

	var pin struct {
		Version    string `json:"version"`
		Interfaces []struct {
			Files []struct {
				Path   string `json:"path"`
				SHA256 string `json:"sha256"`
			} `json:"files"`
		} `json:"interfaces"`
	}

	if err := json.Unmarshal(raw, &pin); err != nil {
		t.Fatal(err)
	}

	if pin.Version == "" || len(pin.Interfaces) == 0 {
		t.Fatal("vendored.json pins nothing")
	}

	for _, iface := range pin.Interfaces {
		for _, file := range iface.Files {
			data, err := os.ReadFile(filepath.Join(vendorRoot, file.Path))
			if err != nil {
				t.Fatal(err)
			}

			sum := sha256.Sum256(data)
			if got := hex.EncodeToString(sum[:]); got != file.SHA256 {
				t.Errorf("%s has sha256 %s, the pin says %s", file.Path, got, file.SHA256)
			}
		}
	}
}

// generatedRequests builds each operation's request with the generated
// client, so the test reads the method and path the client really sends.
var generatedRequests = map[runner.Operation]func(server string, id uuid.UUID) (*http.Request, error){
	runner.OpGetSourceSnapshotAssignment: api.NewRunnerGetSourceSnapshotAssignmentRequest,
	runner.OpOpenArchiveUpload: func(s string, id uuid.UUID) (*http.Request, error) {
		return api.NewRunnerOpenSourceSnapshotArchiveUploadRequest(s, id, api.OpenArchiveUploadRequest{})
	},
	runner.OpReportSourceSnapshotFetch: func(s string, id uuid.UUID) (*http.Request, error) {
		return api.NewRunnerReportSourceSnapshotFetchRequestWithBody(s, id, "application/json", strings.NewReader("{}"))
	},
	runner.OpGetImageBuildAssignment: api.NewRunnerGetImageBuildAssignmentRequest,
	runner.OpGrantPushAccess:         api.NewRunnerGrantImageBuildPushAccessRequest,
	runner.OpReportImageBuildResult: func(s string, id uuid.UUID) (*http.Request, error) {
		return api.NewRunnerReportImageBuildResultRequestWithBody(s, id, "application/json", strings.NewReader("{}"))
	},
	runner.OpCreateImageBuildLogLines: func(s string, id uuid.UUID) (*http.Request, error) {
		return api.NewRunnerCreateImageBuildLogLinesRequest(s, id, api.CreateBuildLogLinesRequest{})
	},
}

// TestEveryOperationMatchesTheSpec holds each runner operation to the
// vendored spec: it exists, it is a /runner/v1 route, it takes only the run
// attempt token, and the generated client sends its method and path.
func TestEveryOperationMatchesTheSpec(t *testing.T) {
	t.Parallel()

	paths := loadSpec(t)
	id := uuid.MustParse("6b3f0f8e-5c1a-4d7e-9a5e-0d6c1c1f0a01")
	param := regexp.MustCompile(`\{[a-z_]+\}`)

	if len(generatedRequests) != len(runner.Operations) {
		t.Fatalf("%d generated requests for %d operations", len(generatedRequests), len(runner.Operations))
	}

	for _, op := range runner.Operations {
		var (
			method, path string
			found        specOperation
		)

		for p, methods := range paths {
			for m, o := range methods {
				if o.OperationID == string(op) {
					method, path, found = strings.ToUpper(m), p, o
				}
			}
		}

		if path == "" {
			t.Errorf("%s is not in the vendored runner.json", op)

			continue
		}

		if !strings.HasPrefix(path, "/runner/v1/") {
			t.Errorf("%s is %s, outside /runner/v1", op, path)
		}

		if len(found.Security) != 1 || len(found.Security[0]) != 1 || found.Security[0]["RunAttemptToken"] == nil {
			t.Errorf("%s takes %v, want the RunAttemptToken alone", op, found.Security)
		}

		build := generatedRequests[op]
		if build == nil {
			t.Errorf("%s has no generated request", op)

			continue
		}

		req, err := build("https://api.musher.test/", id)
		if err != nil {
			t.Fatalf("%s: %v", op, err)
		}

		want := param.ReplaceAllString(path, id.String())
		if req.Method != method || req.URL.Path != want {
			t.Errorf("%s: the generated client sends %s %s, the spec says %s %s", op, req.Method, req.URL.Path, method, want)
		}
	}
}

// TestReportBodiesEncode guards the generated oneOf bodies: the *RequestBody
// aliases drop the union's MarshalJSON, so the client must send the union
// type itself.
func TestReportBodiesEncode(t *testing.T) {
	t.Parallel()

	var fetch api.RunnerReportSourceSnapshotFetchJSONBody
	if err := fetch.FromFetchedReport(api.FetchedReport{Outcome: "FETCHED", CommitSha: commit, ContentDigest: "sha256:" + strings.Repeat("a", 64), SizeBytes: 1}); err != nil {
		t.Fatal(err)
	}

	var result api.RunnerReportImageBuildResultJSONBody
	if err := result.FromPushedReport(api.PushedReport{Outcome: "PUSHED", ImageDigest: pushedDigest}); err != nil {
		t.Fatal(err)
	}

	for name, body := range map[string]interface{ MarshalJSON() ([]byte, error) }{"fetch": fetch, "result": result} {
		data, err := body.MarshalJSON()
		if err != nil || !strings.Contains(string(data), `"outcome"`) {
			t.Errorf("%s report encodes to %s, %v", name, data, err)
		}
	}
}

// TestGeneratedClientCallsOnlyTheRunnerSurface scans the generated client for
// any path outside /runner/v1.
func TestGeneratedClientCallsOnlyTheRunnerSurface(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile("api/runner.gen.go")
	if err != nil {
		t.Fatal(err)
	}

	paths := regexp.MustCompile(`operationPath := fmt\.Sprintf\("([^"]+)"`).FindAllStringSubmatch(string(src), -1)
	if len(paths) != len(runner.Operations) {
		t.Fatalf("the generated client has %d operations, runner mode uses %d", len(paths), len(runner.Operations))
	}

	for _, m := range paths {
		if !strings.HasPrefix(m[1], "/runner/v1/") {
			t.Errorf("the generated client calls %s", m[1])
		}
	}
}
