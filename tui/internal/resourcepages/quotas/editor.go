package quotas

import (
	"fmt"
	"strings"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/detailpane"
	"github.com/asheshgoplani/agent-deck/internal/formui"
	"github.com/asheshgoplani/agent-deck/internal/i18n"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	rpgenerated "github.com/asheshgoplani/agent-deck/internal/resourcepage/generated"
	"github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"
	"github.com/charmbracelet/lipgloss"
)

type editorMode int

const (
	editorNone editorMode = iota
	editorCreate
	editorEdit
)

// Editor is the typed QuotaGroupConfig form driven by generated descriptors.
type Editor struct {
	mode   editorMode
	id     string
	values map[string]string
	cursor int
	err    string
}

func newEditor(mode editorMode, id string, draft *configdraft.Draft) *Editor {
	values := map[string]string{}
	desc, ok := resourcepage.Lookup(rpgenerated.ResourceQuotaGroup)
	if !ok {
		return &Editor{mode: mode, id: id, values: values, err: "missing descriptor for quota_group"}
	}
	for _, field := range desc.Fields {
		if field.DefaultValue != "" {
			values[field.Name] = field.DefaultValue
		}
	}
	if mode == editorEdit {
		values = loadEditorValues(id, draft)
	}
	return &Editor{mode: mode, id: id, values: values}
}

func loadEditorValues(id string, draft *configdraft.Draft) map[string]string {
	values := map[string]string{}
	q, ok := findQuota(draft.LocalCommand(), id)
	if !ok {
		return values
	}
	desc, ok := resourcepage.Lookup(rpgenerated.ResourceQuotaGroup)
	if !ok {
		return values
	}
	for _, field := range desc.Fields {
		values[field.Name] = quotaFieldValue(q, field.Name)
	}
	return values
}

func quotaFieldValue(q generated.QuotaGroupConfig, name string) string {
	switch name {
	case "id":
		return string(q.Id)
	case "name":
		return q.Name
	case "rpm":
		return fmt.Sprintf("%d", q.Rpm)
	case "max_concurrency":
		return fmt.Sprintf("%d", q.MaxConcurrency)
	case "foreground_capacity":
		return fmt.Sprintf("%d", q.ForegroundCapacity)
	case "background_capacity":
		return fmt.Sprintf("%d", q.BackgroundCapacity)
	case "foreground_weight":
		return fmt.Sprintf("%d", q.ForegroundWeight)
	case "background_weight":
		return fmt.Sprintf("%d", q.BackgroundWeight)
	case "queue_timeout_ms":
		return fmt.Sprintf("%d", q.QueueTimeoutMs)
	default:
		return ""
	}
}

func (e *Editor) fieldNames() []string {
	return []string{"name", "rpm", "max_concurrency", "foreground_capacity", "background_capacity", "foreground_weight", "background_weight", "queue_timeout_ms", "id"}
}

func (e *Editor) idEditable() bool { return e.mode == editorCreate }

func (e *Editor) setValues(values map[string]string) {
	if e.values == nil {
		e.values = map[string]string{}
	}
	for k, v := range values {
		e.values[k] = v
	}
}

func (e *Editor) render(width, height int) string {
	return e.formLayout(width, height).View
}

