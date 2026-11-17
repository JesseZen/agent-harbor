package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecoverIntegrationUsesBoundProfileAndPreservesChoices(t *testing.T) {
	for _, tc := range []struct {
		name           string
		kind           profileKind
		config, choice string
		install, fail  bool
	}{
		{"fresh isolated", profileIsolated, "", "", true, false},
		{"existing approved", profileExisting, "", "yes\n", true, false},
		{"existing skipped", profileExisting, "", "skip\n", false, false},
		{"existing cancelled", profileExisting, "", "\n", false, true},
		{"explicitly disabled cancelled", profileIsolated, "[features]\nhooks=false\n", "\n", false, true},
		{"explicitly disabled approved", profileIsolated, "[features]\nhooks=false\n", "yes\n", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			home, ambient := filepath.Join(root, "a"), filepath.Join(root, "b")
			os.Mkdir(home, 0700)
			os.Mkdir(ambient, 0700)
			if tc.config != "" {
				os.WriteFile(filepath.Join(home, "config.toml"), []byte(tc.config), 0600)
			}
			t.Setenv("CODEX_HOME", ambient)
			herdr := filepath.Join(root, "herdr")
			os.WriteFile(herdr, []byte(`#!/bin/sh
case "$*" in
 'integration status')
   if test -f "$CODEX_HOME/installed"; then echo 'codex: current (v10)'; else echo 'codex: not installed'; fi;;
 'integration install codex')
   test "$PWD" = "$CODEX_HOME" || exit 1
   printf '[features]\nhooks=true\n' > "$CODEX_HOME/config.toml"
   touch "$CODEX_HOME/installed";;
 *) exit 1;;
esac
`), 0700)
			binding := profileBinding{WorkspaceID: "workspace", ProfileID: "a", EnvironmentID: "a", Client: "codex"}
			s := session{CLI: "codex", Profile: &binding, WorkingDirectory: home}
			e := profileClientEnvironment{Binding: binding, Kind: tc.kind, Home: home}
			env := replaceEnvironment(os.Environ(), map[string]*string{"CODEX_HOME": &home})
			var out bytes.Buffer
			if tc.kind == profileExisting || strings.Contains(tc.config, "hooks=false") {
				if err := prepareCodexRecoveryChoice(s, e, herdr, env, "", nil, &out); !errors.Is(err, errRecoveryChoice) {
					t.Fatal(err)
				}
				if _, err := os.Stat(filepath.Join(home, "installed")); !os.IsNotExist(err) {
					t.Fatal("preflight installed without consent")
				}
			}
			err := prepareCodexRecovery(s, e, herdr, env, strings.NewReader(tc.choice), &out)
			if (err != nil) != tc.fail {
				t.Fatalf("err=%v output=%s", err, out.String())
			}
			_, installed := os.Stat(filepath.Join(home, "installed"))
			if (installed == nil) != tc.install {
				t.Fatalf("install=%v expected=%v", installed, tc.install)
			}
			files, _ := os.ReadDir(ambient)
			if len(files) != 0 {
				t.Fatal("ambient Profile was modified")
			}
			if tc.install {
				out.Reset()
				if err := prepareCodexRecovery(s, e, herdr, env, strings.NewReader(""), &out); err != nil || out.Len() != 0 {
					t.Fatal("prepared integration prompted again", err, out.String())
				}
			}
		})
	}
}

func TestRecoverIntegrationFailureDoesNotClaimReady(t *testing.T) {
	root := t.TempDir()
	herdr := filepath.Join(root, "herdr")
	os.WriteFile(herdr, []byte("#!/bin/sh\ncase \"$*\" in 'integration status') echo 'codex: not installed';; *) exit 1;; esac\n"), 0700)
	b := profileBinding{Client: "codex"}
	s := session{CLI: "codex", Profile: &b, WorkingDirectory: root}
	e := profileClientEnvironment{Binding: b, Home: root, Kind: profileIsolated}
	var out bytes.Buffer
	err := prepareCodexRecovery(s, e, herdr, []string{"PATH=" + os.Getenv("PATH"), "CODEX_HOME=" + root}, strings.NewReader(""), &out)
	if err == nil || strings.Contains(out.String(), "hooks prepared") {
		t.Fatal("failed install claimed success", err, out.String())
	}
	wrong := e
	wrong.Home = root + "-other"
	if err := prepareCodexRecovery(s, wrong, herdr, []string{"CODEX_HOME=" + root}, strings.NewReader("yes\n"), &out); err != ErrProfileIdentity {
		t.Fatal("wrong Profile accepted", err)
	}
}

func TestRecoverySkipAppliesOnlyToFirstLaunch(t *testing.T) {
	b := profileBinding{Client: "codex"}
	s := session{CLI: "codex", Profile: &b, RecoverySetupSkipped: true}
	e := profileClientEnvironment{Binding: b, Home: t.TempDir()}
	var out bytes.Buffer
	if err := prepareCodexRecovery(s, e, "/missing-herdr", []string{"CODEX_HOME=" + e.Home}, nil, &out); err != nil {
		t.Fatal(err)
	}
	wrong := e
	wrong.Binding.ProfileID = "other"
	if err := prepareCodexRecovery(s, wrong, "/missing-herdr", []string{"CODEX_HOME=" + e.Home}, nil, &out); err != ErrProfileIdentity {
		t.Fatal("skip bypassed Profile validation", err)
	}
	s.Started = true
	if err := prepareCodexRecovery(s, e, "/missing-herdr", []string{"CODEX_HOME=" + e.Home}, nil, &out); err == nil {
		t.Fatal("creation choice bypassed later recovery checks")
	}
}
