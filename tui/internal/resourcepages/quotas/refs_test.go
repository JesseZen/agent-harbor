package quotas

import (
	"strings"
	"testing"
)

func TestDeleteBlockedByTargetQuotaGroupRef(t *testing.T) {
	page, draft := newTestPage(t)
	before := len(draft.LocalCommand().QuotaGroups)

	page.SelectID("quota-default")
	blocked, paths := page.DeleteBlocked("quota-default")
	if !blocked {
		t.Fatal("expected delete blocked by target quota_group_id")
	}
	joined := strings.Join(paths, "\n")
	if !strings.Contains(joined, "targets[target-1].quota_group_id") {
		t.Fatalf("expected target path, got %v", paths)
	}
	if len(draft.LocalCommand().QuotaGroups) != before {
		t.Fatal("blocked delete must not mutate draft")
	}

	blocked, _ = page.TryDelete()
	if !blocked {
		t.Fatal("TryDelete should also block")
	}
	view := page.View()
	if !strings.Contains(view, "targets[target-1].quota_group_id") {
		t.Fatalf("dependency dialog missing path:\n%s", view)
	}
	if !strings.Contains(view, "1 inbound reference") {
		t.Fatalf("dependency dialog missing inbound count:\n%s", view)
	}
	if !strings.Contains(view, "will remain after delete") {
		t.Fatalf("dependency dialog missing remaining count:\n%s", view)
	}
}

func TestDeleteUnreferencedQuota(t *testing.T) {
	page, draft := newTestPage(t)
	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id":                  "quota-free",
		"name":                "free",
		"rpm":                 "60",
		"max_concurrency":     "2",
		"foreground_capacity": "1",
		"background_capacity": "1",
		"foreground_weight":   "9",
		"background_weight":   "1",
		"queue_timeout_ms":    "30000",
	})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("create: %v", err)
	}
	page.SelectID("quota-free")
	if blocked, paths := page.DeleteBlocked("quota-free"); blocked {
		t.Fatalf("quota-free should delete, paths=%v", paths)
	}
	page.SelectID("quota-free")
	if blocked, _ := page.TryDelete(); blocked {
		t.Fatal("unreferenced quota should open confirm dialog")
	}
	view := page.View()
	if !strings.Contains(view, "Confirm delete quota group quota-free") {
		t.Fatalf("expected confirm dialog:\n%s", view)
	}
	if !strings.Contains(view, "0 inbound references") {
		t.Fatalf("confirm dialog missing inbound count:\n%s", view)
	}
	page.ConfirmDelete()
	for _, q := range draft.LocalCommand().QuotaGroups {
		if q.Id == "quota-free" {
			t.Fatal("quota-free still in draft")
		}
	}
}
