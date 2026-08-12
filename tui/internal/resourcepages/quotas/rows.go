package quotas

import (
	"fmt"
	"strconv"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"
)

func quotaColumns() []resourceview.Column {
	return []resourceview.Column{
		{Title: "ID", MinWidth: 10, Priority: 0},
		{Title: "NAME", MinWidth: 10, Priority: 0},
		{Title: "RPM", MinWidth: 6, Priority: 2},
		{Title: "MAX", MinWidth: 6, Priority: 3},
		{Title: "FOREGROUND", MinWidth: 10, Priority: 4},
		{Title: "BACKGROUND", MinWidth: 10, Priority: 5},
		{Title: "NEXT", MinWidth: 10, Priority: 6},
	}
}

func rowsFor(draft *configdraft.Draft, runtime map[string]backend.QuotaGroup) []resourceview.Row {
	groups := draft.LocalCommand().QuotaGroups
	rows := make([]resourceview.Row, 0, len(groups))
	for _, group := range groups {
		id := string(group.Id)
		status, hasStatus := runtime[id]
		fg := strconv.Itoa(group.ForegroundCapacity)
		bg := strconv.Itoa(group.BackgroundCapacity)
		next := "-"
		rpm := group.Rpm
		max := group.MaxConcurrency
		if hasStatus {
			fg = strconv.Itoa(status.ForegroundDepth)
			bg = strconv.Itoa(status.BackgroundDepth)
			rpm = status.RPM
			max = status.MaxConcurrency
			if !status.NextPermitAt.IsZero() {
				next = formatNext(status.NextPermitAt)
			}
		}
		rows = append(rows, resourceview.Row{
			ID: id,
			Cells: []string{
				id,
				group.Name,
				strconv.Itoa(rpm),
				strconv.Itoa(max),
				fg,
				bg,
				next,
			},
		})
	}
	return rows
}

func formatNext(at time.Time) string {
	return at.Local().Format("15:04:05")
}

func inboundRefs(draft *configdraft.Draft, id string) []string {
	cmd := draft.LocalCommand()
	var paths []string
	for _, target := range cmd.Targets {
		if string(target.QuotaGroupId) == id {
			paths = append(paths, fmt.Sprintf("targets[%s].quota_group_id", target.Id))
		}
	}
	return paths
}

func findQuota(cmd generated.MutableConfigCommand, id string) (generated.QuotaGroupConfig, bool) {
	for _, q := range cmd.QuotaGroups {
		if string(q.Id) == id {
			return q, true
		}
	}
	return generated.QuotaGroupConfig{}, false
}

func quotaExists(draft *configdraft.Draft, id string) bool {
	_, ok := findQuota(draft.LocalCommand(), id)
	return ok
}

func quotaCount(draft *configdraft.Draft) int {
	return len(draft.LocalCommand().QuotaGroups)
}
