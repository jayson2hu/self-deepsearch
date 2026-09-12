package identity

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/argon2"
)

const testEncodedPassword = "$argon2id$v=19$m=65536,t=3,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func zeroPasswordKey(_, _ []byte, _, _ uint32, _ uint8, size uint32) []byte {
	return make([]byte, size)
}

func awaitPasswordState(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("password worker did not reach expected state")
		}
		time.Sleep(time.Millisecond)
	}
}

func passwordResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("password calculation did not finish")
		return nil
	}
}

func TestPasswordWorkerBoundsActiveAndWaitingWork(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	worker := newPasswordWorker(1, 2, 2*time.Second, func(_, _ []byte, _, _ uint32, _ uint8, size uint32) []byte {
		<-release
		return make([]byte, size)
	})
	result := make(chan error, 3)
	for index := 0; index < 3; index++ {
		go func() { _, err := worker.hash(context.Background(), "a-long-test-password"); result <- err }()
	}
	awaitPasswordState(t, func() bool { stats := worker.stats(); return stats.Active == 1 && stats.Waiting == 2 })
	if _, err := worker.hash(context.Background(), "a-long-test-password"); !errors.Is(err, ErrAuthBusy) {
		t.Fatal("full queue was not rejected")
	}
	once.Do(func() { close(release) })
	for index := 0; index < 3; index++ {
		if err := passwordResult(t, result); err != nil {
			t.Fatal(err)
		}
	}
	stats := worker.stats()
	if stats.Active != 0 || stats.Waiting != 0 || stats.Rejected != 1 || stats.Completed != 3 {
		t.Fatalf("unexpected worker stats: %+v", stats)
	}
}

func TestPasswordWorkerQueueTimeoutAndCancellationReleaseWaiters(t *testing.T) {
	worker := newPasswordWorker(1, 2, 20*time.Millisecond, zeroPasswordKey)
	worker.active <- struct{}{} // Hold capacity without doing a real calculation.
	if _, err := worker.hash(context.Background(), "a-long-test-password"); !errors.Is(err, ErrAuthBusy) {
		t.Fatal("queue deadline not enforced")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	worker.waitTimeout = time.Second
	go func() { _, err := worker.hash(ctx, "a-long-test-password"); result <- err }()
	awaitPasswordState(t, func() bool { return worker.stats().Waiting == 1 })
	cancel()
	if err := passwordResult(t, result); !errors.Is(err, context.Canceled) {
		t.Fatal("queued cancellation lost")
	}
	<-worker.active
	if _, err := worker.hash(context.Background(), "a-long-test-password"); err != nil {
		t.Fatal("permit leaked")
	}
	stats := worker.stats()
	if stats.Active != 0 || stats.Waiting != 0 || stats.Rejected != 1 || stats.Canceled != 1 || stats.Completed != 1 {
		t.Fatalf("unexpected worker stats: %+v", stats)
	}
}

func TestPasswordWorkerCancellationHoldsActivePermitAndDiscardsKey(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	key := []byte{1, 2, 3}
	worker := newPasswordWorker(1, 0, time.Second, func(_, _ []byte, _, _ uint32, _ uint8, _ uint32) []byte { close(started); <-release; return key })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		hash, err := worker.hash(ctx, "a-long-test-password")
		if hash != "" {
			result <- errors.New("canceled computation returned credentials")
			return
		}
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("deriver did not start")
	}
	cancel()
	select {
	case <-result:
		t.Fatal("active computation detached on cancel")
	default:
	}
	if _, err := worker.hash(context.Background(), "a-long-test-password"); !errors.Is(err, ErrAuthBusy) {
		t.Fatal("cancellation released active permit early")
	}
	once.Do(func() { close(release) })
	if err := passwordResult(t, result); !errors.Is(err, context.Canceled) {
		t.Fatal("active cancellation lost")
	}
	if worker.stats().Active != 0 || worker.stats().Canceled != 1 {
		t.Fatal("canceled work leaked permit or accounting")
	}
	for _, value := range key {
		if value != 0 {
			t.Fatal("canceled derived key not cleared")
		}
	}
}

func TestPasswordWorkerPreCanceledContextAndPanicDoNotLeak(t *testing.T) {
	var calls int
	worker := newPasswordWorker(1, 0, time.Second, func(_, _ []byte, _, _ uint32, _ uint8, _ uint32) []byte { calls++; panic("test deriver panic") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := worker.hash(ctx, "a-long-test-password"); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatal("pre-canceled request computed a password")
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected deriver panic")
			}
		}()
		_, _ = worker.hash(context.Background(), "a-long-test-password")
	}()
	if worker.stats().Active != 0 || worker.stats().Waiting != 0 {
		t.Fatal("panic leaked permit")
	}
}

