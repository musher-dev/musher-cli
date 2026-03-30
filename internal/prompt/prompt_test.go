package prompt

import (
	"bufio"
	"bytes"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/testutil"
)

func TestNew(t *testing.T) {
	t.Parallel()

	w, _, _ := testutil.TestOutputWriter(t)
	p := New(w)

	if p == nil {
		t.Fatal("New() returned nil")
	}

	if p.out != w {
		t.Error("Prompter.out not set to provided Writer")
	}

	if p.reader == nil {
		t.Error("Prompter.reader is nil")
	}
}

func TestAllIndices(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		n    int
		want []int
	}{
		{"zero", 0, []int{}},
		{"one", 1, []int{0}},
		{"three", 3, []int{0, 1, 2}},
		{"five", 5, []int{0, 1, 2, 3, 4}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := allIndices(tt.n)
			if len(got) != len(tt.want) {
				t.Fatalf("allIndices(%d) returned %d elements, want %d", tt.n, len(got), len(tt.want))
			}

			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("allIndices(%d)[%d] = %d, want %d", tt.n, i, v, tt.want[i])
				}
			}
		})
	}
}

func TestParseSelections(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		optionCount int
		wantIndices []int
		wantValid   bool
	}{
		{
			name:        "single valid",
			input:       "1",
			optionCount: 3,
			wantIndices: []int{0},
			wantValid:   true,
		},
		{
			name:        "multiple valid",
			input:       "1,3",
			optionCount: 3,
			wantIndices: []int{0, 2},
			wantValid:   true,
		},
		{
			name:        "with spaces",
			input:       " 1 , 2 , 3 ",
			optionCount: 3,
			wantIndices: []int{0, 1, 2},
			wantValid:   true,
		},
		{
			name:        "out of range high",
			input:       "4",
			optionCount: 3,
			wantValid:   false,
		},
		{
			name:        "out of range zero",
			input:       "0",
			optionCount: 3,
			wantValid:   false,
		},
		{
			name:        "non-numeric",
			input:       "abc",
			optionCount: 3,
			wantValid:   false,
		},
		{
			name:        "duplicate removed",
			input:       "1,1,2",
			optionCount: 3,
			wantIndices: []int{0, 1},
			wantValid:   true,
		},
		{
			name:        "empty parts skipped",
			input:       "1,,2",
			optionCount: 3,
			wantIndices: []int{0, 1},
			wantValid:   true,
		},
		{
			name:        "negative number",
			input:       "-1",
			optionCount: 3,
			wantValid:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, _, _ := testutil.TestOutputWriter(t)
			indices, valid := parseSelections(tt.input, tt.optionCount, w)

			if valid != tt.wantValid {
				t.Fatalf("valid = %v, want %v", valid, tt.wantValid)
			}

			if !valid {
				return
			}

			if len(indices) != len(tt.wantIndices) {
				t.Fatalf("got %d indices, want %d", len(indices), len(tt.wantIndices))
			}

			for i, v := range indices {
				if v != tt.wantIndices[i] {
					t.Errorf("indices[%d] = %d, want %d", i, v, tt.wantIndices[i])
				}
			}
		})
	}
}

// newTestPrompter creates a Prompter that reads from the given input string
// instead of os.Stdin, useful for testing interactive methods.
func newTestPrompter(t *testing.T, input string) (pr *Prompter, outBuf, errBuf *bytes.Buffer) {
	t.Helper()

	var w *output.Writer

	w, outBuf, errBuf = testutil.TestOutputWriter(t)
	pr = &Prompter{
		out:    w,
		reader: bufio.NewReader(strings.NewReader(input)),
	}

	return pr, outBuf, errBuf
}

func TestConfirm(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		defaultValue bool
		want         bool
	}{
		{"empty input returns default true", "\n", true, true},
		{"empty input returns default false", "\n", false, false},
		{"y confirms", "y\n", false, true},
		{"yes confirms", "yes\n", false, true},
		{"Y confirms", "Y\n", false, true},
		{"n denies", "n\n", true, false},
		{"no denies", "no\n", true, false},
		{"random input denies", "maybe\n", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, _, _ := newTestPrompter(t, tt.input)

			got, err := p.Confirm("Continue?", tt.defaultValue)
			if err != nil {
				t.Fatalf("Confirm: %v", err)
			}

			if got != tt.want {
				t.Errorf("Confirm() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSelect(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		options []string
		want    int
	}{
		{"first option", "1\n", []string{"a", "b", "c"}, 0},
		{"last option", "3\n", []string{"a", "b", "c"}, 2},
		{"middle option", "2\n", []string{"a", "b", "c"}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, _, _ := newTestPrompter(t, tt.input)

			got, err := p.Select("Pick one:", tt.options)
			if err != nil {
				t.Fatalf("Select: %v", err)
			}

			if got != tt.want {
				t.Errorf("Select() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSelectMultiple(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		options []string
		want    []int
	}{
		{"single", "2\n", []string{"a", "b", "c"}, []int{1}},
		{"multiple", "1,3\n", []string{"a", "b", "c"}, []int{0, 2}},
		{"all keyword", "all\n", []string{"a", "b", "c"}, []int{0, 1, 2}},
		{"ALL uppercase", "ALL\n", []string{"a", "b"}, []int{0, 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, _, _ := newTestPrompter(t, tt.input)

			got, err := p.SelectMultiple("Pick:", tt.options)
			if err != nil {
				t.Fatalf("SelectMultiple: %v", err)
			}

			if len(got) != len(tt.want) {
				t.Fatalf("got %d selections, want %d", len(got), len(tt.want))
			}

			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("selection[%d] = %d, want %d", i, v, tt.want[i])
				}
			}
		})
	}
}

func TestCanPromptNonTTY(t *testing.T) {
	// In a test environment, stdin is not a TTY, so CanPrompt should return false
	w, _, _ := testutil.TestOutputWriter(t)
	p := New(w)

	if p.CanPrompt() {
		t.Error("CanPrompt() = true in non-TTY test environment, want false")
	}
}

func TestCanPromptNoInput(t *testing.T) {
	w, _, _ := testutil.TestOutputWriter(t)
	w.NoInput = true

	p := New(w)

	if p.CanPrompt() {
		t.Error("CanPrompt() = true with NoInput, want false")
	}
}
