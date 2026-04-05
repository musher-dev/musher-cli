package runtime

import "testing"

func TestSelect_NoTTY_ReturnsPassthrough(t *testing.T) {
	t.Parallel()

	e := Select(false, false)
	if _, ok := e.(*Passthrough); !ok {
		t.Errorf("Select(isTTY=false, noWatch=false) = %T, want *Passthrough", e)
	}
}

func TestSelect_NoWatch_ReturnsPassthrough(t *testing.T) {
	t.Parallel()

	e := Select(true, true)
	if _, ok := e.(*Passthrough); !ok {
		t.Errorf("Select(isTTY=true, noWatch=true) = %T, want *Passthrough", e)
	}
}

func TestSelect_TTY_ReturnsEmbedded(t *testing.T) {
	t.Parallel()

	e := Select(true, false)

	// On Unix this should be *Embedded; on other platforms *Passthrough.
	// Either way it should not be nil.
	if e == nil {
		t.Fatal("Select(isTTY=true, noWatch=false) returned nil")
	}
}
