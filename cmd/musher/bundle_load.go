package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/harness"
	"github.com/musher-dev/musher-cli/internal/harness/provider/claude"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/prompt"
)

func newBundleLoadCmd() *cobra.Command {
	var (
		harnessName string
		force       bool
	)

	cmd := &cobra.Command{
		Use:   "load <namespace/slug[:version]>",
		Short: "Download a bundle and prepare it for use",
		Long: `Download a bundle from the registry and prepare it for use with a harness.

In an interactive terminal, prompts for the next action (run or install).
In non-interactive environments, prints a summary and hints.
Use --harness to specify the target harness for automation.`,
		Example: `  musher bundle load acme/code-review
  musher bundle load acme/code-review:1.0.0
  musher bundle load acme/code-review --harness claude
  musher bundle load acme/code-review --json`,
		Args: requireOneArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runBundleLoad(cmd.Context(), out, args[0], harnessName, force)
		},
	}

	cmd.Flags().StringVar(&harnessName, "harness", "",
		"Target harness (e.g. claude)")
	cmd.Flags().BoolVar(&force, "force", false,
		"Re-download even if already cached")

	return cmd
}

// bundleLoadResult is the JSON output for the bundle load command.
type bundleLoadResult struct {
	Namespace   string            `json:"namespace"`
	Slug        string            `json:"slug"`
	Version     string            `json:"version"`
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	AssetCount  int               `json:"assetCount"`
	Assets      []bundleLoadAsset `json:"assets"`
	CacheRoot   string            `json:"cacheRoot"`
	Cached      bool              `json:"cached"`
	Harness     string            `json:"harness,omitempty"`
	Command     string            `json:"command,omitempty"`
}

type bundleLoadAsset struct {
	Path string `json:"path"`
	Type string `json:"type"`
	Size int64  `json:"size"`
}

func runBundleLoad(ctx context.Context, out *output.Writer, ref, harnessName string, force bool) error {
	namespace, slug, bundleVersion, err := parseBundleRefOptionalVersion(ref)
	if err != nil {
		return err
	}

	result, err := pullToCache(ctx, out, namespace, slug, bundleVersion, force)
	if err != nil {
		return err
	}

	versionRef := result.Namespace + "/" + result.Slug + ":" + result.Version

	// JSON mode: output structured result and exit.
	if out.JSON {
		return printBundleLoadJSON(out, result, harnessName)
	}

	// Print bundle summary.
	printBundleSummary(out, result, versionRef)

	p := prompt.New(out)

	// Non-interactive: print summary with automation hints.
	if !p.CanPrompt() {
		if harnessName != "" {
			return printHarnessCommand(out, result, harnessName, versionRef)
		}

		out.Println()
		out.Info("To load into a harness:")
		out.Muted("  musher bundle load %s --harness claude", versionRef)

		return nil
	}

	// Interactive: prompt for action if no harness pre-selected.
	if harnessName != "" {
		return printHarnessCommand(out, result, harnessName, versionRef)
	}

	return promptForAction(out, p, result, versionRef)
}

func printBundleSummary(out *output.Writer, result *pullCacheResult, versionRef string) {
	if result.Cached {
		out.Success("Already cached: %s", versionRef)
	}

	if result.Name != "" {
		out.Print("%s\n", result.Name)
	}

	if result.Description != "" {
		out.Muted("  %s", result.Description)
	}

	out.Muted("  %s", formatAssetSummary(result.Layers))
}

func printBundleLoadJSON(out *output.Writer, result *pullCacheResult, harnessName string) error {
	assets := make([]bundleLoadAsset, 0, len(result.Layers))
	for _, layer := range result.Layers {
		assets = append(assets, bundleLoadAsset{
			Path: layer.LogicalPath,
			Type: layer.AssetType,
			Size: layer.Size,
		})
	}

	jsonResult := bundleLoadResult{
		Namespace:   result.Namespace,
		Slug:        result.Slug,
		Version:     result.Version,
		Name:        result.Name,
		Description: result.Description,
		AssetCount:  len(result.Layers),
		Assets:      assets,
		CacheRoot:   result.CacheRoot,
		Cached:      result.Cached,
		Harness:     harnessName,
	}

	if jsonErr := out.PrintJSON(jsonResult); jsonErr != nil {
		return fmt.Errorf("print JSON: %w", jsonErr)
	}

	return nil
}

func printHarnessCommand(out *output.Writer, result *pullCacheResult, harnessName, versionRef string) error {
	reg := newHarnessRegistry()

	prov, ok := reg.Get(harnessName)
	if !ok {
		return &clierrors.CLIError{
			Message: "Unknown harness: " + harnessName,
			Hint:    "Available harnesses: " + strings.Join(reg.Names(), ", "),
			Code:    clierrors.ExitUsage,
		}
	}

	if !prov.Available() {
		out.Warning("Harness %q is not installed", prov.Spec.DisplayName)

		if prov.Spec.Status.InstallHint != "" {
			out.Info("  %s", prov.Spec.Status.InstallHint)
		}
	}

	out.Print("\n")
	out.Info("Run with %s:", prov.Spec.DisplayName)
	out.Print("  %s %s %s\n", prov.Spec.Binary, prov.Spec.BundleDir.Flag, result.CacheRoot)
	out.Muted("  Bundle: %s (%d assets)", versionRef, len(result.Layers))

	return nil
}

func promptForAction(out *output.Writer, p *prompt.Prompter, result *pullCacheResult, versionRef string) error {
	reg := newHarnessRegistry()
	available := reg.Available()

	if len(available) == 0 {
		out.Info("No harnesses detected. Install a supported harness to load bundles.")

		return nil
	}

	options := []string{"Run with harness", "Exit"}

	choice, err := p.Select("What would you like to do?", options)
	if err != nil {
		return fmt.Errorf("prompt: %w", err)
	}

	if choice == 0 {
		return selectAndPrintHarness(out, p, reg, result, versionRef)
	}

	return nil
}

func selectAndPrintHarness(out *output.Writer, p *prompt.Prompter, reg *harness.Registry, result *pullCacheResult, versionRef string) error {
	available := reg.Available()
	if len(available) == 1 {
		return printHarnessCommand(out, result, available[0].Spec.Name, versionRef)
	}

	names := make([]string, 0, len(available))
	for _, prov := range available {
		names = append(names, prov.Spec.DisplayName)
	}

	choice, err := p.Select("Select a harness:", names)
	if err != nil {
		return fmt.Errorf("prompt: %w", err)
	}

	return printHarnessCommand(out, result, available[choice].Spec.Name, versionRef)
}

// newHarnessRegistry creates the default harness registry with built-in providers.
func newHarnessRegistry() *harness.Registry {
	reg := harness.NewRegistry()
	harness.RegisterBuiltins(reg, claude.Module)

	return reg
}
