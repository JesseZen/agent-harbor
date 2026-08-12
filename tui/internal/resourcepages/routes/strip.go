package routes

import (
	"strings"

	"github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type stripSegment struct {
	kind Kind
	x    int
	w    int
}

type stripLayout struct {
	segments []stripSegment
	line     string
}

var userKindOrder = []Kind{KindTrafficRules}

func buildStripLayout(active Kind, width int) stripLayout {
	activeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(resourceview.TokenOnAccent)).
		Background(lipgloss.Color(resourceview.TokenHeader)).
		Bold(true)
	idleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(resourceview.TokenText)).
		Faint(true)

	kinds := userKindOrder
	if active != KindTrafficRules {
		kinds = []Kind{active}
	}
	segments := make([]stripSegment, 0, len(kinds))
	parts := make([]string, 0, len(kinds))
	cursor := 0
	for _, kind := range kinds {
		label := routeStripLabel(kind)
		w := ansi.StringWidth(label)
		if cursor >= width {
			break
		}
		if cursor+w > width {
			// Truncated/ellipsis region is not a hit target.
			remain := width - cursor
			if remain > 0 {
				parts = append(parts, ansi.Truncate(label, remain, "…"))
			}
			break
		}
		segments = append(segments, stripSegment{kind: kind, x: cursor, w: w})
		if kind == active {
			parts = append(parts, activeStyle.Render(label))
		} else {
			parts = append(parts, idleStyle.Render(label))
		}
		cursor += w
	}
	return stripLayout{segments: segments, line: strings.Join(parts, "")}
}

func routeStripLabel(kind Kind) string {
	switch kind {
	case KindTrafficRules:
		return " Traffic Rules "
	default:
		return " Advanced Resources / " + kind.Label() + " "
	}
}

func renderStrip(active Kind, width int) string {
	return buildStripLayout(active, width).line
}

func stripHeight() int { return 1 }

func hitTestStrip(x, y, width int) (Kind, bool) {
	if y != 0 {
		return "", false
	}
	layout := buildStripLayout(KindTrafficRules, width) // user strip is the only navigable strip
	for _, seg := range layout.segments {
		if x >= seg.x && x < seg.x+seg.w {
			return seg.kind, true
		}
	}
	return "", false
}
