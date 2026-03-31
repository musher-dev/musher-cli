// Package provider is the parent directory for individual harness provider
// implementations.  Each provider lives in its own subdirectory (e.g.
// provider/claude/, provider/codex/) and exports a Module variable that
// the harness registry uses for registration.
//
// To add a new harness provider:
//  1. Create provider/<name>/spec.yaml with the declarative configuration.
//  2. Create provider/<name>/provider.go that embeds spec.yaml and exports Module.
//  3. Create provider/<name>/executor.go implementing the Executor interface.
//  4. Add <name>.Module to the RegisterBuiltins call in the harness package.
package provider
