package i18n

import (
	"os"
	"strings"
	"sync"
)

type Locale string

const (
	LocaleEN   Locale = "en"
	LocaleZhCN Locale = "zh-CN"
)

type LocaleInfo struct {
	ID       Locale
	Name     string
	Coverage int
}

var (
	mu       sync.RWMutex
	locale   = LocaleEN
	catalogs = map[Locale]map[string]string{}
)

func init() {
	catalogs[LocaleEN] = en
	catalogs[LocaleZhCN] = zhCN
}

func SetLocale(id string) {
	mu.Lock()
	defer mu.Unlock()
	normalized := Locale(normalizeLocaleID(id))
	if _, ok := catalogs[normalized]; ok {
		locale = normalized
		return
	}
	locale = LocaleEN
}

func GetLocale() Locale {
	mu.RLock()
	defer mu.RUnlock()
	return locale
}

func Locales() []LocaleInfo {
	enKeys := len(en)
	list := []LocaleInfo{
		{ID: LocaleEN, Name: "English", Coverage: 100},
		{ID: LocaleZhCN, Name: "简体中文", Coverage: coverage(zhCN, enKeys)},
	}
	return list
}

func coverage(dict map[string]string, total int) int {
	if total == 0 {
		return 100
	}
	n := 0
	for key, value := range dict {
		if _, ok := en[key]; ok && strings.TrimSpace(value) != "" {
			n++
		}
	}
	return (n * 100) / total
}

// T returns the translated string for key. Optional args[0] may be map[string]string for {{vars}}.
func T(key string, args ...any) string {
	mu.RLock()
	current := locale
	mu.RUnlock()
	text := lookup(current, key)
	if text == "" {
		text = lookup(LocaleEN, key)
	}
	if text == "" {
		text = key
	}
	if len(args) > 0 {
		if params, ok := args[0].(map[string]string); ok {
			text = interpolate(text, params)
		}
	}
	return text
}

func lookup(id Locale, key string) string {
	mu.RLock()
	defer mu.RUnlock()
	if dict, ok := catalogs[id]; ok {
		return dict[key]
	}
	return ""
}

func interpolate(template string, params map[string]string) string {
	result := template
	for name, value := range params {
		result = strings.ReplaceAll(result, "{{"+name+"}}", value)
	}
	return result
}

func DetectLocale(env map[string]string) Locale {
	if env == nil {
		env = map[string]string{
			"LC_ALL":      os.Getenv("LC_ALL"),
			"LC_MESSAGES": os.Getenv("LC_MESSAGES"),
			"LANG":        os.Getenv("LANG"),
		}
	}
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if got := detectOne(env[name]); got != "" {
			return got
		}
	}
	return LocaleEN
}

func detectOne(value string) Locale {
	if value == "" {
		return ""
	}
	normalized := strings.ToLower(strings.ReplaceAll(value, "_", "-"))
	switch {
	case strings.HasPrefix(normalized, "zh-hans"), strings.HasPrefix(normalized, "zh-cn"), strings.HasPrefix(normalized, "zh"):
		if strings.Contains(normalized, "tw") || strings.Contains(normalized, "hk") || strings.Contains(normalized, "hant") {
			return LocaleEN // no zh-TW pack yet
		}
		return LocaleZhCN
	case strings.HasPrefix(normalized, "en"):
		return LocaleEN
	default:
		return ""
	}
}

func normalizeLocaleID(id string) string {
	id = strings.TrimSpace(id)
	id = strings.ReplaceAll(id, "_", "-")
	switch strings.ToLower(id) {
	case "zh", "zh-cn", "zh-hans":
		return string(LocaleZhCN)
	case "en", "en-us", "en-gb":
		return string(LocaleEN)
	default:
		return id
	}
}

// SelectInitialLocale prefers persisted, then environment.
func SelectInitialLocale(persisted string) Locale {
	if persisted != "" {
		SetLocale(persisted)
		return GetLocale()
	}
	detected := DetectLocale(nil)
	SetLocale(string(detected))
	return detected
}
