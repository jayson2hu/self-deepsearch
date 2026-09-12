package logsafe

import (
	"context"
	"errors"
)

// ErrorClass keeps potentially sensitive wrapped error text out of application logs.
func ErrorClass(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return "internal"
	}
}
