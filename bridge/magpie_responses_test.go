package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMagpieResponsesFormatResolution(t *testing.T) {
	for _, test := range []struct {
		choice       responsesFormat
		supportsLite bool
		wantLite     bool
		wantError    bool
	}{
		{"", false, false, false},
		{"", true, true, false},
		{responsesAutomatic, true, true, false},
		{responsesStandard, true, false, false},
		{responsesLite, true, true, false},
		{responsesLite, false, false, true},
		{"unknown", true, false, true},
	} {
		d := sessionDraft{ResponsesFormat: test.choice, Model: "aihub/gpt-6.2-sol"}
		err := d.resolveResponsesFormat(test.supportsLite)
		if (d.ResponsesLiteModel != "") != test.wantLite || (err != nil) != test.wantError {
			t.Errorf("%q with Lite=%v: got Lite=%v, error=%v", test.choice, test.supportsLite, d.ResponsesLiteModel != "", err)
		}
	}
}

func TestMagpieResponsesFormatUsesDeclaredCatalogAndRequiresSavedLiteRoute(t *testing.T) {
	const model = "aihub/gpt-6.2-sol"
	var liteAvailable atomic.Bool
	liteAvailable.Store(true)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/hello":
			fmt.Fprint(w, `{"name":"magpie","codex_responses_lite":true}`)
		case "/v1/models":
			fmt.Fprint(w, `{"data":[{"id":"aihub/gpt-6.2-sol","magpie_codex_responses_lite":true},{"id":"group"}]}`)
		case "/v1/codex/models":
			if r.URL.Query().Get("responses_format") == "lite" && liteAvailable.Load() {
				fmt.Fprint(w, `{"magpie_responses_format":"lite","models":[{"slug":"aihub/gpt-6.2-sol","use_responses_lite":true}]}`)
				return
			}
			fmt.Fprint(w, `{"models":[{"slug":"aihub/gpt-6.2-sol","use_responses_lite":false}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer gateway.Close()

	models, err := fetchCatalogWithOfficial(gateway.URL+"/v1", nil)
	if err != nil || len(models) != 2 || !models[0].SupportsResponsesLite || models[1].SupportsResponsesLite {
		t.Fatalf("unexpected catalog: %+v, %v", models, err)
	}
	dir := t.TempDir()
	for _, savedRoute := range []string{"", model} {
		file, err := magpieCodexCatalog(gateway.URL, dir, model, "", savedRoute)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(file)
		if err != nil || strings.Contains(string(data), `"use_responses_lite":true`) != (savedRoute != "") {
			t.Fatalf("wrong session catalog: %s, %v", data, err)
		}
	}
	liteAvailable.Store(false)
	_, err = magpieCodexCatalog(gateway.URL, dir, model, "", model)
	if !errors.Is(err, errMagpieResponsesLiteUnavailable) {
		t.Fatalf("saved Lite route silently changed: %v", err)
	}
}

func TestMagpieResponsesFormatPickerAtNarrowAndWideSizes(t *testing.T) {
	const model = "aihub/gpt-6.2-sol"
	for _, width := range []int{60, 80, 160} {
		m := newHarborModel(&fakeApplicationService{})
		m.width, m.height = width, 24
		m.formActive, m.screen, m.formFocus = true, "new-form", formName
		m.draft = sessionDraft{Connection: "magpie", Instance: "managed", Model: model}
		m.modelsInstance = m.draft.Instance
		m.models = []catalogModel{{ID: model, SupportsResponsesLite: true}}
		press := func(key string) {
			next, _, ok := m.newSessionPageInput(uiKey(key))
			if !ok {
				t.Fatalf("key %q not handled at width %d", key, width)
			}
			m = next.(harborModel)
		}
		press("down")
		press("enter")
		page, _ := m.newSessionPage()
		if !strings.Contains(strings.Join(page.Lines, "\n"), "Responses Lite") {
			t.Fatalf("picker is missing Lite at width %d", width)
		}
		press("down")
		press("enter")
		if m.draft.ResponsesFormat != responsesStandard || m.formFocus != formResponses || m.screen != "new-form" {
			t.Fatal("applying Responses did not return to the format field")
		}
		press("enter")
		press("down")
		press("esc")
		if m.draft.ResponsesFormat != responsesStandard {
			t.Fatal("cancel changed the draft")
		}
		m.setDraftModel("other")
		if m.formFieldVisible(formResponses) || m.draft.ResponsesFormat != "" {
			t.Fatal("format choice leaked to a different route")
		}
	}
}
