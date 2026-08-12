package app

import (
	"testing"
)

func TestHelpContentPutsCurrentTabBeforeGlobal(t *testing.T) {
	content := helpContentForTab(tabRoutes)
	if len(content.Sections) != 2 {
		t.Fatalf("help sections = %d, want current tab plus global", len(content.Sections))
	}
	if content.Sections[0].Title != "ROUTES" {
		t.Fatalf("first help section = %q, want ROUTES", content.Sections[0].Title)
	}
	if content.Sections[1].Title != "GLOBAL" {
		t.Fatalf("last help section = %q, want GLOBAL", content.Sections[1].Title)
	}
}
