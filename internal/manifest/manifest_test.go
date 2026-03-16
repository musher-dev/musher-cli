package manifest

import "testing"

func TestMapAssetType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind string
		want string
	}{
		// Exact matches
		{"skill", "skill"},
		{"agent", "agent_definition"},
		{"agent_definition", "agent_definition"},
		{"tool", "tool_config"},
		{"tool_config", "tool_config"},
		{"prompt", "prompt"},
		{"config", "config"},

		// Case insensitivity
		{"Skill", "skill"},
		{"AGENT", "agent_definition"},
		{"Tool_Config", "tool_config"},
		{"PROMPT", "prompt"},

		// Whitespace trimming
		{" skill ", "skill"},
		{"  agent  ", "agent_definition"},

		// Fallback to other
		{"", "other"},
		{"unknown", "other"},
		{"workflow", "other"},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			t.Parallel()

			if got := MapAssetType(tt.kind); got != tt.want {
				t.Errorf("MapAssetType(%q) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}
