package main

import "testing"

func TestAutomaticNameChangeDoesNotInvalidateSession(t *testing.T) {
	before := session{ID: "0123456789abcdef0123456789abcdef", Name: "project · local", AutoNamePending: true, AutoNameSync: true}
	after := before
	after.Name = "New Codex title"
	after.AutoNamePending = false
	if !sameSession(before, after) {
		t.Fatal("automatic title update invalidated the session")
	}
	if sessionStamp(before) != sessionStamp(after) {
		t.Fatal("automatic title update changed the pane launch stamp")
	}
}

func TestManualRenameAndRecoveryChangesStillInvalidateSession(t *testing.T) {
	before := session{ID: "0123456789abcdef0123456789abcdef", Name: "Existing name"}
	manualRename := before
	manualRename.Name = "User's name"
	if sameSession(before, manualRename) || sessionStamp(before) == sessionStamp(manualRename) {
		t.Fatal("manual rename did not invalidate the reviewed session")
	}
	automatic := before
	automatic.AutoNameSync = true
	changedRecovery := automatic
	changedRecovery.CodexSessionID = "01912345-6789-7abc-8def-0123456789ab"
	if sameSession(automatic, changedRecovery) || sessionStamp(automatic) == sessionStamp(changedRecovery) {
		t.Fatal("recovery identity change did not invalidate the reviewed session")
	}
}
