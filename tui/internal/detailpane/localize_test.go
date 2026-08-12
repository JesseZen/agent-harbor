package detailpane

import (
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/i18n"
	"github.com/charmbracelet/x/ansi"
)

func TestLocalizeZhCNSectionAndHint(t *testing.T) {
	i18n.SetLocale("en")
	t.Cleanup(func() { i18n.SetLocale("en") })

	i18n.SetLocale("zh-CN")
	view := ansi.Strip(Model{
		Title: NamedTitle("Target", "demo"),
		Sections: []Section{{
			Title: "Identity",
			Rows:  []Row{{Label: "name", Value: "demo"}},
		}},
		Width:  50,
		Height: 12,
	}.View())
	for _, want := range []string{"目标 · demo", "▸ 身份", "名称", "esc 关闭"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q in:\n%s", want, view)
		}
	}
}