func (e *Editor) formLayout(width, height int) formui.Layout {
	action := "form.action.create"
	if e.mode == editorEdit {
		action = "form.action.edit"
	}
	title := i18n.T(action, map[string]string{"resource": i18n.T("form.resource.quota_group")})
	errorField, errorDetail := formui.CleanError(e.err)
	fields := make([]formui.Field, 0, len(e.fieldNames()))
	for _, name := range e.fieldNames() {
		field := formui.Field{ID: name, Label: formui.FriendlyLabel(name), Value: e.values[name], Kind: formui.Integer, Section: "Capacity"}
		switch name {
		case "name":
			field.Kind, field.Section, field.Required = formui.Text, "Basics", true
			field.Placeholder = "A name used in routes and targets"
		case "rpm":
			field.Unit, field.Required = "req/min", true
		case "max_concurrency":
			field.Unit, field.Required = "requests", true
		case "foreground_capacity", "background_capacity", "foreground_weight", "background_weight":
			field.Advanced = true
		case "queue_timeout_ms":
			field.Section, field.Unit, field.Advanced = "Queue", "ms", true
		case "id":
			field.Kind, field.Section, field.Advanced = formui.Text, "Advanced", true
			field.ReadOnly = !e.idEditable()
			field.Help = "Generated from the name; customize before saving if needed"
		}
		if errorField == name {
			field.Error = errorDetail
		}
		fields = append(fields, field)
	}
	return formui.Render(formui.Spec{
		Title: title, Context: i18n.T("form.context.quota"), Fields: fields, Focus: e.cursor,
		AdvancedExpanded: e.cursor >= 3, Width: width, Height: height,
		Footer: i18n.T("form.footer.quota"),
	})
}

func renderDetails(draft *configdraft.Draft, id string, width, height int) string {
	q, ok := findQuota(draft.LocalCommand(), id)
	if !ok {
		return detailpane.Model{
			Title: detailpane.NamedTitle("Quota Group", id),
			Sections: []detailpane.Section{{
				Title: "Status",
				Rows:  []detailpane.Row{{Label: "error", Value: i18n.T("detail.value.not_found")}},
			}},
			Width:  width,
			Height: height,
		}.View()
	}
	values := loadEditorValues(id, draft)
	name := q.Name
	if name == "" {
		name = string(q.Id)
	}
	return detailpane.Model{
		Title:   detailpane.NamedTitle("Quota Group", name),
		Summary: detailpane.RowsFromKeys([]string{"id", "name"}, values),
		Sections: []detailpane.Section{
			{Title: "Identity", Rows: detailpane.RowsFromKeys([]string{"id", "name"}, values)},
			{Title: "Capacity", Rows: detailpane.RowsFromKeys([]string{
				"rpm", "max_concurrency", "foreground_capacity", "background_capacity",
				"foreground_weight", "background_weight",
			}, values)},
			{Title: "Queue", Rows: detailpane.RowsFromKeys([]string{"queue_timeout_ms"}, values)},
		},
		Width:  width,
		Height: height,
	}.View()
}

func renderDependencyDialog(id string, paths []string, remaining int, width, height int) string {
	rows := []detailpane.Row{{
		Label: "summary",
		Value: fmt.Sprintf("Delete blocked — %d inbound reference(s) for %s", len(paths), id),
	}}
	for i, path := range paths {
		rows = append(rows, detailpane.Row{Label: fmt.Sprintf("%d", i+1), Value: path})
	}
	if len(paths) == 0 {
		rows = append(rows, detailpane.Row{Label: "refs", Value: i18n.T("detail.value.none")})
	}
	rows = append(rows, detailpane.Row{
		Label: "remaining",
		Value: fmt.Sprintf("%d quota group(s) will remain after delete", remaining),
	})
	return detailpane.Model{
		Title:    detailpane.NamedTitle("Quota Group", id),
		Sections: []detailpane.Section{{Title: "Inbound references", Rows: rows}},
		Width:    width,
		Height:   height,
	}.View()
}

func renderConfirmDeleteDialog(id string, remaining int, width, height int) string {
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(resourceview.TokenWarning))
	var b strings.Builder
	b.WriteString(labelStyle.Render("Confirm delete quota group " + id))
	b.WriteByte('\n')
	b.WriteString(labelStyle.Render("0 inbound references"))
	b.WriteByte('\n')
	b.WriteString(labelStyle.Render(fmt.Sprintf("%d quota group(s) will remain after delete", remaining)))
	b.WriteByte('\n')
	b.WriteString(labelStyle.Faint(true).Render("enter confirm  esc cancel"))
	out := b.String()
	lines := strings.Split(out, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}
