package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// A gateway probe reports the address, what answered there, and, when the
// serving process is readable, the configuration it reads. Anything Harbor
// cannot observe stays unknown with a reason; it is never inferred from the
// address, /api/hello or the model catalog.
const (
	gatewayVerified  = "verified"
	gatewayUnknown   = "unknown"
	gatewayOffline   = "offline"
	gatewayNotMagpie = "not-magpie"
)

const (
	reasonNotLoopback        = "the address is not a loopback address"
	reasonNoServingProcess   = "the gateway did not report the process serving it"
	reasonNotMagpie          = "the local service did not identify itself as Magpie"
	reasonProcessUnreadable  = "the gateway process could not be read, so its configuration cannot be verified"
	reasonProcessOtherUser   = "the gateway process belongs to another user, so its configuration cannot be verified"
	reasonProcessEnded       = "the gateway process ended before its configuration could be read"
	reasonGatewayUnsupported = "this platform cannot read another process's executable and environment, so its configuration cannot be verified"
	reasonProcessReplaced    = "the gateway's executable was replaced after it started, and the configuration location it chose then cannot be read from the current path"

	// redactedAddressPlaceholder replaces any rejected address: the input may
	// carry credentials and only a validated address may be shown.
	redactedAddressPlaceholder = "<address not shown: it is not a loopback host and port>"
)

type gatewayProbe struct {
	Address        string `json:"address"`
	State          string `json:"state"`
	Reason         string `json:"reason,omitempty"`
	Version        string `json:"version,omitempty"`
	PID            int    `json:"pid,omitempty"`
	Serves         string `json:"serves,omitempty"`
	Executable     string `json:"executable,omitempty"`
	ConfigDir      string `json:"config_dir,omitempty"`
	ConfigEvidence string `json:"config_evidence,omitempty"`
	ConfigWarning  string `json:"config_warning,omitempty"`
	variables      map[string]string
}

// managementEnvironment is the environment Magpie's own TUI needs to read the
// verified instance: its home, config, cache and the address it serves. No
// other variable from the gateway process is copied or reported.
func (p gatewayProbe) managementEnvironment() []string {
	var env []string
	for _, key := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "MAGPIE_ADDR"} {
		if value := p.variables[key]; value != "" {
			env = append(env, key+"="+value)
		}
	}
	return env
}

