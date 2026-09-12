package identity

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/argon2"
)

// Release A has one API process with a 384 MiB / 0.4 CPU allocation. Bound
// expensive work across every identity endpoint, including unknown accounts.
// This is not an RSS limit: stored hashes may use up to 256 MiB and GC/other
// request allocations still require measurement on the target host.
var processPasswords = newPasswordWorker(1, 2, 500*time.Millisecond, argon2.IDKey)

type passwordDeriver func(password, salt []byte, time, memory uint32, threads uint8, keyLen uint32) []byte

type passwordWorker struct {
	active, waiting               chan struct{}
	waitTimeout                   time.Duration
	deriveKey                     passwordDeriver
	rejected, canceled, completed atomic.Uint64
}

// PasswordWorkStats contains process-wide aggregate values only, never account
// identifiers, network addresses, passwords or hash parameters.
type PasswordWorkStats struct {
	Active, Waiting, ActiveLimit, WaitingLimit int
	WaitTimeoutSeconds                         float64
	Rejected, Canceled, Completed              uint64
}

func newPasswordWorker(active, waiting int, timeout time.Duration, derive passwordDeriver) *passwordWorker {
	return &passwordWorker{active: make(chan struct{}, active), waiting: make(chan struct{}, waiting), waitTimeout: timeout, deriveKey: derive}
}

func (worker *passwordWorker) stats() PasswordWorkStats {
	return PasswordWorkStats{
		Active: len(worker.active), Waiting: len(worker.waiting), ActiveLimit: cap(worker.active), WaitingLimit: cap(worker.waiting),
		WaitTimeoutSeconds: worker.waitTimeout.Seconds(), Rejected: worker.rejected.Load(), Canceled: worker.canceled.Load(), Completed: worker.completed.Load(),
	}
}

func (service *Service) PasswordWorkStats() PasswordWorkStats { return service.passwords.stats() }

func (worker *passwordWorker) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case worker.active <- struct{}{}:
		return nil
	default:
	}
	select {
	case worker.waiting <- struct{}{}:
		defer func() { <-worker.waiting }()
	default:
		worker.rejected.Add(1)
		return ErrAuthBusy
	}
	deadline := time.Now().Add(worker.waitTimeout)
	timer := time.NewTimer(worker.waitTimeout)
	defer timer.Stop()
	select {
	case worker.active <- struct{}{}:
		// Timer and capacity can become ready together; a late permit must not
		// restart work after the queue deadline.
		if !time.Now().Before(deadline) {
			<-worker.active
			worker.rejected.Add(1)
			return ErrAuthBusy
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		worker.rejected.Add(1)
		return ErrAuthBusy
	}
}

func (worker *passwordWorker) derive(ctx context.Context, password, salt []byte, iterations, memory uint32, threads uint8, keyLen uint32) ([]byte, error) {
	if err := worker.acquire(ctx); err != nil {
		if ctx.Err() != nil {
			worker.canceled.Add(1)
		}
		return nil, err
	}
	defer func() { <-worker.active }()
	if err := ctx.Err(); err != nil {
		worker.canceled.Add(1)
		return nil, err
	}
	// Argon2 is synchronous and cannot be interrupted. Hold the permit until
	// it actually returns, even if the client disconnects during calculation.
	key := worker.deriveKey(password, salt, iterations, memory, threads, keyLen)
	worker.completed.Add(1)
	if err := ctx.Err(); err != nil {
		clear(key)
		worker.canceled.Add(1)
		return nil, err
	}
	return key, nil
}

func HashPassword(password string) (string, error) {
	return HashPasswordContext(context.Background(), password)
}

func HashPasswordContext(ctx context.Context, password string) (string, error) {
	return processPasswords.hash(ctx, password)
}

func (worker *passwordWorker) hash(ctx context.Context, password string) (string, error) {
	if err := validatePassword(password); err != nil {
		return "", err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	hash, err := worker.derive(ctx, []byte(password), salt, 3, 64*1024, 1, 32)
	if err != nil {
		return "", err
	}
	defer clear(hash)
	return fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=1$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}

// VerifyPassword is retained for offline callers. Request handlers must use
// the context-aware variant to distinguish overload from a wrong password.
func VerifyPassword(password, encoded string) bool {
	matched, err := VerifyPasswordContext(context.Background(), password, encoded)
	return err == nil && matched
}

func VerifyPasswordContext(ctx context.Context, password, encoded string) (bool, error) {
	return processPasswords.verify(ctx, password, encoded)
}

func (worker *passwordWorker) verify(ctx context.Context, password, encoded string) (bool, error) {
	var memory, iterations uint32
	var parallelism uint8
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false, nil
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return false, nil
	}
	if memory < 8*1024 || memory > 256*1024 || iterations < 1 || iterations > 10 || parallelism < 1 || parallelism > 8 {
		return false, nil
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 16 {
		return false, nil
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) != 32 {
		return false, nil
	}
	actual, err := worker.derive(ctx, []byte(password), salt, iterations, memory, parallelism, uint32(len(expected)))
	if err != nil {
		return false, err
	}
	defer clear(actual)
	return hmac.Equal(actual, expected), nil
}
