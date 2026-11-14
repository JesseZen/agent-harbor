package main

import (
	"errors"
	"fmt"
	"net"
	"strconv"
)

// A held port is reported with its holder instead of starting a serve that
// would never bind. The managed instance's web page is always its gateway
// port plus one, so a pair is chosen and re-used together.

const (
	defaultMagpiePort = 3435
	defaultWebPort    = 3436
)

const (
	portFree         = "free"
	portMagpie       = "magpie"
	portOtherProgram = "other-program"
)

type portHolder struct {
	State  string
	Probe  gatewayProbe
	Detail string
}

func inspectPort(address string) portHolder {
	probe := probeGateway(address)
	switch probe.State {
	case gatewayVerified, gatewayUnknown, gatewayNotMagpie:
		return portHolder{State: portMagpie, Probe: probe, Detail: probe.ConfigDir}
	case gatewayOffline:
	}
	base, err := parseGatewayAddress(address)
	if err != nil {
		return portHolder{State: portOtherProgram, Detail: err.Error()}
	}
	listener, err := net.Listen("tcp", gatewayAddressOf(base))
	if err != nil {
		return portHolder{State: portOtherProgram, Detail: "another program holds " + gatewayAddressOf(base)}
	}
	listener.Close()
	return portHolder{State: portFree}
}

// pickPortPair returns the first consecutive pair at or after the preferred
// pair that no process holds.
// pickPortPair finds the first free consecutive pair at or after start. The
// managed instance's web page is always its gateway port plus one, so both
// halves are chosen together.
// heldPort reports which half of a pair is held, if either is.

func heldPort(gatewayPort, webPort int) (*portHolder, string) {
	for _, candidate := range []struct {
		address string
		label   string
	}{
		{fmt.Sprintf("http://127.0.0.1:%d", gatewayPort), "gateway"},
		{fmt.Sprintf("http://127.0.0.1:%d", webPort), "web"},
	} {
		holder := inspectPort(candidate.address)
		if holder.State != portFree {
			return &holder, candidate.label
		}
	}
	return nil, ""
}

func pickPortPair(start int) (int, int, error) {
	if start < 1 {
		start = 1
	}
	for port := start; port < 65535; port++ {
		if portHasNoListener(port) && portHasNoListener(port+1) {
			return port, port + 1, nil
		}
	}
	return 0, 0, errors.New("no free consecutive ports are available for the managed Magpie")
}

func portHasNoListener(port int) bool {
	listener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return false
	}
	listener.Close()
	return true
}

func managedPorts(layout managedLayout, record *managedRecord, options managedStartOptions) (int, int, error) {
	if options.Port != 0 && options.WebPort == 0 {
		options.WebPort = options.Port + 1
	}
	if options.Port == 0 && options.WebPort != 0 {
		return 0, 0, errors.New("give both ports or neither")
	}
	if options.Port != 0 {
		if options.Port < 1 || options.Port > 65534 {
			return 0, 0, errors.New("use a gateway port between 1 and 65534")
		}
		web := options.WebPort
		if web < 1 || web > 65535 || web == options.Port {
			return 0, 0, errors.New("use a separate web port")
		}
		return options.Port, web, nil
	}
	if record != nil && record.Address != "" {
		gateway, err := portOfAddress(record.Address)
		if err != nil {
			return 0, 0, err
		}
		web := gateway + 1
		if record.WebAddress != "" {
			parsed, err := portOfAddress(record.WebAddress)
			if err != nil {
				return 0, 0, fmt.Errorf("the recorded web address %q cannot be read: %w", record.WebAddress, err)
			}
			web = parsed
		}
		return gateway, web, nil
	}
	return pickPortPair(defaultMagpiePort)
}

func portOfAddress(address string) (int, error) {
	base, err := parseGatewayAddress(address)
	if err != nil {
		return 0, err
	}
	_, port, err := net.SplitHostPort(gatewayAddressOf(base))
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(port)
}

func describePortHolder(holder portHolder) string {
	switch holder.State {
	case portMagpie:
		if holder.Probe.State == gatewayVerified && holder.Probe.ConfigDir != "" {
			return fmt.Sprintf("Magpie with configuration %s", holder.Probe.ConfigDir)
		}
		if holder.Probe.State == gatewayVerified {
			return fmt.Sprintf("Magpie version %s (configuration unknown)", holder.Probe.Version)
		}
		return "a Magpie whose configuration could not be verified"
	case portFree:
		return "nothing"
	default:
		if holder.Detail != "" {
			return "another program (" + holder.Detail + ")"
		}
		return "another program"
	}
}

// stopManagedMagpie stops only the process recorded as Harbor's own.
