package observations

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"
)

const MaxCopyBytes = 64 * 1024

func observationColumns() []resourceview.Column {
	return []resourceview.Column{
		{Title: "TIME", MinWidth: 10, Priority: 0},
		{Title: "KIND", MinWidth: 8, Priority: 0},
		{Title: "OUTCOME", MinWidth: 10, Priority: 2},
		{Title: "SESSION", MinWidth: 10, Priority: 3},
		{Title: "ROUTE", MinWidth: 10, Priority: 4},
		{Title: "TARGET", MinWidth: 10, Priority: 5},
		{Title: "DURATION", MinWidth: 8, Priority: 6},
	}
}

func rowsFromObservations(observations []backend.Observation) []resourceview.Row {
	rows := make([]resourceview.Row, 0, len(observations))
	for _, obs := range observations {
		rows = append(rows, resourceview.Row{
			ID: obs.ID,
			Cells: []string{
				formatTime(obs.OccurredAt),
				obs.Type,
				outcomeLabel(obs),
				dashIfEmpty(obs.SessionID),
				dashIfEmpty(obs.RouteID),
				dashIfEmpty(obs.TargetID),
				"-",
			},
		})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows
}

func formatTime(at time.Time) string {
	if at.IsZero() {
		return "-"
	}
	return at.Local().Format("15:04:05")
}

func outcomeLabel(obs backend.Observation) string {
	if obs.SemanticOutcome != "" {
		return obs.SemanticOutcome
	}
	if obs.PolicyDecision != "" {
		return obs.PolicyDecision
	}
	return "-"
}

func dashIfEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func detailText(obs backend.Observation) string {
	lines := []string{
		fmt.Sprintf("id: %s", obs.ID),
		fmt.Sprintf("type: %s", obs.Type),
		fmt.Sprintf("occurred_at: %s", obs.OccurredAt.Local().Format(time.RFC3339)),
		fmt.Sprintf("session_id: %s", dashIfEmpty(obs.SessionID)),
		fmt.Sprintf("route_id: %s", dashIfEmpty(obs.RouteID)),
		fmt.Sprintf("target_id: %s", dashIfEmpty(obs.TargetID)),
		fmt.Sprintf("quota_group_id: %s", dashIfEmpty(obs.QuotaGroupID)),
		fmt.Sprintf("semantic_outcome: %s", dashIfEmpty(obs.SemanticOutcome)),
		fmt.Sprintf("policy_decision: %s", dashIfEmpty(obs.PolicyDecision)),
		fmt.Sprintf("decision_reason: %s", obs.DecisionReason),
		fmt.Sprintf("snapshot_generation: %d", obs.SnapshotGeneration),
		fmt.Sprintf("duration: -"),
	}
	return strings.Join(lines, "\n")
}

func boundCopyText(text string) string {
	if len(text) <= MaxCopyBytes {
		return text
	}
	marker := "\n...(truncated)"
	keep := MaxCopyBytes - len(marker)
	if keep < 0 {
		keep = 0
	}
	truncated := text[:keep]
	for len(truncated) > 0 && !utf8.ValidString(truncated) {
		_, size := utf8.DecodeLastRuneInString(truncated)
		if size <= 0 {
			break
		}
		truncated = truncated[:len(truncated)-size]
	}
	return truncated + marker
}

func findObservation(observations []backend.Observation, id string) (backend.Observation, bool) {
	for _, obs := range observations {
		if obs.ID == id {
			return obs, true
		}
	}
	return backend.Observation{}, false
}
