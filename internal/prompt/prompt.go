// Package prompt provides interactive prompts for the Musher CLI.
package prompt

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/musher-dev/musher-cli/internal/output"
	"golang.org/x/term"
)

// Prompter handles interactive prompts.
type Prompter struct {
	out    *output.Writer
	reader *bufio.Reader
}

// New creates a new Prompter.
func New(out *output.Writer) *Prompter {
	return &Prompter{
		out:    out,
		reader: bufio.NewReader(os.Stdin),
	}
}

// CanPrompt returns true if interactive prompts are available.
func (p *Prompter) CanPrompt() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && !p.out.NoInput
}

// Confirm prompts for a yes/no confirmation.
func (p *Prompter) Confirm(message string, defaultValue bool) (bool, error) {
	defaultStr := "y/N"
	if defaultValue {
		defaultStr = "Y/n"
	}

	p.out.Print("%s [%s]: ", message, defaultStr)

	input, err := p.reader.ReadString('\n')
	if err != nil {
		return defaultValue, fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" {
		return defaultValue, nil
	}

	return input == "y" || input == "yes", nil
}

// Password prompts for a password, showing * for each character typed.
func (p *Prompter) Password(prompt string) (string, error) {
	p.out.Print("%s: ", prompt)

	stdinFd := int(os.Stdin.Fd())

	oldState, err := term.MakeRaw(stdinFd)
	if err != nil {
		return "", fmt.Errorf("failed to set raw mode: %w", err)
	}

	defer func() { _ = term.Restore(stdinFd, oldState) }()

	var (
		buf []byte
		b   [1]byte
	)

	for {
		_, err := os.Stdin.Read(b[:])
		if err != nil {
			p.out.Println()
			return "", fmt.Errorf("failed to read input: %w", err)
		}

		switch {
		case b[0] == '\r' || b[0] == '\n':
			_ = term.Restore(stdinFd, oldState)

			p.out.Println()

			return string(buf), nil
		case b[0] == 0x03: // Ctrl+C
			_ = term.Restore(stdinFd, oldState)

			p.out.Println()

			proc, _ := os.FindProcess(os.Getpid())
			_ = proc.Signal(os.Interrupt)

			return "", fmt.Errorf("interrupted")
		case b[0] == 127 || b[0] == 0x08: // Backspace / Delete
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]

				p.out.Print("\b \b")
			}
		case b[0] >= 32: // Printable character
			buf = append(buf, b[0])

			p.out.Print("*")
		}
	}
}

// Select prompts the user to select from a list of options.
func (p *Prompter) Select(message string, options []string) (int, error) {
	p.out.Println(message)

	for i, opt := range options {
		p.out.Print("  [%d] %s\n", i+1, opt)
	}

	p.out.Println()

	for {
		p.out.Print("Select [1-%d]: ", len(options))

		input, err := p.reader.ReadString('\n')
		if err != nil {
			return -1, fmt.Errorf("failed to read input: %w", err)
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		num, err := strconv.Atoi(input)
		if err != nil || num < 1 || num > len(options) {
			p.out.Warning("Invalid selection. Please enter a number between 1 and %d", len(options))
			continue
		}

		return num - 1, nil
	}
}

// APIKey prompts the user for an API key.
func APIKey(out *output.Writer) (string, error) {
	out.Print("Enter your API key: ")

	reader := bufio.NewReader(os.Stdin)

	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}

	return strings.TrimSpace(input), nil
}
