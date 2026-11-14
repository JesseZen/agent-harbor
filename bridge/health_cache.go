package main

import (
	"encoding/json"
	"sync"
	"time"
)

// Background activity polling never waits for connection/route probes. A single
// bounded worker samples health, retaining the record fingerprint with its result.
var sessionHealthCache = struct {
	sync.Mutex
	entries map[string]healthEntry
	busy    bool
}{entries: map[string]healthEntry{}}

type healthEntry struct {
	record string
	view   sessionView
	at     time.Time
}

func cachedSessionHealth(s session) sessionView {
	raw, _ := json.Marshal(s)
	root, _ := bridgeHome()
	fingerprint := root + string(raw)
	sessionHealthCache.Lock()
	cached, ok := sessionHealthCache.entries[s.ID]
	v := sessionView{session: s, WriterLockCheck: "not inspected; explicitly review external writer uncertainty", Health: "pending"}
	if ok && cached.record == fingerprint {
		v.Reason = cached.view.Reason
		v.Health = cached.view.Health
	}
	if !sessionHealthCache.busy && (!ok || cached.record != fingerprint || time.Since(cached.at) > 10*time.Second) {
		sessionHealthCache.busy = true
		go func() {
			view := deriveSession(s)
			view.Health = "checked"
			if view.Reason != "" {
				view.Health = "needs_review"
			}
			sessionHealthCache.Lock()
			defer sessionHealthCache.Unlock()
			if len(sessionHealthCache.entries) > 1024 {
				sessionHealthCache.entries = map[string]healthEntry{}
			}
			sessionHealthCache.entries[s.ID] = healthEntry{fingerprint, view, time.Now()}
			sessionHealthCache.busy = false
		}()
	}
	sessionHealthCache.Unlock()
	return v
}
