package secretinput

import "errors"

// errRedacted is the opaque stand-in returned by RedactError.
var errRedacted = errors.New("secretinput: redacted error")

// RedactError returns an error that never embeds the original message payload.
// Use when wrapping untrusted/framework errors that might mention paste content.
func RedactError(err error) error {
	if err == nil {
		return nil
	}
	return errRedacted
}
