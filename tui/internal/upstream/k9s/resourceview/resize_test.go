package resourceview

import (
	"testing"
)

func TestResizeUpdatesHitTargets(t *testing.T) {
	table := New("Targets", []Column{
		{Title: "ID", MinWidth: 12, Priority: 0},
		{Title: "HEALTH", MinWidth: 9, Priority: 1},
		{Title: "ADAPTER", MinWidth: 10, Priority: 2},
	})
	table.SetScope("prod")
	table.SetRows([]Row{
		{ID: "target_a", Cells: []string{"target_a", "healthy", "openai"}},
		{ID: "target_b", Cells: []string{"target_b", "degraded", "anthropic"}},
	})

	for _, size := range []struct {
		width  int
		height int
	}{
		{160, 45},
		{120, 30},
		{90, 30},
		{70, 30},
	} {
		table.SetSize(size.width, size.height)
		_ = table.View()

		rowHit := table.HitTest(2, 2)
		if rowHit.Kind != HitRow {
			t.Fatalf("%dx%d row hit = %#v", size.width, size.height, rowHit)
		}

		headerHit := table.HitTest(14, 1)
		if headerHit.Kind != HitHeader {
			t.Fatalf("%dx%d header hit = %#v", size.width, size.height, headerHit)
		}

		footerHit := table.FooterFilterHit()
		if footerHit.Kind != HitFooterFilter {
			t.Fatalf("%dx%d footer hit = %#v", size.width, size.height, footerHit)
		}
	}
}

func TestResponsiveColumnsAtVerifiedSizes(t *testing.T) {
	table := New("Targets", []Column{
		{Title: "ID", MinWidth: 12, Priority: 0},
		{Title: "HEALTH", MinWidth: 9, Priority: 1},
		{Title: "ADAPTER", MinWidth: 10, Priority: 2},
	})
	table.SetRows([]Row{{ID: "target_a", Cells: []string{"target_a", "healthy", "openai"}}})

	table.SetSize(70, 30)
	narrow := table.View()
	if !containsAll(narrow, "ID", "target_a") {
		t.Fatalf("70x30 lost identity column:\n%s", narrow)
	}

	table.SetSize(160, 45)
	wide := table.View()
	if !containsAll(wide, "ID", "HEALTH", "ADAPTER") {
		t.Fatalf("160x45 missing columns:\n%s", wide)
	}
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if !contains(text, part) {
			return false
		}
	}
	return true
}

func contains(text, part string) bool {
	return len(part) == 0 || (len(text) >= len(part) && indexOf(text, part) >= 0)
}

func indexOf(text, part string) int {
	for index := 0; index+len(part) <= len(text); index++ {
		if text[index:index+len(part)] == part {
			return index
		}
	}
	return -1
}
