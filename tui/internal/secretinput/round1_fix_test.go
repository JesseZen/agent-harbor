package secretinput_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/secretinput"
)

func TestNoteLossOnlyClearsMatchingStageID(t *testing.T) {
	exp := time.Now().UTC().Add(time.Hour)
	fake := &fakeStageHTTP{
		createFn: func(context.Context, string, []byte) (*generated.CreateProviderSecretStageResponse, error) {
			return buildCreate(201, "stage-s2", exp, ""), nil
		},
	}
	stager := secretinput.NewStager(fake)
	buf := secretinput.New()
	if err := buf.PasteBytes(plantedToken()); err != nil {
		t.Fatalf("paste: %v", err)
	}
	res := stager.Stage(context.Background(), buf)
	if !res.OK() || res.StageID != "stage-s2" {
		t.Fatalf("stage: %#v", res)
	}

	stager.NoteLoss("stage-s1", secretinput.CodeExpired)
	if stager.OwnedStageID() != "stage-s2" {
		t.Fatalf("mismatched NoteLoss must keep owned S2, got %q", stager.OwnedStageID())
	}

	stager.NoteLoss("stage-s2", secretinput.CodeExpired)
	if stager.OwnedStageID() != "" {
		t.Fatalf("matching NoteLoss must clear ownership, got %q", stager.OwnedStageID())
	}
}

func TestBufferBackspaceAndDeleteRune(t *testing.T) {
	b := secretinput.New()
	if err := b.PasteBytes([]byte{'A', 'B', 'C'}); err != nil {
		t.Fatalf("paste: %v", err)
	}
	b.Backspace()
	got := copyOut(t, b)
	if !bytes.Equal(got, []byte{'A', 'B'}) {
		t.Fatalf("backspace = %v", got)
	}
	b.MoveToStart()
	b.DeleteRune()
	got = copyOut(t, b)
	if !bytes.Equal(got, []byte{'B'}) {
		t.Fatalf("delete rune = %v", got)
	}
	b.SelectAll()
	b.Backspace()
	if b.HasContent() {
		t.Fatal("backspace selection must clear")
	}
}
