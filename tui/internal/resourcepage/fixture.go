package resourcepage

import (
	"fmt"

	"github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"
)

func FixtureRows(n int) []resourceview.Row {
	rows := make([]resourceview.Row, 0, n)
	for index := 0; index < n; index++ {
		id := fmt.Sprintf("item-%03d", index+1)
		rows = append(rows, resourceview.Row{
			ID:    id,
			Cells: []string{id, fmt.Sprintf("name-%d", index+1), "healthy"},
		})
	}
	return rows
}
