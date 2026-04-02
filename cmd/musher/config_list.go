package main

import (
	"encoding/json"
	"maps"
	"sort"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/config"
	"github.com/musher-dev/musher-cli/internal/output"
)

func newConfigListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all configuration values",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := output.FromContext(cmd.Context())
			cfg := config.Load()
			settings := cfg.All()

			if out.JSON {
				return json.NewEncoder(out.Out).Encode(settings)
			}

			flat := flattenMap("", settings)

			keys := make([]string, 0, len(flat))
			for key := range flat {
				keys = append(keys, key)
			}

			sort.Strings(keys)

			for _, key := range keys {
				out.Print("%s = %v\n", key, flat[key])
			}

			return nil
		},
	}
}

// flattenMap flattens a nested map into dot-separated keys.
func flattenMap(prefix string, src map[string]any) map[string]any {
	result := make(map[string]any)

	for key, val := range src {
		if prefix != "" {
			key = prefix + "." + key
		}

		if nested, ok := val.(map[string]any); ok {
			maps.Copy(result, flattenMap(key, nested))
		} else {
			result[key] = val
		}
	}

	return result
}
