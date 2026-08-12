package targets

import (
	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/secretinput"
)

// Deps are host-injected collaborators for the Targets owner-tab surface.
//
// TICKET-011 host wiring must provide:
//   - Draft: shared ConfigDraft owned by the host publish/discard loop
//   - StageClient: Core ProviderSecretStage HTTP client for Credentials Replace
//   - TargetStatus: runtime TargetStatus snapshot projection for HEALTH/ELIGIBLE
//     (optional; nil blanks those columns until status is available)
//   - Scope: resourceview scope label (empty defaults to "all")
//
// TICKET-011 host orchestration checklist (call these Page APIs):
//   - DiscardOwnedStages(ctx) error: on publish success / global discard —
//     deletes stager-owned stages and draft-held replace(stage_id) stages;
//     returns/surfaces cleanup status on DELETE failure (retry via same API)
//   - NoteStageLoss(credentialID, code): when publish/poll reports stage loss
//   - HandleOperationUnknown(outcome, submittedReplaceIDs): operation_unknown
//   - ApplyCleanupPending(): secret_cleanup_pending mutation status
//   - ApplyConflictState(): generation conflict UI
//   - HasUnstaged(): before global Publish — true blocks publish (also
//     IntentPublish is suppressed and status is set when unstaged)
//   - LastIntent() == IntentPublish → host runs the publish loop
//   - Refresh HEALTH/ELIGIBLE via TargetStatus provider + Refresh()
//
// Do not register this page inside app/ from this package — composition belongs
// to TICKET-011.
type Deps struct {
	Draft        *configdraft.Draft
	StageClient  secretinput.StageHTTPClient
	TargetStatus TargetStatusProvider
	Scope        string
}

// New constructs the Targets owner-tab surface bound to the shared draft.
// Default secondary kind is Upstreams (then Targets, Endpoints, Credentials).
// Intended for TICKET-011 composition (do not register in app here).
func New(deps Deps) *Page {
	scope := deps.Scope
	if scope == "" {
		scope = "all"
	}
	page := &Page{
		draft:  deps.Draft,
		client: deps.StageClient,
		status: deps.TargetStatus,
		scope:  scope,
		kind:   KindUpstreams,
		width:  120,
		height: 30,
	}
	page.rebuildTable()
	page.Refresh()
	return page
}
