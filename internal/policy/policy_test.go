package policy_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

const moduleRoot = "github.com/musher-dev/musher-cli"

var (
	featureOrchestration = map[string]bool{
		moduleRoot + "/internal/doctor": true,
		moduleRoot + "/internal/output": true,
		moduleRoot + "/internal/prompt": true,
		moduleRoot + "/internal/tui":    true,
	}

	platformCore = map[string]bool{
		moduleRoot + "/internal/auth":          true,
		moduleRoot + "/internal/buildinfo":     true,
		moduleRoot + "/internal/client":        true,
		moduleRoot + "/internal/config":        true,
		moduleRoot + "/internal/deployspec":    true,
		moduleRoot + "/internal/env":           true,
		moduleRoot + "/internal/errors":        true,
		moduleRoot + "/internal/observability": true,
		moduleRoot + "/internal/paths":         true,
		moduleRoot + "/internal/safeio":        true,
		moduleRoot + "/internal/terminal":      true,
		moduleRoot + "/internal/update":        true,
		moduleRoot + "/internal/validate":      true,
		moduleRoot + "/internal/workflow":      true,
		moduleRoot + "/internal/policy":        true,
	}

	testSupport = map[string]bool{
		moduleRoot + "/internal/testutil": true,
	}

	presentationPkgs = map[string]bool{
		moduleRoot + "/internal/output": true,
		moduleRoot + "/internal/prompt": true,
	}
)

func repoRoot(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve caller path")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func loadAllPackages(t *testing.T) []*listedPackage {
	t.Helper()

	root := repoRoot(t)
	pkgImports := map[string]map[string]bool{}
	fset := token.NewFileSet()

	for _, top := range []string{"cmd", "internal"} {
		walkErr := filepath.Walk(filepath.Join(root, top), func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}

			file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if err != nil {
				return repoerrors.Errorf("parse %s: %w", path, err)
			}

			relDir, err := filepath.Rel(root, filepath.Dir(path))
			if err != nil {
				return repoerrors.Errorf("resolve path for %s: %w", path, err)
			}

			importPath := moduleRoot + "/" + filepath.ToSlash(relDir)
			if pkgImports[importPath] == nil {
				pkgImports[importPath] = map[string]bool{}
			}

			for _, imp := range file.Imports {
				pkgImports[importPath][strings.Trim(imp.Path.Value, "\"")] = true
			}

			return nil
		})
		if walkErr != nil {
			t.Fatalf("walk %s: %v", top, walkErr)
		}
	}

	pkgs := make([]*listedPackage, 0, len(pkgImports))
	for importPath, imports := range pkgImports {
		pkg := &listedPackage{ImportPath: importPath}

		for imp := range imports {
			pkg.Imports = append(pkg.Imports, imp)
		}

		pkgs = append(pkgs, pkg)
	}

	return pkgs
}

func parseGoFiles(t *testing.T, dir string) map[string]*ast.File {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(repoRoot(t), dir, "*.go"))
	if err != nil {
		t.Fatalf("glob %s: %v", dir, err)
	}

	files := make(map[string]*ast.File, len(paths))
	fset := token.NewFileSet()

	for _, path := range paths {
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", path, parseErr)
		}

		files[path] = file
	}

	return files
}

func parseGoTree(t *testing.T, dir string) map[string]*ast.File {
	t.Helper()

	files := make(map[string]*ast.File)
	fset := token.NewFileSet()

	walkErr := filepath.Walk(filepath.Join(repoRoot(t), dir), func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return repoerrors.Errorf("parse %s: %w", path, parseErr)
		}

		files[path] = file

		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", dir, walkErr)
	}

	return files
}

func isInternal(path string) bool {
	return strings.HasPrefix(path, moduleRoot+"/internal/")
}

func internalBase(path string) string {
	suffix := strings.TrimPrefix(path, moduleRoot+"/internal/")
	parts := strings.SplitN(suffix, "/", 2)

	base := parts[0]
	base = strings.TrimSuffix(base, ".test")
	base = strings.TrimSuffix(base, "_test")

	return moduleRoot + "/internal/" + base
}

type listedPackage struct {
	ImportPath string   `json:"ImportPath"`
	Imports    []string `json:"Imports"`
}

func isTestPackage(pkg *listedPackage) bool {
	return strings.HasSuffix(pkg.ImportPath, "_test") || strings.HasSuffix(pkg.ImportPath, ".test")
}

func selectorMatches(expr ast.Expr, pkgName, member string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}

	return ident.Name == pkgName && sel.Sel.Name == member
}

func containsSelectorCall(file *ast.File, pkgName, member string) bool {
	found := false

	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		if selectorMatches(call.Fun, pkgName, member) {
			found = true
			return false
		}

		return true
	})

	return found
}

func fileContainsIdentifier(file *ast.File, identName string) bool {
	found := false

	ast.Inspect(file, func(n ast.Node) bool {
		ident, ok := n.(*ast.Ident)
		if ok && ident.Name == identName {
			found = true
			return false
		}

		return true
	})

	return found
}

