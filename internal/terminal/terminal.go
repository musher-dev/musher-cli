// Package terminal provides terminal detection and capabilities.
package terminal

import (
	"os"

	"golang.org/x/term"
)

// Info holds terminal capability information.
type Info struct {
	IsTTY     bool
	NoColor   bool
	Width     int
	Height    int
	ForceFlag bool
}

// Detect returns terminal information for the current environment.
func Detect() *Info {
	stdoutFD := int(os.Stdout.Fd())
	isTTY := term.IsTerminal(stdoutFD)

	width, height := 80, 24

	if isTTY {
		if w, h, err := term.GetSize(stdoutFD); err == nil {
			width, height = w, h
		}
	}

	_, noColor := os.LookupEnv("NO_COLOR")

	if os.Getenv("TERM") == "dumb" {
		noColor = true
	}

	return &Info{
		IsTTY:   isTTY,
		NoColor: noColor,
		Width:   width,
		Height:  height,
	}
}

// ColorEnabled returns true if colored output should be used.
func (t *Info) ColorEnabled() bool {
	if t.ForceFlag {
		return false
	}

	return t.IsTTY && !t.NoColor
}

// InteractiveEnabled returns true if interactive prompts are allowed.
func (t *Info) InteractiveEnabled() bool {
	return t.IsTTY
}

// SpinnersEnabled returns true if spinners should be used.
func (t *Info) SpinnersEnabled() bool {
	return t.IsTTY && !t.NoColor
}
