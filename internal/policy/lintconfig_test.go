package policy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// depguard matches against ABSOLUTE file paths, so a repo-relative glob like
// "internal/client/**/*.go" silently matches nothing. Every path-scoped rule in
// this repo was written that way and was therefore dead: `internal/doctor` could
// import `internal/output` with no complaint at all, and the architecture
// boundaries this repo advertises were enforced only by the import tests in this
// package.
//
// A dead lint rule is worse than a missing one — it reads as enforcement in
// review. This test pins the two glob shapes that actually match so the rules
// cannot quietly revert.
func TestDepguardGlobsAreAbsolute(t *testing.T) {
	t.Parallel()

	cfg := readLintConfig(t)

	rules, ok := nested(cfg, "linters", "settings", "depguard", "rules")
	if !ok {
		t.Fatal("could not find linters.settings.depguard.rules in .golangci.yml")
	}

	ruleMap, ok := rules.(map[string]any)
	if !ok {
		t.Fatalf("depguard.rules is %T, want a mapping", rules)
	}

	if len(ruleMap) == 0 {
		t.Fatal("depguard has no rules; the import boundaries are unenforced")
	}

	for name, raw := range ruleMap {
		rule, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		files, ok := rule["files"].([]any)
		if !ok {
			continue
		}

		for _, entry := range files {
			glob, ok := entry.(string)
			if !ok {
				continue
			}

			checkGlob(t, name, glob)
		}
	}
}

func checkGlob(t *testing.T, rule, glob string) {
	t.Helper()

	// Negations and depguard's own keywords ($all, $test) are matched by a
	// different code path and are fine as written.
	if strings.HasPrefix(glob, "!") || strings.HasPrefix(glob, "$") {
		return
	}

	if !strings.HasPrefix(glob, "**/") {
		t.Errorf(
			"depguard rule %q has glob %q: repo-relative globs match nothing because "+
				"depguard sees absolute paths. Prefix it with \"**/\".",
			rule, glob,
		)
	}
}

// TestDepguardCoversPackageRootFiles guards the subtler half of the same bug.
//
// "**/internal/client/**/*.go" matches internal/client/stream/sse.go but NOT
// internal/client/client.go, because the inner ** wants at least one path
// segment. A rule written only that way silently exempts the package's own
// files — which is exactly the code the rule was written to constrain. Every
// package glob therefore needs a sibling "**/<pkg>/*.go" entry.
func TestDepguardCoversPackageRootFiles(t *testing.T) {
	t.Parallel()

	cfg := readLintConfig(t)

	rules, _ := nested(cfg, "linters", "settings", "depguard", "rules")

	ruleMap, ok := rules.(map[string]any)
	if !ok {
		t.Fatal("depguard.rules is not a mapping")
	}

	for name, raw := range ruleMap {
		rule, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		files, ok := rule["files"].([]any)
		if !ok {
			continue
		}

		globs := map[string]bool{}

		for _, entry := range files {
			if glob, ok := entry.(string); ok {
				globs[glob] = true
			}
		}

		for glob := range globs {
			recursive, found := strings.CutSuffix(glob, "/**/*.go")
			if !found {
				continue
			}

			if sibling := recursive + "/*.go"; !globs[sibling] {
				t.Errorf(
					"depguard rule %q has %q but not %q, so files directly in that "+
						"package are exempt from the rule",
					name, glob, sibling,
				)
			}
		}
	}
}

func readLintConfig(t *testing.T) map[string]any {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".golangci.yml"))
	if err != nil {
		t.Fatalf("read .golangci.yml: %v", err)
	}

	var cfg map[string]any
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse .golangci.yml: %v", err)
	}

	return cfg
}

func nested(root map[string]any, keys ...string) (any, bool) {
	var current any = root

	for _, key := range keys {
		asMap, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}

		current, ok = asMap[key]
		if !ok {
			return nil, false
		}
	}

	return current, true
}
