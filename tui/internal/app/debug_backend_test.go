package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestDebugBackendLoadsEmptyUI(t *testing.T) {
	source := NewDebugBackend()
	snapshot, err := source.LoadSessions(context.Background())
	if err != nil {
		t.Fatalf("LoadSessions: %v", err)
	}
	if snapshot.Identity.Binary != "debug-ui" || len(snapshot.Sessions) != 0 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	config, err := source.LoadConfig(context.Background())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(config.MutableConfig.Routes) != 0 || len(config.MutableConfig.Targets) != 0 {
		t.Fatalf("expected empty config, got %#v", config.MutableConfig)
	}
	if _, err := source.PatchConfig(context.Background(), generated.PatchConfigCommand{}); !errors.Is(err, backend.ErrUnsupported) {
		t.Fatalf("PatchConfig error = %v, want ErrUnsupported", err)
	}

	model := New(source)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	model = updated.(*Model)
	updated, _ = model.Update(model.loadAll(false)())
	model = updated.(*Model)
	view := ansi.Strip(model.View())
	for _, needle := range []string{"Overview", "Sessions", "Upstreams", "Traffic Rules"} {
		if !strings.Contains(view, needle) {
			t.Fatalf("debug UI omitted %q:\n%s", needle, view)
		}
	}
}
