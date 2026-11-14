package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func magpieAddress() (string, error) {
	// The selected instance decides where a new Magpie session goes. Invalid
	// or unreadable registry state must not silently change that destination,
	// so the failure is reported instead of falling back to the environment.
	registry, err := loadMagpieRegistry()
	if err != nil {
		return "", fmt.Errorf("the Magpie instance registry cannot be read, so the selected instance is unknown: %w", err)
	}
	if instance, ok := registry.defaultInstance(); ok && instance.Address != "" {
		return instance.Address, nil
	}
	env, err := userEnvironment()
	if err != nil {
		return "", err
	}
	values := map[string]string{}
	for _, pair := range env {
		key, value, ok := strings.Cut(pair, "=")
		if ok && (key == "HOME" || key == "XDG_CONFIG_HOME" || key == "MAGPIE_ADDR") {
			values[key] = value
		}
	}
	if address := values["MAGPIE_ADDR"]; address != "" {
		return "http://" + address, nil
	}
	root := values["XDG_CONFIG_HOME"]
	if root == "" {
		root = filepath.Join(values["HOME"], ".config")
	}
	port := 3425
	data, err := os.ReadFile(filepath.Join(root, "magpie", "settings.json"))
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("cannot read Magpie settings: %w", err)
	}
	if err == nil {
		var settings struct {
			Port int `json:"port"`
		}
		if json.Unmarshal(data, &settings) != nil {
			return "", errors.New("invalid Magpie settings; check settings.json")
		}
		if settings.Port != 0 {
			port = settings.Port
		}
	}
	if port < 1 || port > 65535 {
		return "", errors.New("invalid Magpie port in settings")
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port), nil
}

func checkMagpieModel(address, id string) error {
	models, err := fetchCatalog(address)
	if err != nil {
		return err
	}
	for _, model := range models {
		if id != "" && model.ID == id {
			return nil
		}
	}
	return errors.New("the saved Magpie model or routing group is unavailable; check Magpie before continuing")
}

func pickMagpie(in *bufio.Reader, dir string) error {
	address, err := magpieAddress()
	if err != nil {
		return err
	}
	probe := probeGateway(address)
	fmt.Printf("\nProposed Magpie address: %s\n", address)
	if probe.State == gatewayVerified {
		fmt.Printf("Configuration: %s (version %s, PID %d)\n", probe.ConfigDir, probe.Version, probe.PID)
	} else {
		fmt.Printf("Identity: %s (%s)\n", probe.State, orUnknown(probe.Reason))
	}
	fmt.Println("Verify the instance and its providers in Magpie.")
	entered, err := readLine(in, "Address (Enter accepts proposed): ")
	if err != nil {
		return err
	}
	if entered != "" {
		address = entered
	}
	models, err := fetchCatalog(address)
	if err != nil {
		return err
	}
	if len(models) == 0 {
		return errors.New("Magpie has no available models; enable a model or routing group in Magpie")
	}
	fmt.Printf("\nMagpie at %s\n", address)
	const pageSize = 10
	page, index := 0, 0
	for {
		start, end := page*pageSize, min((page+1)*pageSize, len(models))
		fmt.Printf("\nModels: page %d / %d\n", page+1, (len(models)+pageSize-1)/pageSize)
		for i := start; i < end; i++ {
			fmt.Printf("  %d  %s  %s\n", i+1, models[i].ID, models[i].DisplayName)
		}
		choice, err := readChoice(in, "\nModel number, n/p for pages (Enter cancels): ")
		if err != nil {
			return err
		}
		if choice == "" {
			return errors.New("start cancelled")
		}
		if choice == "n" && end < len(models) {
			page++
			continue
		}
		if choice == "p" && page > 0 {
			page--
			continue
		}
		index, err = strconv.Atoi(choice)
		if err != nil || index <= start || index > end {
			return errors.New("invalid model number")
		}
		break
	}
	fmt.Println("\nMagpie may send auxiliary requests containing parts of this conversation to other models or providers.")
	answer, err := readChoice(in, "Type yes to start: ")
	if err != nil {
		return err
	}
	if answer != "yes" {
		return errors.New("start cancelled")
	}
	s, err := createSession(dir, "magpie", address, models[index-1].ID)
	if err != nil {
		return err
	}
	return openSessionPane(s, newConversation)
}