func TestPasswordComputationErrorsNeverCreateCredentials(t *testing.T) {
	for _, outcome := range []string{"busy", "canceled_during_work"} {
		for _, path := range []string{"signup", "login", "missing_login", "reauth", "reset", "invitation"} {
			t.Run(outcome+"/"+path, func(t *testing.T) {
				repository := &fakeRepository{sessionUser: User{ID: "user", PasswordHash: testEncodedPassword}, loginUser: User{ID: "user", PasswordHash: testEncodedPassword}}
				if path == "missing_login" {
					repository.loginUser = User{}
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				worker := newPasswordWorker(1, 0, time.Second, zeroPasswordKey)
				want := ErrAuthBusy
				if outcome == "busy" {
					worker.active <- struct{}{}
				} else {
					want = context.Canceled
					worker.deriveKey = func(_, _ []byte, _, _ uint32, _ uint8, size uint32) []byte { cancel(); return make([]byte, size) }
				}
				service := &Service{repository: repository, passwords: worker, now: time.Now, pepper: []byte(strings.Repeat("p", 32)), dummyPasswordHash: testEncodedPassword}
				var err error
				var token string
				var user User
				switch path {
				case "signup":
					user, token, err = service.Signup(ctx, "person@example.test", "123456", "a-long-test-password", "127.0.0.1")
				case "login", "missing_login":
					user, token, err = service.Login(ctx, "person@example.test", "a-long-test-password", "127.0.0.1", "request")
				case "reauth":
					token, err = service.Reauthenticate(ctx, "session", "a-long-test-password", "127.0.0.1", "request")
				case "reset":
					err = service.ResetPassword(ctx, "person@example.test", "123456", "a-long-test-password")
				case "invitation":
					user, token, err = service.AcceptInvitation(ctx, "person@example.test", "123456", "a-long-test-password", "127.0.0.1", "request")
				}
				if !errors.Is(err, want) || token != "" || user.ID != "" {
					t.Fatalf("computation error was not propagated safely: %v", err)
				}
				if repository.createdSession.TokenHash != "" || repository.accepted.PasswordHash != "" || repository.signup.PasswordHash != "" || repository.reset.PasswordHash != "" || len(repository.loginResults) != 0 {
					t.Fatal("failed calculation wrote credentials or false login audit")
				}
			})
		}
	}
}

func TestPasswordFinalMutationsUsePostComputationTime(t *testing.T) {
	for _, path := range []string{"signup", "reset", "invitation", "login", "reauth"} {
		t.Run(path, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Second)
			want := now.Add(time.Minute)
			repository := &fakeRepository{sessionUser: User{ID: "user", PasswordHash: testEncodedPassword}, loginUser: User{ID: "user", PasswordHash: testEncodedPassword, Role: "user"}}
			worker := newPasswordWorker(1, 0, time.Second, func(_, _ []byte, _, _ uint32, _ uint8, size uint32) []byte { now = want; return make([]byte, size) })
			service := &Service{repository: repository, passwords: worker, now: func() time.Time { return now }, pepper: []byte(strings.Repeat("p", 32))}
			var err error
			var finalTime time.Time
			switch path {
			case "signup":
				_, _, err = service.Signup(context.Background(), "person@example.test", "123456", "a-long-test-password", "127.0.0.1")
				finalTime = repository.signup.Now
			case "reset":
				err = service.ResetPassword(context.Background(), "person@example.test", "123456", "a-long-test-password")
				finalTime = repository.reset.Now
			case "invitation":
				_, _, err = service.AcceptInvitation(context.Background(), "person@example.test", "123456", "a-long-test-password", "127.0.0.1", "request")
				finalTime = repository.accepted.Now
			case "login":
				_, _, err = service.Login(context.Background(), "person@example.test", "a-long-test-password", "127.0.0.1", "request")
				finalTime = repository.createdSession.CreatedAt
			case "reauth":
				var token string
				token, err = service.Reauthenticate(context.Background(), "session", "a-long-test-password", "127.0.0.1", "request")
				now = want.Add(ReauthDuration - time.Second)
				if err == nil {
					err = service.ValidateReauthentication("session", token)
				}
				finalTime = want
			}
			if err != nil || !finalTime.Equal(want) {
				t.Fatalf("mutation used stale pre-computation timestamp: %v", err)
			}
		})
	}
}

func TestPasswordWorkerRealArgon2Burst(t *testing.T) {
	var active, peak atomic.Int32
	worker := newPasswordWorker(1, 2, 500*time.Millisecond, func(password, salt []byte, iterations, memory uint32, threads uint8, size uint32) []byte {
		count := active.Add(1)
		for old := peak.Load(); count > old && !peak.CompareAndSwap(old, count); old = peak.Load() {
		}
		defer active.Add(-1)
		return argon2.IDKey(password, salt, iterations, memory, threads, size)
	})
	start := make(chan struct{})
	results := make(chan error, 8)
	for index := 0; index < 8; index++ {
		go func() { <-start; _, err := worker.hash(context.Background(), "a-long-test-password"); results <- err }()
	}
	close(start)
	for index := 0; index < 8; index++ {
		if err := passwordResult(t, results); err != nil && !errors.Is(err, ErrAuthBusy) {
			t.Fatal(err)
		}
	}
	stats := worker.stats()
	if peak.Load() != 1 || stats.Active != 0 || stats.Waiting != 0 || stats.Completed+stats.Rejected != 8 {
		t.Fatalf("burst exceeded limits: peak=%d stats=%+v", peak.Load(), stats)
	}
	t.Logf("real Argon2 burst: peak=%d completed=%d rejected=%d", peak.Load(), stats.Completed, stats.Rejected)
}

func TestPasswordServicesShareProcessGate(t *testing.T) {
	for index := 0; index < 2; index++ {
		service, err := NewService(&fakeRepository{}, &fakeEmail{}, strings.Repeat("p", 32), time.Now)
		if err != nil {
			t.Fatal(err)
		}
		if service.passwords != processPasswords {
			t.Fatal("service bypassed process-wide password gate")
		}
	}
	stats := processPasswords.stats()
	if stats.ActiveLimit != 1 || stats.WaitingLimit != 2 || stats.WaitTimeoutSeconds != 0.5 {
		t.Fatal("Release A resource defaults changed")
	}
}
