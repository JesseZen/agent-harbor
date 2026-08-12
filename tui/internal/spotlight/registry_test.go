package spotlight_test

import (
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/i18n"
	"github.com/asheshgoplani/agent-deck/internal/spotlight"
)

func TestRootItemsIncludeLanguageAndTheme(t *testing.T) {
	i18n.SetLocale("en")
	items := spotlight.RootItems()
	found := map[string]bool{}
	for _, item := range items {
		found[item.ID] = true
		if item.ID == "language" && !item.OpensMenu {
			t.Fatal("language should open submenu")
		}
		if item.Shortcut == "" || strings.HasPrefix(item.Shortcut, "/") == false {
			t.Fatalf("%s shortcut = %q, want Grok-style /command", item.ID, item.Shortcut)
		}
		if len(item.Keys) > 0 {
			t.Fatalf("%s should not expose single-key Keys on the right; got %v", item.ID, item.Keys)
		}
	}
	for _, id := range []string{"language", "theme", "quit", "reload"} {
		if !found[id] {
			t.Fatalf("missing %s", id)
		}
	}
}

func TestLanguageItemsLocalized(t *testing.T) {
	i18n.SetLocale("zh-CN")
	defer i18n.SetLocale("en")
	items := spotlight.LanguageItems("zh-CN")
	if len(items) < 2 {
		t.Fatalf("expected locales, got %d", len(items))
	}
	var names []string
	for _, item := range items {
		names = append(names, item.LabelColumns[0])
		if item.ID == "zh-CN" && item.Widget == 0 {
			// RadioSelected is non-zero
		}
	}
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "简体中文") || !strings.Contains(joined, "English") {
		t.Fatalf("names = %v", names)
	}
}
