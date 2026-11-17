package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// Harbor owns exactly one Magpie instance, in its own state directory. It
// copies the packaged Magpie CLI, marks it portable so the configuration
// lives beside it, and runs it in the background independently of Herdr and
// of any management UI.
type managedLayout struct {
	Root       string
	Executable string
	Marker     string
	DataDir    string
	Home       string
	WebKey     string
	Record     string
}

func managedLayoutFor(root string) managedLayout {
	bin := filepath.Join(root, "bin")
	return managedLayout{
		Root: root, Executable: filepath.Join(bin, "magpie"), Marker: filepath.Join(bin, ".portable"),
		DataDir: filepath.Join(bin, "data"), Home: filepath.Join(root, "home"),
		WebKey: filepath.Join(root, "web.key"), Record: filepath.Join(root, "instance.json"),
	}
}

// lockManagedLifecycle serializes start, stop and upgrade for the managed
// instance. The registry is locked only after startup, so without this two
// concurrent starts could each start a child and one ownership record would
// be lost, orphaning the other gateway.
func lockManagedLifecycle(layout managedLayout) (func(), error) {
	if err := os.MkdirAll(layout.Root, 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(layout.Root, "lifecycle.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		file.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		file.Close()
	}, nil
}

func managedInstanceState() (managedLayout, error) {
	root, err := bridgeHome()
	if err != nil {
		return managedLayout{}, err
	}
	if err := ensureHome(root); err != nil {
		return managedLayout{}, err
	}
	return managedLayoutFor(filepath.Join(root, "magpie")), nil
}

// managedRecord is the only evidence Harbor accepts for owning a process: the
// PID it started, the executable it started from, and the address it served.
type managedStartOptions struct {
	Port    int
	WebPort int
}

// bundledMagpiePath is the Magpie CLI shipped with Agent Harbor.
func bundledMagpiePath() (string, error) {
	if override := os.Getenv("AGENT_HARBOR_MAGPIE_BIN"); override != "" {
		if info, err := os.Stat(override); err != nil || info.IsDir() {
			return "", fmt.Errorf("AGENT_HARBOR_MAGPIE_BIN does not name a Magpie executable: %s", override)
		}
		return override, nil
	}
	executable, err := resolvedExecutable()
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(filepath.Dir(executable), "magpie")
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return candidate, nil
	}
	return "", errors.New("the packaged Magpie CLI was not found beside agent-harbor; set AGENT_HARBOR_MAGPIE_BIN")
}

