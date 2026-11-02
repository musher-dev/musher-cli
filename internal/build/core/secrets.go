package core

import (
	"os"
	"path/filepath"

	"github.com/musher-dev/musher-cli/internal/env"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

// LookupFunc reads an environment variable the way os.LookupEnv does.
type LookupFunc func(key string) (string, bool)

// ResolveSecrets reads every secret's value from its local source. File paths
// are relative to root unless absolute. It fails on the first missing source so
// a build never starts without its secrets. Errors name the secret ID and its
// source, never the value. A nil lookup reads the process environment.
func ResolveSecrets(secrets map[string]SecretSource, root string, lookup LookupFunc) (map[string][]byte, error) {
	if lookup == nil {
		lookup = env.Lookup
	}

	values := make(map[string][]byte, len(secrets))

	for _, id := range sortedKeys(secrets) {
		src := secrets[id]

		switch {
		case src.Env != "":
			value, ok := lookup(src.Env)
			if !ok {
				return nil, clierrors.Errorf("build secret %s: environment variable %s is not set", id, src.Env)
			}

			values[id] = []byte(value)
		case src.File != "":
			path := src.File
			if !filepath.IsAbs(path) {
				path = filepath.Join(root, path)
			}

			data, err := os.ReadFile(path) //nolint:gosec // G304: the user names their own secret file in musher.yaml or --secret
			if err != nil {
				return nil, clierrors.Errorf("build secret %s: read %s: %w", id, path, err)
			}

			values[id] = data
		default:
			return nil, clierrors.Errorf("build secret %s has no local source (env or file)", id)
		}
	}

	return values, nil
}
