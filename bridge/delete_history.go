package main

import "errors"

const historyRemovalExplanation = "Harbor no longer deletes client conversation history. Use the client’s own tools to manage history. Remove only the Harbor record with x or the remove command; conversation history is kept."

// DeleteHistory refuses stale callers without touching records or client state.
func (harborService) DeleteHistory(sessionApproval, string) error {
	return errors.New(historyRemovalExplanation)
}
