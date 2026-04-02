package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/musher-dev/musher-cli/internal/client"
)

func TestDetailScreenInit(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	screen := newDetailScreen(context.Background(), &mockSearcher{}, nil, nil, nil, "acme", "bundle", &sty, &keys)

	cmd := screen.Init()
	if cmd == nil {
		t.Error("expected non-nil cmd from Init")
	}

	if !screen.loading {
		t.Error("expected loading = true after Init")
	}
}

func TestDetailScreenDetailResult(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	screen := newDetailScreen(context.Background(), &mockSearcher{}, nil, nil, nil, "acme", "bundle", &sty, &keys)

	detail := &client.HubBundleDetail{
		HubBundleSummary: client.HubBundleSummary{
			DisplayName:    "My Bundle",
			Slug:           "bundle",
			Summary:        "A test bundle",
			LatestVersion:  "2.0.0",
			StarsCount:     10,
			DownloadsTotal: 50,
			AssetTypes:     []string{"skill", "prompt"},
			License:        "MIT",
			Publisher: client.HubPublisher{
				Handle:    "acme",
				TrustTier: "verified",
			},
		},
		Description: "Full description",
	}

	updated, _ := screen.Update(detailResultMsg{detail: detail})
	detailScr, ok := updated.(*detailScreen)

	if !ok {
		t.Fatal("expected *detailScreen from Update")
	}

	if detailScr.loading {
		t.Error("expected loading = false after result")
	}

	if detailScr.detail == nil {
		t.Fatal("expected detail to be set")
	}

	if detailScr.detail.DisplayName != "My Bundle" {
		t.Errorf("displayName = %q, want %q", detailScr.detail.DisplayName, "My Bundle")
	}
}

func TestDetailScreenDetailError(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	screen := newDetailScreen(context.Background(), &mockSearcher{}, nil, nil, nil, "acme", "bundle", &sty, &keys)

	testErr := errors.New("network error")
	updated, _ := screen.Update(detailErrorMsg{err: testErr})
	detailScr, ok := updated.(*detailScreen)

	if !ok {
		t.Fatal("expected *detailScreen from Update")
	}

	if detailScr.loading {
		t.Error("expected loading = false after error")
	}

	if detailScr.err == nil {
		t.Error("expected err to be set")
	}
}

func TestDetailScreenView(t *testing.T) {
	t.Parallel()

	t.Run("loading state", func(t *testing.T) {
		t.Parallel()

		sty := newStyles(true)
		keys := defaultKeyMap()
		screen := newDetailScreen(context.Background(), &mockSearcher{}, nil, nil, nil, "acme", "bundle", &sty, &keys)

		view := screen.View()
		if !strings.Contains(view, "Loading") {
			t.Errorf("loading view should contain 'Loading', got %q", view)
		}
	})

	t.Run("error state", func(t *testing.T) {
		t.Parallel()

		sty := newStyles(true)
		keys := defaultKeyMap()
		screen := newDetailScreen(context.Background(), &mockSearcher{}, nil, nil, nil, "acme", "bundle", &sty, &keys)
		screen.loading = false
		screen.err = errors.New("test error")

		view := screen.View()
		if !strings.Contains(view, "Error") {
			t.Errorf("error view should contain 'Error', got %q", view)
		}
	})

	t.Run("detail state with data", func(t *testing.T) {
		t.Parallel()

		sty := newStyles(true)
		keys := defaultKeyMap()
		screen := newDetailScreen(context.Background(), &mockSearcher{}, nil, nil, nil, "acme", "bundle", &sty, &keys)
		screen.loading = false
		screen.detail = &client.HubBundleDetail{
			HubBundleSummary: client.HubBundleSummary{
				DisplayName:    "My Bundle",
				Summary:        "A summary",
				LatestVersion:  "1.0.0",
				AssetTypes:     []string{"skill"},
				StarsCount:     5,
				DownloadsTotal: 20,
				License:        "Apache-2.0",
				Publisher: client.HubPublisher{
					Handle:    "acme",
					TrustTier: "verified",
				},
			},
		}

		view := screen.View()

		if !strings.Contains(view, "My Bundle") {
			t.Error("view should contain display name")
		}

		if !strings.Contains(view, "1.0.0") {
			t.Error("view should contain version")
		}

		if !strings.Contains(view, "Apache-2.0") {
			t.Error("view should contain license")
		}

		if !strings.Contains(view, "A summary") {
			t.Error("view should contain summary")
		}

		if !strings.Contains(view, "skill") {
			t.Error("view should contain asset types")
		}
	})

	t.Run("panel layout with width", func(t *testing.T) {
		t.Parallel()

		sty := newStyles(true)
		keys := defaultKeyMap()
		screen := newDetailScreen(context.Background(), &mockSearcher{}, nil, nil, nil, "acme", "bundle", &sty, &keys)
		screen.loading = false
		screen.width = 80
		screen.height = 30
		screen.detail = &client.HubBundleDetail{
			HubBundleSummary: client.HubBundleSummary{
				DisplayName:   "My Bundle",
				Summary:       "A summary",
				LatestVersion: "1.0.0",
				Publisher: client.HubPublisher{
					Handle:    "acme",
					TrustTier: "verified",
				},
			},
		}

		view := screen.View()

		if !strings.Contains(view, "My Bundle") {
			t.Error("panel view should contain display name")
		}

		if !strings.Contains(view, "acme/bundle") {
			t.Error("panel view should contain breadcrumb")
		}
	})

	t.Run("nil detail shows empty", func(t *testing.T) {
		t.Parallel()

		sty := newStyles(true)
		keys := defaultKeyMap()
		screen := newDetailScreen(context.Background(), &mockSearcher{}, nil, nil, nil, "acme", "bundle", &sty, &keys)
		screen.loading = false

		view := screen.View()
		if view == "" {
			t.Error("expected non-empty view even with nil detail")
		}
	})
}

