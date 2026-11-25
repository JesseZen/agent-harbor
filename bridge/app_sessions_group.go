package main

import (
	"fmt"
	"strconv"
)

// sessionGroupMode identifies how the Sessions list partitions records into
// visual bands. Only status grouping is built in for now; additional modes
// (project, connection, profile, host, tag) plug into the same sessionGrouper
// interface without changing the list shell.
type sessionGroupMode string

const (
	groupByStatus      sessionGroupMode = "status"
	groupCollapseLimit                  = 5
)

type sessionGroup struct {
	ID       string
	Title    string
	Meta     string
	Sessions []sessionView
}

type sessionGrouper interface {
	Mode() sessionGroupMode
	Group(views []sessionView) []sessionGroup
}

type statusGrouper struct{}

func (statusGrouper) Mode() sessionGroupMode { return groupByStatus }

func pinFirst(views []sessionView) []sessionView {
	if len(views) < 2 {
		return views
	}
	out := make([]sessionView, 0, len(views))
	for _, v := range views {
		if !v.External && v.Pinned {
			out = append(out, v)
		}
	}
	if len(out) == 0 || len(out) == len(views) {
		return views
	}
	for _, v := range views {
		if v.External || !v.Pinned {
			out = append(out, v)
		}
	}
	return out
}

func (statusGrouper) Group(views []sessionView) []sessionGroup {
	var active, stopped, external []sessionView
	for _, v := range views {
		switch {
		case v.External:
			external = append(external, v)
		case v.Running || v.Runtime.Presence == "observed":
			active = append(active, v)
		default:
			stopped = append(stopped, v)
		}
	}
	groups := make([]sessionGroup, 0, 3)
	if len(active) > 0 {
		groups = append(groups, sessionGroup{
			ID:       "active",
			Title:    "Active",
			Meta:     strconv.Itoa(len(active)),
			Sessions: pinFirst(active),
		})
	}
	if len(stopped) > 0 {
		groups = append(groups, sessionGroup{
			ID:       "stopped",
			Title:    "Stopped",
			Meta:     strconv.Itoa(len(stopped)),
			Sessions: pinFirst(stopped),
		})
	}
	if len(external) > 0 {
		groups = append(groups, sessionGroup{
			ID:       "external",
			Title:    "External",
			Meta:     strconv.Itoa(len(external)),
			Sessions: external,
		})
	}
	return groups
}

type sessionRowKind int

const (
	sessionRowItem sessionRowKind = iota
	sessionRowToggle
)

type sessionRow struct {
	Kind       sessionRowKind
	GroupID    string
	GroupTitle string
	View       sessionView
	Expanded   bool
	Hidden     int
}

func (r sessionRow) RowID() string {
	if r.Kind == sessionRowToggle {
		return "toggle:" + r.GroupID
	}
	return r.View.ID
}

func (r sessionRow) HitID() string {
	if r.Kind == sessionRowToggle {
		return "toggle:" + r.GroupID
	}
	return "session:" + r.View.ID
}

func (r sessionRow) ToggleLabel() string {
	if r.Expanded {
		return "Show less"
	}
	return fmt.Sprintf("Show %d more", r.Hidden)
}

type sessionDisplayGroup struct {
	ID        string
	Title     string
	Meta      string
	Expanded  bool
	HasToggle bool
	Rows      []sessionRow
}

func sessionNeedsAttention(s sessionView) bool {
	if s.DiscoveryWarning != "" || sessionAttention(s) != "" || recoveryConflict(s.session) {
		return true
	}
	badge, _ := sessionStatusBadge(s)
	return badge != ""
}

func sessionRowCollapsible(groupID string, s sessionView) bool {
	if s.External || s.Pinned || sessionNeedsAttention(s) {
		return false
	}
	switch groupID {
	case "active":
		return s.Runtime.Presence == "observed" &&
			!s.Runtime.LaunchPending &&
			(s.Runtime.Activity == "idle" || s.Runtime.Activity == "done")
	case "stopped":
		return !s.Running &&
			(s.Runtime.Presence == "historical_exit" ||
				s.Runtime.Presence == "confirmed_exit" ||
				s.Runtime.Presence == "not_started")
	default:
		return false
	}
}

func foldSessionGroup(g sessionGroup, expanded, filtering bool, keepID string) sessionDisplayGroup {
	if filtering || (g.ID != "active" && g.ID != "stopped") {
		rows := make([]sessionRow, len(g.Sessions))
		for i, s := range g.Sessions {
			rows[i] = sessionRow{Kind: sessionRowItem, GroupID: g.ID, GroupTitle: g.Title, View: s}
		}
		return sessionDisplayGroup{ID: g.ID, Title: g.Title, Meta: g.Meta, Rows: rows}
	}
	collapsible := 0
	head := make([]sessionView, 0, len(g.Sessions))
	tail := make([]sessionView, 0)
	for _, s := range g.Sessions {
		if !sessionRowCollapsible(g.ID, s) {
			head = append(head, s)
			continue
		}
		if collapsible < groupCollapseLimit {
			collapsible++
			head = append(head, s)
			continue
		}
		if !expanded && keepID != "" && s.ID == keepID {
			head = append(head, s)
			continue
		}
		tail = append(tail, s)
	}
	rows := make([]sessionRow, 0, len(head)+len(tail)+1)
	for _, s := range head {
		rows = append(rows, sessionRow{Kind: sessionRowItem, GroupID: g.ID, GroupTitle: g.Title, View: s})
	}
	hasToggle := len(tail) > 0
	if hasToggle {
		rows = append(rows, sessionRow{
			Kind:       sessionRowToggle,
			GroupID:    g.ID,
			GroupTitle: g.Title,
			Expanded:   expanded,
			Hidden:     len(tail),
		})
		if expanded {
			for _, s := range tail {
				rows = append(rows, sessionRow{Kind: sessionRowItem, GroupID: g.ID, GroupTitle: g.Title, View: s})
			}
		}
	}
	return sessionDisplayGroup{
		ID:        g.ID,
		Title:     g.Title,
		Meta:      g.Meta,
		Expanded:  hasToggle && expanded,
		HasToggle: hasToggle,
		Rows:      rows,
	}
}
