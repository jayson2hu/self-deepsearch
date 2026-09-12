package logsafe

import (
	"context"
	"errors"
	"testing"
)

func TestErrorClassDoesNotExposeWrappedErrorText(t *testing.T) {
	secret := errors.New("postgres://user:secret@example.test/database")
	if got := ErrorClass(secret); got != "internal" {
		t.Fatalf("expected internal, got %q", got)
	}
	if got := ErrorClass(errors.Join(secret, context.DeadlineExceeded)); got != "timeout" {
		t.Fatalf("expected timeout, got %q", got)
	}
}