// probeGateway asks an address for Magpie's self-report and, when that
// succeeds, reads the serving process to establish its configuration.
func probeGateway(address string) gatewayProbe {
	base, err := parseGatewayAddress(address)
	if err != nil {
		// The input may carry credentials, such as the managed instance's own
		// web key in a query string, so only a redacted form is reported.
		return gatewayProbe{Address: redactAddress(address), State: gatewayUnknown, Reason: reasonNotLoopback + ": " + err.Error()}
	}
	probe := gatewayProbe{Address: base, State: gatewayOffline}
	client := &http.Client{
		Timeout:   3 * time.Second,
		Transport: &http.Transport{Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Get(base + "/")
	if err != nil {
		return probe
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil || len(body) > 64<<10 || !json.Valid(body) {
		probe.State, probe.Reason = gatewayNotMagpie, reasonNotMagpie
		return probe
	}
	var report struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		PID     int    `json:"pid"`
		Serves  string `json:"serves"`
	}
	// A body that is valid JSON but not an object, such as a bare number or an
	// array, decodes with an error; both cases mean the service is not Magpie.
	if err := json.Unmarshal(body, &report); err != nil {
		probe.State, probe.Reason = gatewayNotMagpie, reasonNotMagpie
		return probe
	}
	probe.Version, probe.PID, probe.Serves = report.Version, report.PID, report.Serves
	if response.StatusCode != http.StatusOK || report.Name != "magpie" {
		probe.State, probe.Reason = gatewayNotMagpie, reasonNotMagpie
		return probe
	}
	if report.PID <= 1 || report.Serves == "" {
		probe.State, probe.Reason = gatewayUnknown, reasonNoServingProcess
		return probe
	}
	facts, err := readProcessFacts(report.PID)
	if err != nil {
		probe.State, probe.Reason = gatewayUnknown, processFactsReason(err)
		return probe
	}
	probe.Executable = facts.Executable
	probe.variables = facts.Variables
	resolution := resolveMagpieConfig(facts.Executable, facts.Variables)
	if resolution.Reason != "" {
		probe.State, probe.Reason = gatewayUnknown, resolution.Reason
		return probe
	}
	probe.State, probe.ConfigDir, probe.ConfigEvidence = gatewayVerified, resolution.Location, resolution.Evidence
	probe.ConfigWarning = resolution.Warning
	return probe
}

func processFactsReason(err error) string {
	switch {
	case errors.Is(err, errProcessDifferentUser):
		return reasonProcessOtherUser
	case errors.Is(err, errProcessGone):
		return reasonProcessEnded
	case errors.Is(err, errProcessUnsupported):
		return reasonGatewayUnsupported
	case errors.Is(err, errProcessReplaced):
		return reasonProcessReplaced
	default:
		return reasonProcessUnreadable
	}
}

// parseGatewayAddress accepts a loopback IP and port, with or without the
// http scheme, and returns it as http://IP:PORT.
func parseGatewayAddress(address string) (string, error) {
	trimmed := strings.TrimSpace(address)
	if trimmed == "" {
		return "", errors.New("a Magpie address is required")
	}
	hostport := trimmed
	if strings.Contains(trimmed, "://") {
		parsed, err := url.Parse(trimmed)
		if err != nil {
			return "", errors.New("use a Magpie address such as http://127.0.0.1:3425")
		}
		if parsed.Scheme != "http" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
			return "", errors.New("use a local Magpie HTTP address without credentials, path or query parameters")
		}
		hostport = parsed.Host
	}
	normalized, err := parseLoopbackAddress(hostport)
	if err != nil {
		return "", err
	}
	return "http://" + normalized, nil
}

// parseLoopbackAddress accepts host:port only when host is a loopback IP.
func parseLoopbackAddress(hostport string) (string, error) {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return "", errors.New("use a loopback address and port such as 127.0.0.1:3425")
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return "", errors.New("use a loopback IP address; host names and other machines are not supported")
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return "", errors.New("use a port between 1 and 65535")
	}
	return net.JoinHostPort(ip.String(), strconv.Itoa(number)), nil
}

// redactAddress reports an address without ever echoing credentials. Only a
// validated loopback host and port are shown; anything else becomes a fixed
// placeholder, because a rejected string can carry userinfo or a key and
// cannot be trusted to be free of secrets.
func redactAddress(address string) string {
	trimmed := strings.TrimSpace(address)
	if trimmed == "" {
		return ""
	}
	if loopback, err := parseLoopbackAddress(hostportOf(trimmed)); err == nil {
		return "http://" + loopback
	}
	return redactedAddressPlaceholder
}

// hostportOf takes the host:port part of an address without decoding
// credentials or query values from it.
func hostportOf(address string) string {
	rest := address
	if _, after, found := strings.Cut(rest, "://"); found {
		rest = after
	}
	if cut, _, found := strings.Cut(rest, "/"); found {
		rest = cut
	}
	if cut, _, found := strings.Cut(rest, "?"); found {
		rest = cut
	}
	if cut, _, found := strings.Cut(rest, "#"); found {
		rest = cut
	}
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		rest = rest[at+1:]
	}
	return rest
}

// gatewayAddressOf returns the host:port form MAGPIE_ADDR uses.
func gatewayAddressOf(base string) string {
	return strings.TrimPrefix(strings.TrimSuffix(base, "/"), "http://")
}

func describeProbe(probe gatewayProbe) string {
	if probe.State == gatewayVerified {
		return fmt.Sprintf("verified at %s (version %s, process %d, serving %s, configuration %s)", probe.Address, probe.Version, probe.PID, probe.Serves, probe.ConfigDir)
	}
	if probe.Reason != "" {
		return fmt.Sprintf("%s at %s (%s)", probe.State, probe.Address, probe.Reason)
	}
	return fmt.Sprintf("%s at %s", probe.State, probe.Address)
}
