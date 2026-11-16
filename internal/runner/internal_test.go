package runner

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestRedactorRemovesLongestFirst(t *testing.T) {
	t.Parallel()

	r := NewRedactor("abc", "", "abcdef")
	r.Add("later")

	got := r.Redact("x abcdef y abc z later")
	if got != "x [REDACTED] y [REDACTED] z [REDACTED]" {
		t.Fatalf("Redact = %q", got)
	}
}

func TestSplitMessageKeepsRunesWhole(t *testing.T) {
	t.Parallel()

	msg := strings.Repeat("é", maxMessageBytes) // 2 bytes each
	parts := splitMessage(msg)

	if len(parts) != 2 {
		t.Fatalf("parts = %d, want 2", len(parts))
	}

	for _, p := range parts {
		if len(p) > maxMessageBytes || !utf8.ValidString(p) {
			t.Fatalf("part of %d bytes, valid %v", len(p), utf8.ValidString(p))
		}
	}

	if strings.Join(parts, "") != msg {
		t.Fatal("splitting lost bytes")
	}
}

func TestTruncateDetail(t *testing.T) {
	t.Parallel()

	got := truncateDetail(strings.Repeat("x", 5000))
	if len(got) > maxDetailBytes || !strings.HasSuffix(got, "[truncated]") {
		t.Fatalf("len %d, %q", len(got), got[len(got)-20:])
	}
}

func TestTokenExpiry(t *testing.T) {
	t.Parallel()

	at, ok := TokenExpiry("mush_run_kid.0123456789abcdef0123456789abcdef.2.1900000000.c2ln")
	if !ok || !at.Equal(time.Unix(1900000000, 0)) {
		t.Fatalf("TokenExpiry = %v, %v", at, ok)
	}

	for _, bad := range []string{"", "mush_run_x", "mush_run_a.b.c.notanumber.e", "mush_abc.def"} {
		if _, ok := TokenExpiry(bad); ok {
			t.Fatalf("TokenExpiry(%q) read an expiry", bad)
		}
	}
}

func TestResolveID(t *testing.T) {
	t.Parallel()

	const id = "6b3f0f8e-5c1a-4d7e-9a5e-0d6c1c1f0a01"

	if got, err := ResolveID("", id, "build"); err != nil || got.String() != id {
		t.Fatalf("from inputs: %v, %v", got, err)
	}

	if got, err := ResolveID(id, id, "build"); err != nil || got.String() != id {
		t.Fatalf("both agree: %v, %v", got, err)
	}

	if _, err := ResolveID(id, "8c2d1e7a-3b4f-4a6e-8d9c-1e2f3a4b5c02", "build"); err == nil {
		t.Fatal("a flag and inputs file that disagree were accepted")
	}

	if _, err := ResolveID("", "", "build"); err == nil {
		t.Fatal("no id was accepted")
	}
}

func TestLocalPath(t *testing.T) {
	t.Parallel()

	for p, want := range map[string]bool{
		".": true, "app": true, "a/b/../c": true, "Dockerfile": true,
		"": false, "/abs": false, "..": false, "../x": false, "a/../../x": false, `a\b`: false,
	} {
		if got := localPath(p); got != want {
			t.Errorf("localPath(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestGrantRefreshLead(t *testing.T) {
	t.Parallel()

	now := time.Now()

	for life, lead := range map[time.Duration]time.Duration{
		15 * time.Minute: 2 * time.Minute,
		3 * time.Minute:  time.Minute,
		6 * time.Second:  5 * time.Second,
	} {
		g := &PushGrant{obtainedAt: now, ExpiresAt: now.Add(life)}
		if got := g.ExpiresAt.Sub(g.refreshAt()); got != lead {
			t.Errorf("a %s grant renews %s before it ends, want %s", life, got, lead)
		}
	}
}

func TestGitHubAPIBase(t *testing.T) {
	t.Parallel()

	if got := gitHubAPIBase("https://github.com/acme/web.git"); got != "https://api.github.com" {
		t.Fatalf("github.com: %s", got)
	}

	if got := gitHubAPIBase("https://ghe.acme.test/acme/web.git"); got != "https://ghe.acme.test/api/v3" {
		t.Fatalf("enterprise: %s", got)
	}
}