func TestDetailScreenPanelWidth(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	screen := newDetailScreen(context.Background(), &mockSearcher{}, nil, nil, nil, "acme", "bundle", &sty, &keys)

	// Two-panel layout.
	screen.width = 120
	pw := screen.panelWidth()

	if pw <= 0 {
		t.Errorf("two-panel panelWidth() = %d, want > 0", pw)
	}

	// Compact layout.
	screen.width = 50
	pw = screen.panelWidth()

	if pw <= 0 {
		t.Errorf("compact panelWidth() = %d, want > 0", pw)
	}

	// Minimal layout.
	screen.width = 30
	pw = screen.panelWidth()

	if pw < 20 {
		t.Errorf("minimal panelWidth() = %d, want >= 20", pw)
	}
}

func TestDetailScreenFetchDetail(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	detail := &client.HubBundleDetail{
		HubBundleSummary: client.HubBundleSummary{
			DisplayName: "Test",
		},
	}
	searcher := &mockSearcher{detailResult: detail}
	screen := newDetailScreen(context.Background(), searcher, nil, nil, nil, "acme", "bundle", &sty, &keys)

	cmd := screen.fetchDetail()
	if cmd == nil {
		t.Fatal("expected non-nil cmd from fetchDetail")
	}

	msg := cmd()

	resultMsg, ok := msg.(detailResultMsg)
	if !ok {
		t.Fatalf("expected detailResultMsg, got %T", msg)
	}

	if resultMsg.detail.DisplayName != "Test" {
		t.Errorf("detail.DisplayName = %q, want %q", resultMsg.detail.DisplayName, "Test")
	}
}

func TestDetailScreenKeyHandling(t *testing.T) {
	t.Parallel()

	t.Run("quit key", func(t *testing.T) {
		t.Parallel()

		sty := newStyles(true)
		keys := defaultKeyMap()
		screen := newDetailScreen(context.Background(), &mockSearcher{}, nil, nil, nil, "acme", "bundle", &sty, &keys)

		_, cmd := screen.Update(tea.KeyPressMsg{Code: -1, Text: "q"})
		if cmd == nil {
			t.Error("expected non-nil cmd for quit key")
		}
	})

	t.Run("back key", func(t *testing.T) {
		t.Parallel()

		sty := newStyles(true)
		keys := defaultKeyMap()
		screen := newDetailScreen(context.Background(), &mockSearcher{}, nil, nil, nil, "acme", "bundle", &sty, &keys)

		_, cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		if cmd == nil {
			t.Error("expected non-nil cmd for esc key")
		}

		msg := cmd()
		if _, ok := msg.(popScreenMsg); !ok {
			t.Errorf("expected popScreenMsg, got %T", msg)
		}
	})

	t.Run("enter with detail pushes load screen", func(t *testing.T) {
		t.Parallel()

		sty := newStyles(true)
		keys := defaultKeyMap()
		screen := newDetailScreen(context.Background(), &mockSearcher{}, nil, nil, nil, "acme", "bundle", &sty, &keys)
		screen.loading = false
		screen.detail = &client.HubBundleDetail{
			HubBundleSummary: client.HubBundleSummary{
				LatestVersion: "3.0.0",
			},
		}

		_, cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if cmd == nil {
			t.Fatal("expected non-nil cmd for enter key")
		}

		msg := cmd()
		pushMsg, ok := msg.(pushScreenMsg)

		if !ok {
			t.Fatalf("expected pushScreenMsg, got %T", msg)
		}

		loadScr, ok := pushMsg.screen.(*loadScreen)
		if !ok {
			t.Fatalf("expected *loadScreen, got %T", pushMsg.screen)
		}

		if loadScr.namespace != "acme" {
			t.Errorf("namespace = %q, want %q", loadScr.namespace, "acme")
		}

		if loadScr.version != "3.0.0" {
			t.Errorf("version = %q, want %q", loadScr.version, "3.0.0")
		}
	})

	t.Run("enter without detail is no-op", func(t *testing.T) {
		t.Parallel()

		sty := newStyles(true)
		keys := defaultKeyMap()
		screen := newDetailScreen(context.Background(), &mockSearcher{}, nil, nil, nil, "acme", "bundle", &sty, &keys)
		screen.loading = false

		_, cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if cmd != nil {
			t.Error("expected nil cmd when detail is nil")
		}
	})

	t.Run("window size updates dimensions", func(t *testing.T) {
		t.Parallel()

		sty := newStyles(true)
		keys := defaultKeyMap()
		screen := newDetailScreen(context.Background(), &mockSearcher{}, nil, nil, nil, "acme", "bundle", &sty, &keys)

		updated, _ := screen.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		detailScr := updated.(*detailScreen)

		if detailScr.width != 120 {
			t.Errorf("width = %d, want 120", detailScr.width)
		}

		if detailScr.height != 40 {
			t.Errorf("height = %d, want 40", detailScr.height)
		}
	})
}
