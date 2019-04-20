package persistence

import "testing"

func TestErrUnknownAccount(t *testing.T) {
	err := ErrUnknownAccount("unknown")
	if message := err.Error(); message != "unknown" {
		t.Errorf("Unexpected error message %s", message)
	}
}