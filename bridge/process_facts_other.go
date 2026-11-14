//go:build !darwin && !linux

package main

// processFacts is unavailable where the platform offers no unprivileged way
// to read another process's executable and environment.
func readProcessFacts(pid int) (processFacts, error) {
	return processFacts{}, errProcessUnsupported
}
