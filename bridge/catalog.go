package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"
	"unicode/utf8"
)

type catalogModel struct {
	MagpieLabel string `json:"magpie_label,omitempty"`
	OwnedBy     string `json:"owned_by,omitempty"`
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
}

func showModels(args []string) error {
	flags := flag.NewFlagSet("models", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	address := flags.String("url", "", "local Magpie address")
	instance := flags.String("instance", "", "Magpie instance ID")
	jsonOutput := flags.Bool("json", false, "JSON output")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return errors.New("usage: models --url http://127.0.0.1:PORT [--json]")
	}
	var models []catalogModel
	var err error
	if *instance != "" {
		models, err = appService.Catalog(*instance)
	} else {
		models, err = appService.Catalog(*address)
	}
	if err != nil {
		return err
	}
	if *jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(models)
	}
	fmt.Println("Magpie catalog")
	for _, model := range models {
		fmt.Printf("\n  ID     %q\n  Name   %q\n", model.ID, model.DisplayName)
	}
	fmt.Println("\nRouting groups may change or switch providers in Magpie.")
	return nil
}

func fetchCatalog(address string) ([]catalogModel, error) {
	return fetchCatalogWithOfficial(address, cachedCodexModels)
}

func fetchCatalogWithOfficial(address string, official func() []catalogModel) ([]catalogModel, error) {
	endpoint, err := url.Parse(address)
	if err != nil || endpoint.Scheme != "http" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || (endpoint.Path != "" && endpoint.Path != "/" && endpoint.Path != "/v1" && endpoint.Path != "/v1/") {
		return nil, errors.New("use a local Magpie HTTP address without credentials or query parameters")
	}
	ip := net.ParseIP(endpoint.Hostname())
	if ip == nil || !ip.IsLoopback() || endpoint.Port() == "" {
		return nil, errors.New("Magpie catalog address must use a loopback IP and port")
	}
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	endpoint.Path = "/api/hello"
	hello, err := client.Get(endpoint.String())
	if err != nil {
		return nil, errors.New("cannot reach Magpie; check its address and make sure Magpie is running")
	}
	var identity struct {
		Name       string `json:"name"`
		CallerOnly bool   `json:"codex_caller_only"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(hello.Body, 4096)).Decode(&identity)
	hello.Body.Close()
	if hello.StatusCode != http.StatusOK || decodeErr != nil || identity.Name != "magpie" {
		return nil, errors.New("the local service did not identify itself as Magpie; check the gateway address")
	}
	endpoint.Path = "/v1/models"
	resp, err := client.Get(endpoint.String())
	if err != nil {
		return nil, errors.New("cannot reach the local Magpie catalog; check its address and make sure Magpie is running")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Magpie catalog request failed\n  HTTP status: %d\n  Error code: catalog_http_error\n  Recovery: check Magpie's local gateway", resp.StatusCode)
	}
	var data struct {
		Models []catalogModel `json:"data"`
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(body) > 2<<20 || !utf8.Valid(body) || json.NewDecoder(bytes.NewReader(body)).Decode(&data) != nil || data.Models == nil {
		return nil, errors.New("Magpie returned an invalid model catalog")
	}
	for _, model := range data.Models {
		if model.ID == "" {
			return nil, errors.New("Magpie returned a model without an ID")
		}
	}
	if identity.CallerOnly && official != nil {
		data.Models = append(official(), data.Models...)
	}
	return data.Models, nil
}
