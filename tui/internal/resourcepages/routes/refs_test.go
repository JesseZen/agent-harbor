package routes

import (
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
)

func TestDeleteBackendSetBlockedByRoute(t *testing.T) {
	page, draft := newTestPage(t)
	before := len(draft.LocalCommand().BackendSets)

	page.SetKind(KindBackendSets)
	page.SelectID("bs-1")
	blocked, paths := page.TryDelete()
	if !blocked {
		t.Fatal("expected delete blocked by Route.backend_set_id")
	}
	joined := strings.Join(paths, "\n")
	if !strings.Contains(joined, "routes[") || !strings.Contains(joined, "backend_set_id") {
		t.Fatalf("expected route backend_set_id path, got %v", paths)
	}
	view := page.View()
	if !strings.Contains(view, "backend_set_id") {
		t.Fatalf("dependency dialog missing path:\n%s", view)
	}
	if len(draft.LocalCommand().BackendSets) != before {
		t.Fatal("blocked delete must not mutate draft")
	}
}

func TestDeleteRouteBlockedByClientProfile(t *testing.T) {
	page, draft := newTestPage(t)
	before := len(draft.LocalCommand().Routes)

	page.SetKind(KindRoutes)
	page.SelectID("route-1")
	blocked, paths := page.TryDelete()
	if !blocked {
		t.Fatal("expected delete blocked by ClientProfile.default_route_id")
	}
	joined := strings.Join(paths, "\n")
	if !strings.Contains(joined, "client_profiles[") || !strings.Contains(joined, "default_route_id") {
		t.Fatalf("expected profile default_route_id path, got %v", paths)
	}
	if len(draft.LocalCommand().Routes) != before {
		t.Fatal("blocked delete must not mutate draft")
	}
}

func TestDeleteTransformBlockedByProfile(t *testing.T) {
	page, draft := newTestPage(t)
	before := len(draft.LocalCommand().CompatibilityTransforms)

	page.SetKind(KindTransforms)
	page.SelectID("xf-1")
	blocked, paths := page.TryDelete()
	if !blocked {
		t.Fatal("expected delete blocked by compatibility_transform_ids")
	}
	joined := strings.Join(paths, "\n")
	if !strings.Contains(joined, "compatibility_transform_ids") {
		t.Fatalf("expected transform path, got %v", paths)
	}
	if len(draft.LocalCommand().CompatibilityTransforms) != before {
		t.Fatal("blocked delete must not mutate draft")
	}
}

func TestDeleteContentPolicyBlockedByRoute(t *testing.T) {
	page, draft := newTestPage(t)
	before := len(draft.LocalCommand().ContentPolicies)
	page.SetKind(KindContentPolicies)
	page.SelectID("cp-1")
	blocked, paths := page.TryDelete()
	if !blocked {
		t.Fatal("expected delete blocked by Route.content_policy_id")
	}
	if !strings.Contains(strings.Join(paths, "\n"), "content_policy_id") {
		t.Fatalf("paths=%v", paths)
	}
	if len(draft.LocalCommand().ContentPolicies) != before {
		t.Fatal("blocked delete must not mutate draft")
	}
}

func TestDeleteModelPolicyBlockedByRoute(t *testing.T) {
	page, draft := newTestPage(t)
	before := len(draft.LocalCommand().ModelPolicies)
	page.SetKind(KindModelPolicies)
	page.SelectID("mp-1")
	blocked, paths := page.TryDelete()
	if !blocked {
		t.Fatal("expected delete blocked by Route.model_policy_id")
	}
	if !strings.Contains(strings.Join(paths, "\n"), "model_policy_id") {
		t.Fatalf("paths=%v", paths)
	}
	if len(draft.LocalCommand().ModelPolicies) != before {
		t.Fatal("blocked delete must not mutate draft")
	}
}

func TestDeleteModelProjectionBlockedByProfile(t *testing.T) {
	page, draft := newTestPage(t)
	before := len(draft.LocalCommand().ModelProjections)
	page.SetKind(KindModelProjections)
	page.SelectID("proj-1")
	blocked, paths := page.TryDelete()
	if !blocked {
		t.Fatal("expected delete blocked by ClientProfile.model_projection_id")
	}
	if !strings.Contains(strings.Join(paths, "\n"), "model_projection_id") {
		t.Fatalf("paths=%v", paths)
	}
	if len(draft.LocalCommand().ModelProjections) != before {
		t.Fatal("blocked delete must not mutate draft")
	}
}

func TestInboundRefsListsEveryPath(t *testing.T) {
	page, draft := newTestPage(t)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Routes = append(cmd.Routes, sampleRoute("route-2"))
		cmd.Routes[1].BackendSetId = "bs-1"
		cmd.ClientProfiles = append(cmd.ClientProfiles, sampleProfile("profile-2", "route-1", "proj-1", "xf-1"))
	})
	page.Refresh()

	refs := InboundRefs(draft, KindBackendSets, "bs-1")
	if len(refs) < 2 {
		t.Fatalf("expected multiple inbound refs for bs-1, got %v", refs)
	}
	refs = InboundRefs(draft, KindRoutes, "route-1")
	if len(refs) < 2 {
		t.Fatalf("expected multiple profile refs for route-1, got %v", refs)
	}
}
