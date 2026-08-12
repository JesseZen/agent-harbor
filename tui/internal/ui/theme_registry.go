package ui

import "github.com/charmbracelet/lipgloss"

type palette struct {
	Bg, Surface, Border, Text, TextDim  lipgloss.Color
	Accent, Purple, Cyan, Green, Yellow lipgloss.Color
	Orange, Red, Comment                lipgloss.Color
	Mode                                Theme
}

var currentThemeName = "tokyonight"

var themePalettes = map[string]palette{
	"tokyonight": {
		Mode: ThemeDark,
		Bg:   lipgloss.Color("#1a1b26"), Surface: lipgloss.Color("#24283b"), Border: lipgloss.Color("#414868"),
		Text: lipgloss.Color("#c0caf5"), TextDim: lipgloss.Color("#787fa0"), Accent: lipgloss.Color("#7aa2f7"),
		Purple: lipgloss.Color("#bb9af7"), Cyan: lipgloss.Color("#7dcfff"), Green: lipgloss.Color("#9ece6a"),
		Yellow: lipgloss.Color("#e0af68"), Orange: lipgloss.Color("#ff9e64"), Red: lipgloss.Color("#f7768e"),
		Comment: lipgloss.Color("#787fa0"),
	},
	"tokyonight-light": {
		Mode: ThemeLight,
		Bg:   lipgloss.Color("#d5d6db"), Surface: lipgloss.Color("#e9e9ec"), Border: lipgloss.Color("#9699a3"),
		Text: lipgloss.Color("#343b58"), TextDim: lipgloss.Color("#6a6d7c"), Accent: lipgloss.Color("#34548a"),
		Purple: lipgloss.Color("#7847bd"), Cyan: lipgloss.Color("#166775"), Green: lipgloss.Color("#485e30"),
		Yellow: lipgloss.Color("#8f5e15"), Orange: lipgloss.Color("#965027"), Red: lipgloss.Color("#8c4351"),
		Comment: lipgloss.Color("#6a6d7c"),
	},
	"nord": {
		Mode: ThemeDark,
		Bg:   lipgloss.Color("#2e3440"), Surface: lipgloss.Color("#3b4252"), Border: lipgloss.Color("#4c566a"),
		Text: lipgloss.Color("#eceff4"), TextDim: lipgloss.Color("#d8dee9"), Accent: lipgloss.Color("#88c0d0"),
		Purple: lipgloss.Color("#b48ead"), Cyan: lipgloss.Color("#8fbcbb"), Green: lipgloss.Color("#a3be8c"),
		Yellow: lipgloss.Color("#ebcb8b"), Orange: lipgloss.Color("#d08770"), Red: lipgloss.Color("#bf616a"),
		Comment: lipgloss.Color("#616e88"),
	},
	"dracula": {
		Mode: ThemeDark,
		Bg:   lipgloss.Color("#282a36"), Surface: lipgloss.Color("#21222c"), Border: lipgloss.Color("#44475a"),
		Text: lipgloss.Color("#f8f8f2"), TextDim: lipgloss.Color("#6272a4"), Accent: lipgloss.Color("#bd93f9"),
		Purple: lipgloss.Color("#ff79c6"), Cyan: lipgloss.Color("#8be9fd"), Green: lipgloss.Color("#50fa7b"),
		Yellow: lipgloss.Color("#f1fa8c"), Orange: lipgloss.Color("#ffb86c"), Red: lipgloss.Color("#ff5555"),
		Comment: lipgloss.Color("#6272a4"),
	},
	"catppuccin": {
		Mode: ThemeDark,
		Bg:   lipgloss.Color("#1e1e2e"), Surface: lipgloss.Color("#313244"), Border: lipgloss.Color("#45475a"),
		Text: lipgloss.Color("#cdd6f4"), TextDim: lipgloss.Color("#a6adc8"), Accent: lipgloss.Color("#89b4fa"),
		Purple: lipgloss.Color("#cba6f7"), Cyan: lipgloss.Color("#94e2d5"), Green: lipgloss.Color("#a6e3a1"),
		Yellow: lipgloss.Color("#f9e2af"), Orange: lipgloss.Color("#fab387"), Red: lipgloss.Color("#f38ba8"),
		Comment: lipgloss.Color("#6c7086"),
	},
	"gruvbox": {
		Mode: ThemeDark,
		Bg:   lipgloss.Color("#282828"), Surface: lipgloss.Color("#3c3836"), Border: lipgloss.Color("#504945"),
		Text: lipgloss.Color("#ebdbb2"), TextDim: lipgloss.Color("#a89984"), Accent: lipgloss.Color("#83a598"),
		Purple: lipgloss.Color("#d3869b"), Cyan: lipgloss.Color("#8ec07c"), Green: lipgloss.Color("#b8bb26"),
		Yellow: lipgloss.Color("#fabd2f"), Orange: lipgloss.Color("#fe8019"), Red: lipgloss.Color("#fb4934"),
		Comment: lipgloss.Color("#928374"),
	},
	"rosepine": {
		Mode: ThemeDark,
		Bg:   lipgloss.Color("#191724"), Surface: lipgloss.Color("#1f1d2e"), Border: lipgloss.Color("#26233a"),
		Text: lipgloss.Color("#e0def4"), TextDim: lipgloss.Color("#908caa"), Accent: lipgloss.Color("#c4a7e7"),
		Purple: lipgloss.Color("#c4a7e7"), Cyan: lipgloss.Color("#9ccfd8"), Green: lipgloss.Color("#31748f"),
		Yellow: lipgloss.Color("#f6bc66"), Orange: lipgloss.Color("#ebbcba"), Red: lipgloss.Color("#eb6f92"),
		Comment: lipgloss.Color("#6e6a86"),
	},
	"onedark": {
		Mode: ThemeDark,
		Bg:   lipgloss.Color("#282c34"), Surface: lipgloss.Color("#21252b"), Border: lipgloss.Color("#3e4452"),
		Text: lipgloss.Color("#abb2bf"), TextDim: lipgloss.Color("#5c6370"), Accent: lipgloss.Color("#61afef"),
		Purple: lipgloss.Color("#c678dd"), Cyan: lipgloss.Color("#56b6c2"), Green: lipgloss.Color("#98c379"),
		Yellow: lipgloss.Color("#e5c07b"), Orange: lipgloss.Color("#d19a66"), Red: lipgloss.Color("#e06c75"),
		Comment: lipgloss.Color("#5c6370"),
	},
}

func resolveThemeName(name string) string {
	switch name {
	case "", "dark":
		return "tokyonight"
	case "light":
		return "tokyonight-light"
	default:
		if _, ok := themePalettes[name]; ok {
			return name
		}
		return "tokyonight"
	}
}

// ListThemes returns built-in theme ids in stable order.
func ListThemes() []string {
	order := []string{
		"tokyonight", "tokyonight-light", "catppuccin", "dracula",
		"gruvbox", "nord", "onedark", "rosepine",
	}
	return append([]string(nil), order...)
}

// GetCurrentThemeName returns the active named theme id.
func GetCurrentThemeName() string {
	themeMu.RLock()
	defer themeMu.RUnlock()
	return currentThemeName
}

func applyPalette(p palette) {
	currentTheme = p.Mode
	ColorBg = p.Bg
	ColorSurface = p.Surface
	ColorBorder = p.Border
	ColorText = p.Text
	ColorTextDim = p.TextDim
	ColorAccent = p.Accent
	ColorPurple = p.Purple
	ColorCyan = p.Cyan
	ColorGreen = p.Green
	ColorYellow = p.Yellow
	ColorOrange = p.Orange
	ColorRed = p.Red
	ColorComment = p.Comment
}
