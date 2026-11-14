package main

import (
	"errors"
	"os"
)

// This public integration subset supplies executable data, not a guarantee of
// recovery or hook readiness. Aliased targets await an upstream kind contract.
func nativeClientProgram(client string) (string, error) {
	if os.Getenv("HERDR_SOCKET_PATH") == "" {
		return "", errors.New("native client discovery requires managed Herdr; reopen the workspace")
	}
	integrations, err := listHerdrIntegrations()
	if err != nil {
		return "", errors.New("managed Herdr integration discovery unavailable; reopen the workspace or inspect upstream support")
	}
	for _, v := range integrations {
		if v.Target == client {
			if !v.nativeExecutableReady() {
				return "", errors.New("native executable or canonical kind unavailable; inspect Herdr integration support")
			}
			return v.Command, nil
		}
	}
	return "", errors.New("client absent from Herdr integration subset; install a supported client or inspect upstream capabilities")
}
func candidateNeutral(s session) bool {
	return s.CLI != "codex"
}
