package main

import "strings"

// runPlan belongs to the managed launch reservation, independently of identity capture.
type runPlan struct {
	Profile   *profileBinding `json:"profile,omitempty"`
	SessionID string          `json:"session_id"`
	Token     string          `json:"token"`
	Home      string          `json:"home"`
}

func environmentValue(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if k, v, ok := strings.Cut(env[i], "="); ok && k == key {
			return v
		}
	}
	return ""
}
