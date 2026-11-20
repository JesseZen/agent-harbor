package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The tab bar reads only Harbor's recorded default and makes one bounded
// loopback request. A reachable server is not evidence of configuration identity.
func harborStatus() string {
	entry := "⚓ Harbor ^B a · Magpie ^B m"
	root, err := bridgeHome()
	if err != nil {
		return entry + " · unknown"
	}
	if p, e := readShortcuts(root); e == nil {
		prefix := statusKey(p.prefixes()[0])
		harbor := strings.Replace(p.bindings(shortcutHarbor)[0], "prefix+", prefix+" ", 1)
		magpie := strings.Replace(p.bindings(shortcutMagpie)[0], "prefix+", prefix+" ", 1)
		entry = "⚓ Harbor " + harbor + " · Magpie " + magpie
	} else {
		entry = "⚓ Harbor · shortcut settings need attention"
	}
	data, err := os.ReadFile(filepath.Join(root, "magpie.json"))
	if os.IsNotExist(err) {
		return entry + " · local"
	}
	var registry magpieRegistry
	if err != nil || json.Unmarshal(data, &registry) != nil {
		return entry + " · unknown"
	}
	instance, ok := registry.defaultInstance()
	if !ok {
		return entry + " · local"
	}
	base, err := parseGatewayAddress(instance.Address)
	if err != nil {
		return entry + " · unknown"
	}
	client := &http.Client{Timeout: 150 * time.Millisecond, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	response, err := client.Get(base + "/")
	if err != nil {
		return entry + " · offline"
	}
	defer response.Body.Close()
	var report struct {
		Name string `json:"name"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&report) != nil || report.Name != "magpie" {
		return entry + " · unknown"
	}
	return entry + " · online (identity unchecked)"
}

func statusKey(key string) string {
	if letter, ok := strings.CutPrefix(key, "ctrl+"); ok && len(letter) == 1 {
		return "^" + strings.ToUpper(letter)
	}
	return key
}
