# Runner mode

`musher runner` is the CLI inside an isolation guest. The host agent starts it for one attempt of a run that
platform-api owns: a source snapshot's fetch or an image build. It is hidden from `musher --help`. This page is
the reference for its invocation, its rules, its exit codes and the documents it writes. The host agent and
platform-api depend on all of them.

## Invocation

platform-api puts these argv in the run (`source_build/application/services/runner_runs.py`):

| Command | Does |
| --- | --- |
| `musher runner fetch-source --snapshot <id>` | Downloads the commit's tarball with the assignment's `fetchToken`. Hashes it (SHA-256 and size) and refuses one over `maxArchiveBytes`. Opens the upload (`:openArchiveUpload`) and PUTs the bytes with exactly the returned headers. Reports `:reportFetch` FETCHED, or FAILED with a reason. |
| `musher runner build --build <id>` | Downloads the archive and checks its `archiveDigest` and size, which are mandatory. Unpacks it safely into scratch, then asks `:grantPushAccess` and renews the grant before it ends. Builds on the guest's buildkitd and pushes with the grant. Sends output to `log-lines` and reports `:reportResult` PUSHED, or FAILED with a kind. |
| `musher runner self-test` | The host canary's in-guest half (see [Self-test](#self-test)). |

`--snapshot` and `--build` may be left out when `MUSHER_INPUTS_FILE` names the id. If both name one, they must
agree.

## Environment

