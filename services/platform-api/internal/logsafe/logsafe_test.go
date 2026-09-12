package logsafe

import (
	"context"
	"errors"
	"testing"
)

func TestErrorClassDoesNotExposeWrappedErrorText(t *testing.T) {
	secret := errors.New("database rejected user@example.test with code 123456")
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{name: "internal", err: secret, want: "internal"},
		{name: "timeout", err: errors.Join(secret, context.DeadlineExceeded), want: "timeout"},
		{name: "canceled", err: errors.Join(secret, context.Canceled), want: "canceled"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := ErrorClass(test.err); got != test.want {
				t.Fatalf("expected %q, got %q", test.want, got)
			}
		})
	}
}
