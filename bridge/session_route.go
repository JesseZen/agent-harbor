package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type routeAttempt struct {
	Model  string `json:"model,omitempty"`
	Status int    `json:"status,omitempty"`
}
type routeRecord struct {
	Asked  string         `json:"asked,omitempty"`
	Group  string         `json:"group,omitempty"`
	Model  string         `json:"model,omitempty"`
	Kind   string         `json:"kind,omitempty"`
	Tries  []routeAttempt `json:"tries,omitempty"`
	Served string         `json:"served,omitempty"`
}

func (harborService) ObservedRoute(id string) (observedRoute, error) {
	s, err := getSession(id)
	if err != nil {
		return observedRoute{}, err
	}
	unavailable := observedRoute{Reason: "not observed"}
	if s.Mode != "magpie" || !validCodexID(s.CodexSessionID) {
		return unavailable, nil
	}
	// Apply the same loopback validation as catalogs before issuing a request.
	if _, err := parseGatewayAddress(s.MagpieURL); err != nil {
		return unavailable, nil
	}
	endpoint, err := url.Parse(s.MagpieURL)
	if err != nil {
		return unavailable, nil
	}
	endpoint.Path = "/v1/magpie/route"
	endpoint.RawQuery = url.Values{"session": []string{s.CodexSessionID}}.Encode()
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get(endpoint.String())
	if err != nil {
		return unavailable, nil
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return unavailable, nil
	}
	var responseData struct {
		Session string       `json:"session"`
		Route   *routeRecord `json:"route"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&responseData) != nil || responseData.Session != s.CodexSessionID || responseData.Route == nil || strings.TrimSpace(responseData.Route.Model) == "" {
		return unavailable, nil
	}
	route := *responseData.Route
	result := observedRoute{Observed: true}
	if route.Kind != "" && route.Kind != "main" {
		result.Auxiliary = []any{route}
	} else {
		result.Main = route
	}
	return result, nil
}
