// Package runner is runner mode: the musher CLI inside an isolation guest,
// run by the host agent for one attempt of a platform-owned run.
//
//   - `musher runner fetch-source --snapshot <id>` fetches a commit's tarball
//     with the assignment's fetch token, uploads it to the presigned URL the
//     API opens, and reports FETCHED or a classified FAILED.
//   - `musher runner build --build <id>` verifies and unpacks the snapshot's
//     archive, builds it on the guest's buildkitd (Dockerfile, or the CNB
//     lifecycle for buildpacks), pushes with the build's short-lived push
//     grant, streams its output to log-lines, and reports PUSHED or a
//     classified FAILED.
//   - `musher runner self-test` is the host canary's in-guest half.
//
// The rules runner mode keeps (docs/runner-mode.md):
//
//   - The only credential is the run attempt token in MUSHER_API_KEY. The
//     CLI's config, profiles and keyring are never read.
//   - The only API calls are the /runner/v1 operations in Operations, through
//     the client generated from the vendored runner.json. This package never
//     imports the /v1 client (internal/client), and runner mode never checks
//     for updates.
//   - A 401 or 404 from the runner surface fences the attempt: the runner
//     stops and reports nothing more.
//   - Nothing runs past the assignment's deadlineAt or the token's expiry.
//   - Exit codes are fixed (internal/errors ExitRunner*), and every failure is
//     classified against the API's enums.
package runner
