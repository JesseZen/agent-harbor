package main

import "strconv"

// sessionGroupMode identifies how the Sessions list partitions records into
// visual bands. Only status grouping is built in for now; additional modes
// (project, connection, profile, host, tag) plug into the same sessionGrouper
// interface without changing the list shell.
type sessionGroupMode string

const (
	groupByStatus sessionGroupMode = "status"
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
			Sessions: active,
		})
	}
	if len(stopped) > 0 {
		groups = append(groups, sessionGroup{
			ID:       "stopped",
			Title:    "Stopped",
			Meta:     strconv.Itoa(len(stopped)),
			Sessions: stopped,
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
