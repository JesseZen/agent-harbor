package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestPortableArchiveRoundTrip(t *testing.T) {
	want := portableArchive{Version: 1, Files: []portableFile{{Path: "core/config.yaml", Mode: 0o600, Data: []byte("schema_version: 1\n")}}}
	var encoded bytes.Buffer
	if err := writeArchive(&encoded, want); err != nil {
		t.Fatal(err)
	}
	got, err := readArchive(bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != want.Version || len(got.Files) != 1 || !bytes.Equal(got.Files[0].Data, want.Files[0].Data) {
		t.Fatalf("round trip mismatch: %#v", got)
	}
}

func TestPortableArchiveRejectsTraversal(t *testing.T) {
	value := portableArchive{Version: 1, Files: []portableFile{{Path: "../outside", Data: []byte("x")}}}
	var encoded bytes.Buffer
	if err := writeArchive(&encoded, value); err != nil {
		t.Fatal(err)
	}
	if _, err := readArchive(bytes.NewReader(encoded.Bytes())); err == nil {
		t.Fatal("expected traversal rejection")
	}
}

func TestRewritePortableConfigRehomesStateAndManagedSecret(t *testing.T) {
	if got := rewriteManagedAccount("agent-harbor/ins_old/credential_main/2", "ins_0123456789abcdef0123456789abcdef"); got == "agent-harbor/ins_old/credential_main/2" {
		t.Fatal("managed account was not rewritten")
	}
	source := []byte("instance:\n  state_root: /old\ncredentials:\n  - secret:\n      exportable: false\n      keychain:\n        service: agent-harbor\n        account: agent-harbor/ins_old/credential_main/2\n")
	got, err := rewritePortableConfig(source, "/new/state", "ins_0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte("state_root: /new/state")) || !bytes.Contains(got, []byte("path: /new/state/secrets/ins_0123456789abcdef0123456789abcdef/credential_main/2.secret")) || bytes.Contains(got, []byte("keychain:")) {
		t.Fatalf("unexpected rewritten config:\n%s", got)
	}
}

func TestRewritePortableConfigRehomesManagedFileLocator(t *testing.T) {
	source := []byte("instance:\n  state_root: /old\ncredentials:\n  - id: credential_main\n    secret:\n      file:\n        path: /old/state/secrets/ins_0123456789abcdef0123456789abcdef/credential_main/2.secret\n")
	got, err := rewritePortableConfig(source, "/new/state", "ins_fedcba9876543210fedcba9876543210")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte("/new/state/secrets/ins_fedcba9876543210fedcba9876543210/credential_main/2.secret")) {
		t.Fatalf("managed file locator was not rehomed:\n%s", got)
	}
}

func TestPromoteMaterializedFilesCreatesManagedLocator(t *testing.T) {
	source := []byte("credentials:\n  - id: credential_main\n    generation: 2\n    secret:\n      exportable: true\n      file:\n        path: __AGENT_HARBOR_STATE_ROOT__/secrets/external/1.secret\n")
	files := []portableFile{{Path: "core/secrets/external/1.secret", Data: []byte("secret")}}
	got, err := promoteMaterializedFiles(source, &files)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(got, []byte("secrets/external")) || !bytes.Contains(got, []byte("service: agent-harbor")) || !strings.HasPrefix(files[0].Path, "core/secrets/ins_") {
		t.Fatalf("materialized secret was not promoted: config=%s path=%s", got, files[0].Path)
	}
}
