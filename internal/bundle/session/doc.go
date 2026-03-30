// Package session manages the lifecycle of loading a bundle into a harness.
//
// A LoadSession materializes bundle assets from the content-addressable cache
// into a harness-native directory layout, launches the harness binary, and
// cleans up injected files when the session ends.
//
// Two injection modes are supported, driven by the harness spec:
//
//   - "add_dir": Assets are written to a temporary directory and passed to the
//     harness via a CLI flag (e.g. --add-dir). Cleanup removes the entire temp
//     directory.
//
//   - "cwd": Assets are injected directly into the project working directory.
//     Existing files are backed up and restored on cleanup.
//
// Tool configuration assets (MCP servers) are handled with a prefer-CLI-flag
// strategy: if the harness supports a --mcp-config flag, a standalone config
// file is written and passed via the flag. Otherwise, the bundle config is
// merged into the harness's existing config file.
package session
