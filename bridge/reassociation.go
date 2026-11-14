package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type reassociationDecision struct {
	Original  json.RawMessage       `json:"original_record,omitempty"`
	At        time.Time             `json:"at"`
	Reference herdrIdentity         `json:"reference"`
	Reason    string                `json:"reason"`
	Accepted  bool                  `json:"accepted"`
	Previous  *conversationEvidence `json:"previous,omitempty"`
}

func (harborService) Reassociate(a sessionApproval, ref herdrIdentity, reason string) error {
	if strings.TrimSpace(reason) == "" || strings.ContainsAny(reason, "\r\n\x00") {
		return errors.New("review reason required")
	}
	if ref.Source == "" || ref.Agent != a.Session.CLI || ref.Kind == "" || ref.Value == "" {
		return errors.New("complete upstream reference required; inspect Herdr identity or explicitly review a compatibility reference")
	}
	return changeSessionRecord(a.Session.ID, func(s session) (*session, error) {
		if !sameSession(s, a.Session) {
			return nil, errors.New("session changed; review its details again")
		}
		if processGroupActive(s.ProcessGroup) {
			return nil, errors.New("stop the session before reassociation")
		}
		if err := neutralReferenceCheck(s); err != nil {
			return nil, err
		}
		if s.CodexSessionID != "" && (ref.Kind != "id" || ref.Value != s.CodexSessionID) {
			return nil, errors.New("conflicting legacy reference; preserve the original and review the supplied reference")
		}
		if s.Conversation != nil && s.Conversation.Reference != ref {
			return nil, errors.New("conflicting neutral reference; review original evidence")
		}
		original, err := reviewBaseline(s)
		if err != nil {
			return nil, err
		}
		s.Reassociations = append(s.Reassociations, reassociationDecision{Original: original, At: time.Now().UTC(), Reference: ref, Reason: reason, Accepted: a.Confirmed, Previous: s.Conversation})
		s.RunToken = ""
		s.Recovery = "needs_review"
		s.IdentityWarning = "Reassociation rejected; inspect Herdr identity and use reassociate to review the original conversation and connection."
		if a.Confirmed {
			s.Version = 3
			s.Conversation = &conversationEvidence{Reference: ref, Provenance: "reviewed-compatibility", Status: "reviewed"}
			s.Recovery = "ready"
			s.ReportedCodexID = ""
			s.CandidateIDs = nil
			s.Candidates = nil
			s.IdentityWarning = "Manually reviewed compatibility reference; upstream identity remains unproven. Use resume --development-query and review conversation, login and connection uncertainty."
		}
		return &s, nil
	})
}
func reassociateCommand(args []string) error {
	if len(args) != 3 {
		return errors.New("usage: reassociate SESSION_ID REFERENCE_JSON REVIEW_REASON; supply the full reference explicitly")
	}
	var ref herdrIdentity
	if json.Unmarshal([]byte(args[1]), &ref) != nil {
		return errors.New("invalid reference JSON")
	}
	view, err := appService.Get(args[0])
	if err != nil {
		return err
	}
	fmt.Printf("Review %q: original conversation and connection are preserved. Manual compatibility approval is not official hook evidence. Type yes to accept or reject to retain needs review: ", view.Name)
	var answer string
	if _, err := fmt.Scanln(&answer); err != nil || answer != "yes" && answer != "reject" {
		return errors.New("reassociation cancelled")
	}
	return appService.Reassociate(sessionApproval{Session: view.session, Confirmed: answer == "yes"}, ref, args[2])
}

// Existing serialized snapshots remain untouched. New snapshots retain the
// complete reviewed record without recursively embedding the audit history.
func reviewBaseline(s session) (json.RawMessage, error) {
	s.Reassociations = nil
	return json.Marshal(s)
}
