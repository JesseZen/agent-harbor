package main

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/cellbuf"
	"github.com/muesli/termenv"
)

func parsePreview(text string, truncated bool) previewSnapshot {
	profile := colorprofile.TrueColor
	switch lipgloss.ColorProfile() {
	case termenv.ANSI:
		profile = colorprofile.ANSI
	case termenv.ANSI256:
		profile = colorprofile.ANSI256
	}
	color := !colorOff()
	parser := ansi.NewParser()
	var pen, emitted cellbuf.Style
	parser.SetHandler(ansi.Handler{HandleCsi: func(command ansi.Cmd, params ansi.Params) {
		if command == 'm' {
			cellbuf.ReadStyle(params, &pen)
			pen.Attrs &= cellbuf.BoldAttr | cellbuf.ItalicAttr | cellbuf.FaintAttr | cellbuf.ReverseAttr | cellbuf.StrikethroughAttr
			pen.Ul = nil
		}
	}})
	var line strings.Builder
	var lines []string
	lastContent := 0
	meaningful := false
	finish := func() {
		if !emitted.Empty() {
			line.WriteString(ansi.ResetStyle)
		}
		lines = append(lines, line.String())
		if meaningful {
			lastContent = len(lines)
		}
		line.Reset()
		meaningful = false
		emitted = cellbuf.Style{}
	}
	const (
		ground = iota
		escape
		intermediates
		csi
		controlString
		stringEscape
	)
	state := ground
	osc := false
	var sequence strings.Builder
	for len(text) > 0 {
		char, size := utf8.DecodeRuneInString(text)
		if size == 1 && text[0] >= 0x80 && text[0] <= 0x9f {
			char = rune(text[0])
		}
		text = text[size:]
		if state == controlString || state == stringEscape {
			switch {
			case osc && char == '\a' || char == 0x9c || state == stringEscape && char == '\\' || char == 0x18 || char == 0x1a:
				state = ground
			case char == 0x1b:
				state = stringEscape
			default:
				state = controlString
			}
			continue
		}
		switch char {
		case 0x1b:
			state = escape
			continue
		case 0x90, 0x98, 0x9d, 0x9e, 0x9f:
			osc = char == 0x9d
			state = controlString
			continue
		case 0x9b:
			state = csi
			sequence.Reset()
			continue
		}
		switch state {
		case escape, intermediates:
			switch {
			case state == escape && strings.ContainsRune("]PX^_", char):
				state = controlString
				osc = char == ']'
			case state == escape && char == '[':
				state = csi
				sequence.Reset()
			case char >= 0x20 && char <= 0x2f:
				state = intermediates
			default:
				state = ground
			}
			continue
		case csi:
			if char == 0x18 || char == 0x1a {
				state = ground
			} else if char >= 0x40 && char <= 0x7e {
				if color && char == 'm' && sequence.Len() < 192 {
					parser.Parse([]byte("\x1b[" + sequence.String() + "m"))
				}
				state = ground
			} else if char >= 0x20 && char <= 0x3f && sequence.Len() < 192 {
				sequence.WriteRune(char)
			}
			continue
		}
		if char == '\n' {
			finish()
			continue
		}
		if unicode.IsControl(char) || unicode.In(char, unicode.Bidi_Control) {
			continue
		}
		if color && emitted != pen {
			line.WriteString(ansi.ResetStyle)
			if !pen.Empty() {
				line.WriteString(cellbuf.ConvertStyle(pen, profile).Sequence())
			}
			emitted = pen
		}
		line.WriteRune(char)
		meaningful = meaningful || !unicode.IsSpace(char) || color && pen.Bg != nil
	}
	finish()
	lines = lines[:lastContent]
	if len(lines) > 512 {
		lines = lines[len(lines)-512:]
		truncated = true
	}
	return previewSnapshot{lines: lines, truncated: truncated}
}
