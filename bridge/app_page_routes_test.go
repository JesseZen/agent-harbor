package main

import "testing"

func TestHelpRestoresParentScroll(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.screen = "detail"
	m.scroll = 7
	n, _ := m.Update(uiKey("?"))
	m = n.(harborModel)
	if m.screen != "help" || m.secondaryHelpScroll != 7 || m.scroll != 0 {
		t.Fatal("help lost parent scroll")
	}
	n, _ = m.Update(uiKey("esc"))
	m = n.(harborModel)
	if m.screen != "detail" || m.scroll != 7 {
		t.Fatal("help did not restore parent", m.screen, m.scroll)
	}
}
func TestInstanceRefreshPreservesSelectionUnlessContextOverrides(t *testing.T) {
	for _, context := range []string{"", "b"} {
		m := newHarborModel(&fakeApplicationService{})
		m.screen = "magpie"
		m.contextInstance = context
		a := instanceReport{magpieInstance: magpieInstance{ID: "a", Name: "A"}}
		b := instanceReport{magpieInstance: magpieInstance{ID: "b", Name: "B"}}
		m.reports = []instanceReport{a, b}
		m.instanceCursor = 0
		n, _ := m.Update(appInstances{generation: m.generation, reports: []instanceReport{b, a}})
		m = n.(harborModel)
		want := "a"
		if context != "" {
			want = context
		}
		if m.searchSelectedID() != want {
			t.Fatal("wrong instance", context, m.searchSelectedID())
		}
	}
}