| Variable | Meaning |
| --- | --- |
| `MUSHER_API_KEY` | The run attempt token (`mush_run_…`), the only credential. Anything else is refused (exit 4). |
| `MUSHER_API_URL` | The API. When it is unset, `api_url` in the inputs file is used. |
| `MUSHER_INPUTS_FILE` | `/run/musher/inputs.json`: `{"api_url", "snapshot_id"}` or `{"api_url", "build_id"}`. |
| `MUSHER_RESULTS_FILE` | Where the command writes its [results](#results-file). |
| `MUSHER_BUILDKIT_HOST` | buildkitd. `--buildkit-host` wins over it, and the default is `unix:///run/buildkit/buildkitd.sock`. The generic `BUILDKIT_HOST` and the Docker Engine are never used. |
| `MUSHER_REGISTRY_HOST` | The registry `self-test` probes. `--registry-host` wins over it. |
| `TMPDIR` | Scratch, `/scratch` in the guest. The archive, the source and the engine's layouts are kept here. |
| `MUSHER_UPDATE_DISABLED` | Set by guest protocol v2. Runner mode never checks for updates either way. |

## Rules

- **One credential.** Only the attempt token. The CLI's config file, profiles and keyring are never read.
- **One surface.** Runner mode calls only these seven `/runner/v1` operations: `runner_get_source_snapshot_assignment`,
  `runner_open_source_snapshot_archive_upload`, `runner_report_source_snapshot_fetch`,
  `runner_get_image_build_assignment`, `runner_grant_image_build_push_access`, `runner_report_image_build_result` and
  `runner_create_image_build_log_lines`. They go through the client generated from the vendored `runner.json`
  (`contracts/vendor/platform-api/`, platform-api 24.0.0). It makes no `/v1` call and no update check. `internal/policy`
  fails the build if `internal/runner` can reach `internal/client`, `auth`, `config`, `update` or `workflow`.
- **Other hosts get no token.** The forge download carries only the fetch token. The presigned upload and download
  carry only what the API signed. The registry gets only the push grant.
- **Fencing.** Any 401 from the runner surface stops the runner, and it reports nothing more. The 401 covers the API's
  `NOT_CURRENT`, `ENDED` and `EXPIRED` refusals, which its problem body does not tell apart. So does a 404, which
  means the resource is not this attempt's. A fenced answer to `log-lines` or `:grantPushAccess` in the middle of a
  build cancels the build.
- **Deadlines.** The command never outlives the token's expiry or the build's `deadlineAt`. Work stops 15 seconds
  before the deadline, so a FAILED report can still be made. A build the deadline cuts short reports nothing and
  exits 22. platform-api then times the attempt out.
- **Retries.** A network error, 408, 425, 429 or 5xx is retried with backoff (1 s doubling to 16 s, 6 tries), and
  never past the deadline. Any other refusal is final.
- **Redaction.** The build's secret values, every push grant's password and the attempt token are removed from log
  lines and from report details. A log line over 16 KiB is split, and a detail is cut to 4096 bytes.
- **No cache.** No cache crosses builds in v1. A cache per organization and project, written only from trusted refs,
  is a follow-up.

## Exit codes

Fixed (`internal/errors`, `TestExitCodesAreStable`):

| Code | Meaning | Reported |
| --- | --- | --- |
| 0 | FETCHED or PUSHED was reported and accepted; or self-test found no failed check | yes |
| 20 | A classified FAILED was reported and accepted | yes |
| 21 | Fenced: 401 (`NOT_CURRENT`/`ENDED`/`EXPIRED`) or 404 from the runner surface | no |
| 22 | The deadline (or the token's expiry) arrived first | no |
| 23 | An outcome was reached but could not be reported: the API was unreachable after retries, or refused it other than by fencing | no |
| 24 | `self-test` wrote its results and at least one check failed | n/a |
| 4 | The environment is incomplete: no attempt token, no API URL, or an unreadable inputs file | no |
| 64 | Usage: no id, or a flag and the inputs file disagree | no |

## Failure classification

`fetch-source`, `FailedFetchReport.reason`:

| Reason | When |
| --- | --- |
| `COMMIT_NOT_FOUND` | The forge answers 404 or 422 for the commit's tarball. |
| `REPOSITORY_UNREACHABLE` | The fetch token is refused (401/403), or the forge is unreachable or answers 5xx after retries. |
| `ARCHIVE_TOO_LARGE` | The tarball is over `maxArchiveBytes`. |
| `FETCH_FAILED` | Anything else: storage refuses the upload, the upload cannot be opened, the archive is empty, the assignment is malformed, or the deadline is near. |

`build`, `FailedBuildReport.kind`:

| Kind | When | Retried by the platform |
| --- | --- | --- |
| `RECIPE_INVALID` | The context or Dockerfile path leaves the source, or a buildpacks builder is not pinned by digest. Also an unknown kind, a secret id with no value, a missing Dockerfile, or a Dockerfile parse error. | no |
| `BUILD_FAILED` | A step or buildpack failed, or the source archive is unsafe (an absolute or climbing path, a symlink or hard link leaving the source, a decompression bomb). | no |
| `PUSH_FAILED` | The image was built, but the registry refused it or returned no manifest digest. | no |
| `INFRASTRUCTURE` | buildkitd is unreachable or the engine failed, or the archive is not the assigned one (digest or size). Also a push grant that is refused or for another repository, or a malformed assignment. | yes |

## Results file

`fetch-source` and `build` write a flat JSON object of strings to `MUSHER_RESULTS_FILE`. The keys are snake_case.

| Key | Values |
| --- | --- |
| `outcome` | `FETCHED`, `PUSHED` or `FAILED`; absent when nothing was reported |
| `reason` | The reason or kind of a FAILED outcome |
| `exit` | `reported`, `fenced`, `deadline` or `unreported` |

The image digest is never written here. It travels only on `:reportResult`.

## Self-test

`musher runner self-test` runs every check at once. It writes the results document to `--results-file` (or
`MUSHER_RESULTS_FILE`) and to stdout, and exits 0 or 24. The document's schema is
[`contracts/runner/self-test.schema.json`](../contracts/runner/self-test.schema.json), and
`TestSelfTestDocumentMatchesItsSchema` holds the code to it. The keys are snake_case and stable.
`schema_version` changes only when a key is removed or changes meaning. A check may be added without a version
change, so a reader ignores checks it does not know. A check is `pass`, `fail` or `skip`. `passed` is false only
when some check is `fail`.

| Check | Passes when | Isolation condition |
| --- | --- | --- |
| `buildkit_workers` | buildkitd lists at least one worker (`buildctl debug workers`) | `BUILD_ENGINE_READY` |
| `buildkit_scratch_build` | A `FROM scratch` build with one copied file solves, with nothing exported or pushed | `BUILD_ENGINE_READY` |
| `api_reachable` | TCP and a verified TLS handshake reach the API URL's host. It fails when no API URL is set. | `API_EGRESS` |
| `registry_reachable` | TCP and verified TLS reach `--registry-host` (port 443 unless given). It is skipped when none is set. | `REGISTRY_EGRESS` |
| `egress_denied_metadata` | A TCP connect to `169.254.169.254:80` does not succeed within 3 s | `EGRESS_POLICY_ENFORCED` |
| `egress_denied_rfc1918` | The same, for `10.0.0.1:443` | `EGRESS_POLICY_ENFORCED` |
| `egress_denied_cgnat` | The same, for `100.64.0.1:443` (RFC 6598) | `EGRESS_POLICY_ENFORCED` |
| `scratch_writable` | A file written to `TMPDIR` reads back unchanged | `SCRATCH_DISK` |

`--deny-metadata`, `--deny-rfc1918` and `--deny-cgnat` change the deny targets. For a deny probe, refused,
unreachable and timed out all pass, and the reason is in `detail`.

```json
{
  "schema_version": 1,
  "runner_version": "0.4.0",
  "started_at": "2026-11-15T23:31:18.597Z",
  "finished_at": "2026-11-15T23:31:21.599Z",
  "passed": true,
  "checks": {
    "api_reachable": { "status": "pass", "target": "api.musher.dev:443", "detail": "TLS 1.3 handshake verified", "duration_ms": 78 },
    "buildkit_scratch_build": { "status": "pass", "target": "unix:///run/buildkit/buildkitd.sock", "detail": "FROM scratch built, nothing pushed", "duration_ms": 444 },
    "buildkit_workers": { "status": "pass", "target": "unix:///run/buildkit/buildkitd.sock", "detail": "ti5z7… (linux/amd64)", "duration_ms": 66 },
    "egress_denied_cgnat": { "status": "pass", "target": "100.64.0.1:443", "detail": "denied: timed out", "duration_ms": 3000 },
    "egress_denied_metadata": { "status": "pass", "target": "169.254.169.254:80", "detail": "denied: refused", "duration_ms": 2 },
    "egress_denied_rfc1918": { "status": "pass", "target": "10.0.0.1:443", "detail": "denied: timed out", "duration_ms": 3001 },
    "registry_reachable": { "status": "pass", "target": "registry.musher.dev:443", "detail": "TLS 1.3 handshake verified", "duration_ms": 137 },
    "scratch_writable": { "status": "pass", "target": "/scratch", "detail": "written and read back", "duration_ms": 0 }
  }
}
```

## Engines

Both engines run on the guest's buildkitd, which guest-init starts as root (guest protocol v2). Both push with the
build's push grant alone. Registry credentials come from the grant and are handed out only for the grant's
registry host, and only when the registry asks. The docker credential store is never read.

- **Dockerfile.** This is the `internal/build/buildkit` driver pointed at the guest socket. Secrets go in through
  BuildKit's session (`--secret`), and BuildKit's image exporter pushes. BuildKit keeps a credential it has cached for
  at most a minute, so a renewed grant is the one used.
- **Buildpacks** (`internal/build/cnb`). The builder image, pinned by digest, is the root filesystem of one BuildKit
  exec. That exec runs the CNB lifecycle's `creator` as the builder's `CNB_USER_ID:CNB_GROUP_ID`, in OCI layout mode
  (`-layout`, platform API 0.12, `CNB_EXPERIMENTAL_MODE=warn`) with `-skip-restore` and no cache.
  - The run image the builder names is pulled anonymously into an OCI layout and mounted in.
  - Build arguments become the platform's `env/` files. Secrets are session secrets mounted at
    `/platform/env/<id>`, so their values never enter the build definition.
  - The app image comes out as an OCI layout through BuildKit's local exporter. The runner then pushes it itself with
    the grant.
  - Nothing in detect, build or export talks to a registry, so no phase holds a registry credential.

The buildpacks path was proved before it was built (the go/no-go spike). `go test -tags integration
./internal/build/cnb` with a standalone buildkitd (`moby/buildkit:v0.34.1`, by digest) built the Paketo Node sample
in `internal/build/cnb/testdata`. The builder was `paketobuildpacks/builder-jammy-base@sha256:5cd813b8…`, with
lifecycle 0.21.29. The test pushed the layout to a local registry, and the image served requests. The pushed digest
was the one the lifecycle reported. The fallback (`creator` publishing to the registry with a scoped grant) was not
needed.
