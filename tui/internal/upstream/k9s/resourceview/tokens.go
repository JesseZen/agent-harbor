package resourceview

// Theme tokens for table chrome. Defaults match the historical k9s port palette;
// ui.InitTheme overwrites them via ApplyTheme so :theme reaches tables/strips.
var (
	// TokenOnAccent is dark contrast text for bright accent fills (footer chips).
	TokenOnAccent = "#000000"
	TokenBorder   = "#54D6E8"
	// TokenHeader is the header/label accent (fg on column headers; bg on kind strips).
	TokenHeader = "#63E6E2"
	// TokenHeaderBg is the muted column-header band (below selected-row emphasis).
	TokenHeaderBg = "#24283B"
	TokenText     = "#D8DEE9"
	TokenHealthy  = "#8BD600"
	TokenWarning  = "#F0B95B"
	TokenError    = "#FF5F67"
	// TokenSelected is the selected-row background (continuous band; brightest chrome).
	TokenSelected = "#2E4A5E"
	// TokenAccent matches Session ColorAccent for footer key chips.
	TokenAccent = "#7AA2F7"
	// TokenRuleIdle is the idle glyph column rule color.
	TokenRuleIdle = "#3B4261"
	// TokenRuleSelected is the selected-row glyph column rule color.
	TokenRuleSelected = "#7AA2F7"
)

// ThemeTokens is the palette snapshot applied from the AH UI theme registry.
type ThemeTokens struct {
	OnAccent, Border, Header, HeaderBg, Text string
	Healthy, Warning, Error                 string
	Selected, Accent                        string
	RuleIdle, RuleSelected                  string
}

// ApplyTheme updates live table tokens (called from ui.InitTheme).
func ApplyTheme(tokens ThemeTokens) {
	if tokens.OnAccent != "" {
		TokenOnAccent = tokens.OnAccent
	}
	if tokens.Border != "" {
		TokenBorder = tokens.Border
	}
	if tokens.Header != "" {
		TokenHeader = tokens.Header
	}
	if tokens.HeaderBg != "" {
		TokenHeaderBg = tokens.HeaderBg
	}
	if tokens.Text != "" {
		TokenText = tokens.Text
	}
	if tokens.Healthy != "" {
		TokenHealthy = tokens.Healthy
	}
	if tokens.Warning != "" {
		TokenWarning = tokens.Warning
	}
	if tokens.Error != "" {
		TokenError = tokens.Error
	}
	if tokens.Selected != "" {
		TokenSelected = tokens.Selected
	}
	if tokens.Accent != "" {
		TokenAccent = tokens.Accent
	}
	if tokens.RuleIdle != "" {
		TokenRuleIdle = tokens.RuleIdle
	}
	if tokens.RuleSelected != "" {
		TokenRuleSelected = tokens.RuleSelected
	}
}
