package detailpane

import (
	"strings"
	"unicode"

	"github.com/asheshgoplani/agent-deck/internal/i18n"
)

// NamedTitle builds "{{kind}} · {{name}}" via i18n.
func NamedTitle(kind, name string) string {
	return i18n.T("detail.title.named", map[string]string{
		"kind": KindLabel(kind),
		"name": name,
	})
}

// KindLabel translates a resource kind display name.
func KindLabel(kind string) string {
	key := "detail.kind." + slug(kind)
	if got := i18n.T(key); got != key {
		return got
	}
	return kind
}

// FieldLabel translates a field id when a catalog entry exists.
func FieldLabel(field string) string {
	key := "detail.field." + field
	if got := i18n.T(key); got != key {
		return got
	}
	return field
}

func localizeHeading(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return title
	}
	if strings.HasPrefix(title, "detail.") {
		return i18n.T(title)
	}
	key := "detail.section." + slug(title)
	if got := i18n.T(key); got != key {
		return got
	}
	return title
}

func localizeHints(hints []string) []string {
	if len(hints) == 0 {
		return []string{i18n.T("detail.hint.close")}
	}
	out := make([]string, len(hints))
	for i, hint := range hints {
		switch hint {
		case "esc close":
			out[i] = i18n.T("detail.hint.close")
		case "enter confirm  esc cancel":
			out[i] = i18n.T("detail.hint.confirm")
		case "enter view blockers  esc cancel":
			out[i] = i18n.T("detail.hint.confirm_blocked")
		default:
			out[i] = hint
		}
	}
	return out
}

func slug(value string) string {
	var b strings.Builder
	lastUnderscore := true
	for _, r := range strings.ToLower(value) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastUnderscore = false
		case r == ' ' || r == '/' || r == '-' || r == '.' || r == '·':
			if !lastUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}
