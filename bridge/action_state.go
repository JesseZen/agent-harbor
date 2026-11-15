package main

import (
	"context"
	"errors"
	"reflect"
	"time"
)

var errConversationObserved = errors.New("this conversation is already observed in Herdr; focus its verified target")

const (
	launchStateRetryWindow = 3 * time.Second
	launchStateRetryDelay  = 200 * time.Millisecond
)

func currentRuntime(id string) (sessionView, []sessionView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()
	views, err := (harborService{}).ListContext(ctx)
	if err != nil {
		return sessionView{}, nil, err
	}
	for _, v := range views {
		if v.ID == id {
			return v, views, nil
		}
	}
	return sessionView{}, views, errors.New("target disappeared or changed; refresh and review again")
}
func sameRuntimeTarget(a, b runtimeObservation) bool {
	return a.Instance == b.Instance && a.Terminal == b.Terminal && a.Workspace == b.Workspace && a.Presence == b.Presence && a.Associated == b.Associated && a.RunVerified == b.RunVerified && reflect.DeepEqual(a.Reference, b.Reference)
}
func validateApprovalRuntime(a sessionApproval) error {
	v, _, err := currentRuntime(a.Session.ID)
	if err != nil {
		return err
	}
	if !sameSession(v.session, a.Session) {
		return errors.New("session changed; review it again")
	}
	if a.Observation != nil && !sameRuntimeTarget(*a.Observation, v.Runtime) {
		return errors.New("runtime target changed; refresh and confirm again")
	}
	if v.External {
		return errors.New("unmanaged agent is read-only")
	}
	if v.DiscoveryWarning != "" && a.Session.HerdrSocket != "" {
		return errors.New(v.DiscoveryWarning)
	}
	return nil
}
func validateLaunchState(s session) error {
	v, all, err := currentRuntime(s.ID)
	if err != nil {
		return err
	}
	if !sameSession(v.session, s) {
		return errors.New("session changed; review it again")
	}
	if v.DiscoveryWarning != "" {
		return errors.New(v.DiscoveryWarning)
	}
	switch v.Runtime.Presence {
	case "not_started":
	case "historical_exit", "confirmed_exit":
		if v.Runtime.Instance == "" {
			return errors.New("live workspace state unavailable; recovery requires review")
		}
	default:
		return errors.New("runtime is observed, unknown or not confirmed ended; refresh and review before launch")
	}
	for _, other := range all {
		if other.Runtime.Presence == "observed" && s.Conversation != nil && other.Runtime.Reference != nil && *other.Runtime.Reference == s.Conversation.Reference {
			return errConversationObserved
		}
	}
	return nil
}

func waitForLaunchState(s session) error {
	return waitForLaunchStateFn(func() error { return validateLaunchState(s) }, launchStateRetryWindow)
}

func waitForLaunchStateFn(validate func() error, window time.Duration) error {
	deadline := time.Now().Add(window)
	for {
		err := validate()
		if !errors.Is(err, errConversationObserved) {
			return err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return err
		}
		delay := launchStateRetryDelay
		if remaining < delay {
			delay = remaining
		}
		timer := time.NewTimer(delay)
		<-timer.C
	}
}

func (h harborService) FocusObserved(expected sessionView) error {
	v, _, err := currentRuntime(expected.ID)
	if err != nil {
		return err
	}
	if v.DiscoveryWarning != "" || !sameRuntimeTarget(expected.Runtime, v.Runtime) || v.Runtime.Presence != "observed" || v.Runtime.Terminal == "" || (!v.External && !v.Runtime.Associated && !v.Runtime.RunVerified) {
		return errors.New("runtime target unavailable or changed; refresh and review again")
	}

	if !v.External && !sameSession(v.session, expected.session) {
		return errors.New("session changed; refresh and review again")
	}
	if socketIdentity(v.HerdrSocket) != v.Runtime.Instance {
		return errors.New("workspace changed; refresh")
	}
	var result map[string]any
	return agentRPC(v.HerdrSocket, "agent.focus", map[string]any{"target": v.HerdrPane}, &result)
}
