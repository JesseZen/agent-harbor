package routes

import (
	"fmt"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/detailpane"
	"github.com/asheshgoplani/agent-deck/internal/i18n"
)

func renderDetails(draft *configdraft.Draft, kind Kind, id string, width, height int, scroll *int) string {
	ed := editorState{kind: kind, values: loadEditorValues(kind, id, draft)}
	names := ed.fieldNames()
	name := id
	if got := ed.values["name"]; got != "" {
		name = got
	}
	sc := 0
	if scroll != nil {
		sc = *scroll
	}
	out, next := detailpane.Model{
		Title:    detailpane.NamedTitle(kind.Label(), name),
		Summary:  detailpane.RowsFromKeys([]string{"id", "name"}, ed.values),
		Sections: sectionsFromFields(names, ed.values, identityKeysFor(kind)),
		Width:    width,
		Height:   height,
		Scroll:   sc,
	}.ViewScroll()
	if scroll != nil {
		*scroll = next
	}
	return out
}

func identityKeysFor(kind Kind) []string {
	switch kind {
	case KindRoutes:
		return []string{"id", "name", "policy", "backend_set_id"}
	default:
		return []string{"id", "name"}
	}
}

func sectionsFromFields(names []string, values map[string]string, identity []string) []detailpane.Section {
	identitySet := map[string]bool{}
	for _, key := range identity {
		identitySet[key] = true
	}
	var identityRows, configRows []detailpane.Row
	for _, name := range names {
		value := values[name]
		if value == "" {
			value = "-"
		}
		row := detailpane.Row{Label: name, Value: value}
		if identitySet[name] {
			identityRows = append(identityRows, row)
		} else {
			configRows = append(configRows, row)
		}
	}
	sections := make([]detailpane.Section, 0, 2)
	if len(identityRows) > 0 {
		sections = append(sections, detailpane.Section{Title: "Identity", Rows: identityRows})
	}
	if len(configRows) > 0 {
		sections = append(sections, detailpane.Section{Title: "Configuration", Rows: configRows})
	}
	return sections
}

func renderDependencyDialog(kind Kind, id string, paths []string, width, height int, scroll *int) string {
	rows := make([]detailpane.Row, 0, len(paths))
	for i, path := range paths {
		rows = append(rows, detailpane.Row{Label: fmt.Sprintf("%d", i+1), Value: path})
	}
	if len(rows) == 0 {
		rows = []detailpane.Row{{Label: "refs", Value: i18n.T("detail.value.none")}}
	}
	sc := 0
	if scroll != nil {
		sc = *scroll
	}
	out, next := detailpane.Model{
		Title:    fmt.Sprintf("Cannot delete %s · %s", kind.Label(), id),
		Sections: []detailpane.Section{{Title: "Inbound references", Rows: rows}},
		Width:    width,
		Height:   height,
		Scroll:   sc,
	}.ViewScroll()
	if scroll != nil {
		*scroll = next
	}
	return out
}

func renderConfirmDelete(kind Kind, id string, deps []string, remaining int, width, height int, scroll *int) string {
	// Keep legacy prose substrings so confirm copy stays searchable in tests/UI.
	rows := []detailpane.Row{
		{Label: "summary", Value: fmt.Sprintf("Remaining after delete: %d", remaining)},
	}
	if len(deps) == 0 {
		rows = append(rows, detailpane.Row{Label: "deps", Value: "Dependent changes: none"})
	} else {
		rows = append(rows, detailpane.Row{Label: "deps", Value: "Dependent changes:"})
		for i, path := range deps {
			rows = append(rows, detailpane.Row{Label: fmt.Sprintf("%d", i+1), Value: path})
		}
	}
	hints := []string{"enter confirm  esc cancel"}
	if len(deps) > 0 {
		hints = []string{"enter view blockers  esc cancel"}
	}
	sc := 0
	if scroll != nil {
		sc = *scroll
	}
	out, next := detailpane.Model{
		Title:    fmt.Sprintf("Delete %s · %s?", kind.Label(), id),
		Sections: []detailpane.Section{{Title: "Confirm", Rows: rows}},
		Hints:    hints,
		Width:    width,
		Height:   height,
		Scroll:   sc,
	}.ViewScroll()
	if scroll != nil {
		*scroll = next
	}
	return out
}