func TestAllInternalPackagesClassified(t *testing.T) {
	pkgs := loadAllPackages(t)
	seen := map[string]bool{}

	for _, pkg := range pkgs {
		if !isInternal(pkg.ImportPath) {
			continue
		}

		base := internalBase(pkg.ImportPath)
		if seen[base] {
			continue
		}

		seen[base] = true
		if !featureOrchestration[base] && !platformCore[base] && !testSupport[base] {
			t.Errorf(
				"internal package %s is not classified in internal/policy; add it to featureOrchestration, platformCore, or testSupport",
				base,
			)
		}
	}
}

// TestNoStaleClassifications is the inverse of TestAllInternalPackagesClassified.
//
// That test only fails on packages missing from the maps, so an entry naming a
// package that has since been deleted lingers forever and nothing notices. After
// a large refactor those stale entries are indistinguishable from live ones, and
// a future package reusing the name silently inherits the wrong classification.
func TestNoStaleClassifications(t *testing.T) {
	root := repoRoot(t)

	maps := map[string]map[string]bool{
		"featureOrchestration": featureOrchestration,
		"platformCore":         platformCore,
		"testSupport":          testSupport,
		"presentationPkgs":     presentationPkgs,
	}

	for mapName, classified := range maps {
		for importPath := range classified {
			rel := strings.TrimPrefix(importPath, moduleRoot+"/")
			if rel == importPath {
				t.Errorf("%s: entry %q is not in this module", mapName, importPath)

				continue
			}

			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
				t.Errorf("%s: entry %q names a package that no longer exists; remove it", mapName, importPath)
			}
		}
	}
}

func TestPlatformPackagesDoNotImportPresentation(t *testing.T) {
	for _, pkg := range loadAllPackages(t) {
		if !isInternal(pkg.ImportPath) || isTestPackage(pkg) {
			continue
		}

		base := internalBase(pkg.ImportPath)
		if !platformCore[base] {
			continue
		}

		for _, imp := range pkg.Imports {
			if presentationPkgs[imp] {
				t.Errorf("%s imports presentation package %s", pkg.ImportPath, imp)
			}
		}
	}
}

func TestInternalPackagesDoNotImportCmd(t *testing.T) {
	cmdPrefix := moduleRoot + "/cmd/"

	for _, pkg := range loadAllPackages(t) {
		if !isInternal(pkg.ImportPath) || isTestPackage(pkg) {
			continue
		}

		for _, imp := range pkg.Imports {
			if strings.HasPrefix(imp, cmdPrefix) {
				t.Errorf("%s imports cmd package %s", pkg.ImportPath, imp)
			}
		}
	}
}

func TestDoctorDoesNotImportOutput(t *testing.T) {
	doctorPkg := moduleRoot + "/internal/doctor"
	outputPkg := moduleRoot + "/internal/output"

	for _, pkg := range loadAllPackages(t) {
		if pkg.ImportPath != doctorPkg || isTestPackage(pkg) {
			continue
		}

		for _, imp := range pkg.Imports {
			if imp == outputPkg {
				t.Fatalf("internal/doctor imports internal/output")
			}
		}
	}
}

func TestTestutilNotImportedByProductionCode(t *testing.T) {
	testutilPkg := moduleRoot + "/internal/testutil"

	for _, pkg := range loadAllPackages(t) {
		if isTestPackage(pkg) {
			continue
		}

		for _, imp := range pkg.Imports {
			if imp == testutilPkg {
				t.Errorf("%s imports internal/testutil", pkg.ImportPath)
			}
		}
	}
}

func TestNoCrossLayerFeatureImports(t *testing.T) {
	allowed := map[string]map[string]bool{
		moduleRoot + "/internal/prompt": {
			moduleRoot + "/internal/output": true,
		},
	}

	for _, pkg := range loadAllPackages(t) {
		if !isInternal(pkg.ImportPath) || isTestPackage(pkg) {
			continue
		}

		base := internalBase(pkg.ImportPath)
		if !featureOrchestration[base] {
			continue
		}

		for _, imp := range pkg.Imports {
			if !isInternal(imp) {
				continue
			}

			impBase := internalBase(imp)
			if !featureOrchestration[impBase] || impBase == base || allowed[base][impBase] {
				continue
			}

			t.Errorf("%s imports sibling feature package %s", pkg.ImportPath, imp)
		}
	}
}

func TestNoOsExitOutsideMain(t *testing.T) {
	for path, file := range parseGoFiles(t, "cmd/musher") {
		if strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, string(filepath.Separator)+"main.go") {
			continue
		}

		if containsSelectorCall(file, "os", "Exit") {
			t.Errorf("%s uses os.Exit outside main.go", path)
		}
	}
}