// startManagedMagpie starts the instance Harbor owns. It never touches a
// gateway it did not start.
func startManagedMagpie(options managedStartOptions) error {
	layout, err := managedInstanceState()
	if err != nil {
		return err
	}
	unlock, err := lockManagedLifecycle(layout)
	if err != nil {
		return err
	}
	defer unlock()
	bundled, err := bundledMagpiePath()
	if err != nil {
		return err
	}
	record, err := readManagedRecord(layout)
	if err != nil {
		return err
	}
	// New managed instances start without Codex account pooling. Keep an
	// existing instance's mode across restarts instead of changing its users.
	callerOnly := record != nil && record.CodexCallerOnly
	if record == nil {
		_, dataErr := os.Stat(layout.DataDir)
		callerOnly = os.IsNotExist(dataErr)
	}
	if record != nil && record.PID > 1 {
		probe := probeGateway(record.Address)
		switch managedStopAction(record, probe, probeFacts(probe)) {
		case managedStopNotRunning:
		case managedStopUnverifiable:
			return fmt.Errorf("the managed Magpie at %s does not answer and its process %d cannot be read; refusing to start a second one", record.Address, record.PID)
		default:
			return fmt.Errorf("the managed Magpie is already running at %s", record.Address)
		}
	}

	gatewayPort, webPort, err := managedPorts(layout, record, options)
	if err != nil {
		return err
	}
	// A recorded pair that something else now holds is re-picked once rather
	// than leaving the instance permanently unstartable. An explicitly
	// requested pair is reported instead.
	if holder, label := heldPort(gatewayPort, webPort); holder != nil {
		if options.Port != 0 {
			return fmt.Errorf("cannot start the managed Magpie: its %s port is held by %s", label, describePortHolder(*holder))
		}
		if gatewayPort, webPort, err = pickPortPair(defaultMagpiePort); err != nil {
			return err
		}
		if holder, label := heldPort(gatewayPort, webPort); holder != nil {
			return fmt.Errorf("cannot start the managed Magpie: its %s port is held by %s", label, describePortHolder(*holder))
		}
	}
	address := fmt.Sprintf("http://127.0.0.1:%d", gatewayPort)
	webAddress := fmt.Sprintf("http://127.0.0.1:%d", webPort)

	if err := ensurePortableMarker(layout); err != nil {
		return err
	}
	if _, err := installBundledMagpie(layout, bundled, 0); err != nil {
		return err
	}
	webKey, err := ensureWebKey(layout)
	if err != nil {
		return err
	}
	if err := disableManagedExtras(layout, address); err != nil {
		return err
	}
	digest, err := fileDigest(layout.Executable)
	if err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(layout.Root, "serve.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	startedAt := time.Now().UTC()
	cmd := exec.Command(layout.Executable, "serve", "--web="+gatewayAddressOf(webAddress))
	cmd.Env = managedProcessEnvironment(layout, address, webKey, startedAt)
	if callerOnly {
		value := "1"
		cmd.Env = replaceEnvironment(cmd.Env, map[string]*string{"MAGPIE_CODEX_CALLER_ONLY": &value})
	}
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Dir = layout.Root
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("the managed Magpie could not start: %w", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	if err := waitForManagedReady(gatewayPort, cmd.Process.Pid, exited); err != nil {
		_ = cmd.Process.Kill()
		return err
	}
	owned := true
	defer func() {
		// Until the record is persisted, a failure must not leave a detached
		// gateway that no record can stop.
		if owned {
			_ = cmd.Process.Kill()
		}
	}()
	probe := probeGateway(address)
	if probe.PID != cmd.Process.Pid {
		return fmt.Errorf("the managed Magpie answered as PID %d instead of the started PID %d; it was stopped", probe.PID, cmd.Process.Pid)
	}
	if err := writeManagedRecord(layout, managedRecord{
		Address: address, WebAddress: webAddress, ConfigDir: probe.ConfigDir, Executable: probe.Executable,
		Version: probe.Version, Digest: digest, PID: cmd.Process.Pid, StartedAt: startedAt,
		CodexCallerOnly: requireCallerOnlyGateway(address) == nil,
	}); err != nil {
		return fmt.Errorf("the managed Magpie was stopped because its record could not be written: %w", err)
	}
	owned = false
	if _, err := registerManagedMagpie(probe); err != nil {
		return err
	}
	fmt.Printf("Managed Magpie started\n  Address     %s\n  Configuration %s\n  Web page    %s (private key stored in %s)\n", address, probe.ConfigDir, webAddress, layout.WebKey)
	return nil
}

func probeFacts(probe gatewayProbe) processFacts {
	if probe.State != gatewayVerified {
		return processFacts{}
	}
	facts, err := readProcessFacts(probe.PID)
	if err != nil {
		return processFacts{}
	}
	return facts
}

// managedPorts keeps the recorded ports while the instance exists and picks a
// free pair only for a new instance.
func waitForManagedReady(port, pid int, exited <-chan error) error {
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	address := fmt.Sprintf("http://127.0.0.1:%d", port)
	for {
		select {
		case err := <-exited:
			// The child is reaped; the caller must not wait on it again.
			exited = nil
			if err == nil {
				return errors.New("the managed Magpie exited during startup; see serve.log")
			}
			return fmt.Errorf("the managed Magpie stopped during startup: %v; see serve.log", err)
		case <-ticker.C:
			if managedReady(pid, probeGateway(address)) {
				return nil
			}
		case <-deadline.C:
			return errors.New("the managed Magpie did not become ready; it was stopped")
		}
	}
}

func stopManagedMagpie() error {
	layout, err := managedInstanceState()
	if err != nil {
		return err
	}
	unlock, err := lockManagedLifecycle(layout)
	if err != nil {
		return err
	}
	defer unlock()
	record, err := readManagedRecord(layout)
	if err != nil {
		return err
	}
	var probe gatewayProbe
	if record != nil && record.Address != "" {
		probe = probeGateway(record.Address)
	}
	action := managedStopAction(record, probe, probeFacts(probe))
	switch action {
	case managedStopNotRecorded:
		return errors.New("no managed Magpie is recorded; nothing was stopped")
	case managedStopNotRunning:
		if record != nil && record.PID > 1 {
			record.PID, record.StartedAt = 0, time.Time{}
			if err := writeManagedRecord(layout, *record); err != nil {
				return err
			}
		}
		fmt.Println("The managed Magpie is not running.")
		return nil
	case managedStopUnverifiable:
		return fmt.Errorf("the managed Magpie at %s does not answer and its process %d cannot be read; the record was kept. Check whether PID %d is still running before starting again", record.Address, record.PID, record.PID)
	case managedStopForeign:
		if probe.State != gatewayVerified {
			return fmt.Errorf("the gateway at %s cannot be verified as the managed instance; it was left alone", record.Address)
		}
		return fmt.Errorf("the process at %s (PID %d) is not the managed Magpie Harbor started; it was left alone", record.Address, probe.PID)
	}
	// The pid is re-checked against the record before any signal, so a pid that
	// was recycled between the probe and here is never signalled, and evidence
	// that cannot be read never clears the record.
	switch recordedProcessEvidence(*record) {
	case processEvidenceMatches:
	case processEvidenceUnreadable:
		return fmt.Errorf("the process %d for the managed Magpie at %s cannot be read; the record was kept and nothing was signalled", record.PID, record.Address)
	default:
		record.PID, record.StartedAt = 0, time.Time{}
		if err := writeManagedRecord(layout, *record); err != nil {
			return err
		}
		fmt.Println("The managed Magpie is not running.")
		return nil
	}
	if err := syscall.Kill(record.PID, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		// The child is gone when its pid is free or belongs to another
		// process; unreadable evidence keeps waiting rather than clearing the
		// record on uncertainty.
		if recordedProcessEvidence(*record) == processEvidenceGone {
			record.PID, record.StartedAt = 0, time.Time{}
			if err := writeManagedRecord(layout, *record); err != nil {
				return err
			}
			fmt.Println("Managed Magpie stopped.")
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("the managed Magpie at %s did not stop; it is still running as PID %d", record.Address, record.PID)
}

// managedWebURL is the managed instance's own page, with the key only in the
// URL handed to the browser opener.
