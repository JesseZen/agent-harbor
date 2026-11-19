package main

import (
	"context"
	"errors"
	"os"
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
		if v.Runtime.Presence == "observed" || !recoveryEndMatches(s, s.Ended) || s.Ended.Source != idleRecoverySource || s.Ended.SocketIdentity != v.Runtime.Instance {
			return errors.New("runtime is observed, unknown or not confirmed ended; refresh and review before launch")
		}
	}
	return recoveryOccupied(s, all)
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
	if v.DiscoveryWarning != "" || !sameFocusTarget(expected, v) || v.Runtime.Presence != "observed" || v.Runtime.Terminal == "" || (!v.External && !v.Runtime.Associated && !v.Runtime.RunVerified) {
		return errors.New("runtime target unavailable or changed; refresh and review again")
	}

	if socketIdentity(v.HerdrSocket) != v.Runtime.Instance {
		return errors.New("workspace changed; refresh")
	}
	if os.Getenv("HERDR_PLUGIN_ENTRYPOINT_ID") == "app" || os.Getenv("HERDR_ACTIVE_PANE_ID") != "" {
		return focusPaneAfterPopup(v.HerdrSocket, v.Runtime.Instance, v.HerdrPane, v.Runtime.Terminal)
	}
	var result map[string]any
	return agentRPC(v.HerdrSocket, "agent.focus", map[string]any{"target": v.HerdrPane}, &result)
}

func sameFocusTarget(expected, current sessionView) bool {
	if expected.External || current.External {
		return expected.External == current.External && sameRuntimeTarget(expected.Runtime, current.Runtime)
	}
	a, b := expected.Runtime, current.Runtime
	return a.Presence == b.Presence && a.Instance == b.Instance && a.Workspace == b.Workspace && a.Terminal == b.Terminal && expected.HerdrPane == current.HerdrPane && expected.RunToken == current.RunToken && expected.ProcessGroup == current.ProcessGroup && sameProfileBinding(expected.Profile, current.Profile)
}
