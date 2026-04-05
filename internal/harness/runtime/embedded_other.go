//go:build !unix

package runtime

// newEmbedded returns a Passthrough executor on non-Unix platforms where
// PTY support is not available.
func newEmbedded() Executor {
	return &Passthrough{}
}
