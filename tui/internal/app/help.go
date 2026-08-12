package app

import (
	"github.com/asheshgoplani/agent-deck/internal/i18n"
	"github.com/asheshgoplani/agent-deck/internal/ui"
)

func helpContentForTab(active tab) ui.HelpContent {
	title := "RESOURCE"
	items := []ui.HelpItem{
		{Key: "j / k", Description: "Move selection"},
		{Key: "Space", Description: "Mark row"},
		{Key: "/", Description: "Filter rows"},
		{Key: "[ / ]", Description: "Select column"},
		{Key: "s", Description: "Sort selected column"},
	}

	switch active {
	case tabOverview:
		title = "OVERVIEW"
		items = []ui.HelpItem{
			{Key: "e", Description: "Edit instance"},
			{Key: "Ctrl+S", Description: "Save and apply the open form"},
			{Key: "Ctrl+R", Description: "Refresh runtime and configuration snapshot"},
		}
	case tabSessions:
		title = "SESSIONS"
		items = []ui.HelpItem{
			{Key: "Alt+Left / Alt+Right", Description: "Switch Sessions and Profiles"},
			{Key: "j / k", Description: "Select session or profile"},
			{Key: "Enter", Description: "Open details"},
			{Key: "r", Description: "Retry preview"},
			{Key: "a / l / R / x", Description: "Attach, launch, resume or end"},
		}
	case tabRoutes:
		title = "ROUTES"
		items[0].Description = "Select route"
		items[1].Description = "Mark route"
		items[2].Description = "Filter routes"
	case tabTargets:
		title = "TARGETS"
		items[0].Description = "Select target"
		items[1].Description = "Mark target"
		items[2].Description = "Filter targets"
	case tabQuotas:
		title = "QUOTAS"
		items[0].Description = "Select quota"
		items[1].Description = "Mark quota"
		items[2].Description = "Filter quotas"
	case tabObservations:
		title = "OBSERVATIONS"
		items[0].Description = "Select observation"
		items[1].Description = "Mark observation"
		items[2].Description = "Filter observations"
	}

	return ui.HelpContent{
		Sections: []ui.HelpSection{
			{Title: title, Items: items},
			{
				Title: "GLOBAL",
				Items: []ui.HelpItem{
					{Key: "Ctrl+Left / Ctrl+Right", Description: "Switch tab"},
					{Key: ":", Description: i18n.T("help.spotlight")},
					{Key: "?", Description: i18n.T("help.help")},
					{Key: "Ctrl+R", Description: i18n.T("command.reload.title")},
					{Key: "q", Description: i18n.T("help.quit")},
				},
			},
		},
		Footer: "Agent Harbor TUI",
	}
}
