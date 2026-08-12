package i18n_test

import (
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/i18n"
)

func TestTFallsBackToEnglish(t *testing.T) {
	i18n.SetLocale("en")
	got := i18n.T("spotlight.title")
	if got != "Spotlight" {
		t.Fatalf("got %q", got)
	}
}

func TestTUsesZhCN(t *testing.T) {
	i18n.SetLocale("zh-CN")
	defer i18n.SetLocale("en")
	if i18n.T("command.language.title") != "语言" {
		t.Fatalf("language title = %q", i18n.T("command.language.title"))
	}
	if i18n.T("command.theme.title") != "主题" {
		t.Fatalf("theme title = %q", i18n.T("command.theme.title"))
	}
	if i18n.T("spotlight.empty") != "无匹配命令" {
		t.Fatalf("empty = %q", i18n.T("spotlight.empty"))
	}
}

func TestDetectLocale(t *testing.T) {
	if got := i18n.DetectLocale(map[string]string{"LANG": "zh_CN.UTF-8"}); got != "zh-CN" {
		t.Fatalf("got %q", got)
	}
	if got := i18n.DetectLocale(map[string]string{"LANG": "en_US.UTF-8"}); got != "en" {
		t.Fatalf("got %q", got)
	}
	if got := i18n.DetectLocale(nil); got != "en" {
		t.Fatalf("got %q", got)
	}
}

func TestInterpolate(t *testing.T) {
	i18n.SetLocale("en")
	got := i18n.T("spotlight.footer", map[string]string{"tab": "Tab"})
	if got == "" || got == "spotlight.footer" {
		t.Fatalf("unexpected %q", got)
	}
}