func TestOutputDefaultOnlyUsedByDefaultOutput(t *testing.T) {
	rootFile := filepath.Join(repoRoot(t), "cmd", "musher", "root.go")
	allowedCalls := 1
	actualCalls := 0

	for path, file := range parseGoFiles(t, "cmd/musher") {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !selectorMatches(call.Fun, "output", "Default") {
				return true
			}

			actualCalls++

			if path != rootFile {
				t.Errorf("%s uses output.Default outside cmd/musher/root.go", path)
			}

			return true
		})
	}

	if actualCalls != allowedCalls {
		t.Fatalf("output.Default call count = %d, want %d", actualCalls, allowedCalls)
	}
}

func TestProductionCodeDoesNotUseFmtErrorf(t *testing.T) {
	for _, dir := range []string{"cmd/musher", "internal"} {
		for path, file := range parseGoTree(t, dir) {
			if strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, string(filepath.Separator)+"policy_test.go") {
				continue
			}

			if containsSelectorCall(file, "fmt", "Errorf") {
				t.Errorf("%s uses fmt.Errorf; use internal/errors.Errorf instead", path)
			}
		}
	}
}

func TestConfirmCallersHandleNonInteractiveMode(t *testing.T) {
	for path, file := range parseGoFiles(t, "cmd/musher") {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}

		if !containsSelectorCall(file, "prompter", "Confirm") && !containsSelectorCall(file, "p", "Confirm") {
			hasConfirm := false

			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}

				sel, ok := call.Fun.(*ast.SelectorExpr)
				if ok && sel.Sel.Name == "Confirm" {
					hasConfirm = true
					return false
				}

				return true
			})

			if !hasConfirm {
				continue
			}
		}

		if !fileContainsIdentifier(file, "CanPrompt") && !fileContainsIdentifier(file, "NoInput") && !fileContainsIdentifier(file, "yes") {
			t.Errorf("%s uses Confirm without explicit non-interactive handling", path)
		}
	}
}

// TestCmdUsesSafeio asserts that cmd/musher does not call file-I/O functions
// on the os package directly — they must go through internal/safeio so that
// permission, error wrapping, and gosec exemptions live in one place.
func TestCmdUsesSafeio(t *testing.T) {
	forbidden := []string{"ReadFile", "WriteFile", "MkdirAll", "Create", "OpenFile"}

	for path, file := range parseGoFiles(t, "cmd/musher") {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}

		for _, name := range forbidden {
			if containsSelectorCall(file, "os", name) {
				t.Errorf("%s uses os.%s; use internal/safeio instead", path, name)
			}
		}
	}
}

// TestEnvVarsCentralized asserts that production code reads environment
// variables only through internal/env (or via internal/config which binds env
// vars through viper). Test files and the env package itself are exempt.
func TestEnvVarsCentralized(t *testing.T) {
	envPkgPrefix := filepath.Join("internal", "env") + string(filepath.Separator)

	for _, dir := range []string{"cmd/musher", "internal"} {
		for path, file := range parseGoTree(t, dir) {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}

			rel, err := filepath.Rel(repoRoot(t), path)
			if err == nil && strings.HasPrefix(rel, envPkgPrefix) {
				continue
			}

			for _, name := range []string{"Getenv", "LookupEnv"} {
				if containsSelectorCall(file, "os", name) {
					t.Errorf("%s uses os.%s; use internal/env instead", path, name)
				}
			}
		}
	}
}

// TestCmdReturnsCLIError asserts that cmd/musher RunE functions surface
// errors through internal/errors (CLIError) rather than constructing raw
// stdlib errors. This is a heuristic check: it forbids errors.New and
// errors.Unwrap-style construction inside cmd files. errors.Is / errors.As
// remain allowed because they only inspect, never construct.
//
// This check is complemented at runtime by the cmd layer always wrapping
// foreign errors via clierrors.Wrap before returning them from RunE.
func TestCmdReturnsCLIError(t *testing.T) {
	for path, file := range parseGoFiles(t, "cmd/musher") {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}

		// Walk function bodies only — package-level `var X = errors.New(...)`
		// declarations are sentinel errors used with errors.Is checks and are
		// permitted (they never escape to the user as the *primary* return).
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}

			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}

				if selectorMatches(call.Fun, "errors", "New") {
					pos := fn.Name.Name
					t.Errorf("%s: function %s constructs a stdlib errors.New; use internal/errors (clierrors.New / Wrap / Errorf) so cmd layer returns CLIError", path, pos)
				}

				return true
			})
		}
	}
}

func TestPromptPackageUsesStdinFDForTTYChecks(t *testing.T) {
	for path, file := range parseGoFiles(t, "internal/prompt") {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Fd" {
				return true
			}

			outer, ok := sel.X.(*ast.SelectorExpr)
			if !ok {
				return true
			}

			pkg, ok := outer.X.(*ast.Ident)
			if ok && pkg.Name == "os" && outer.Sel.Name == "Stdout" {
				t.Errorf("%s uses os.Stdout.Fd for TTY checks", path)
				return false
			}

			return true
		})
	}
}
