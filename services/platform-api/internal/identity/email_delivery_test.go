package identity

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestRequestCodeReservesQuotaBeforeSendingAndCompletesAudit(t *testing.T) {
	repository := &fakeRepository{target: User{ID: "user-1", Email: "person@example.com"}, deliver: true}
	email := &fakeEmail{onSend: func(context.Context) {
		repository.deliverySteps = append(repository.deliverySteps, "send")
	}}
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	service, err := NewService(repository, email, "0123456789abcdef0123456789abcdef", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RequestCode(context.Background(), "person@example.com", PurposePasswordReset, nil, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(repository.deliverySteps, []string{"reserve", "send", "complete"}) {
		t.Fatalf("unexpected delivery order: %#v", repository.deliverySteps)
	}
	if repository.reservation.NotificationType != "password_code" || repository.reservation.UserID == nil || *repository.reservation.UserID != "user-1" {
		t.Fatalf("unexpected reservation: %#v", repository.reservation)
	}
	if !repository.completion.Succeeded || repository.completion.ErrorClass != "" {
		t.Fatalf("unexpected completion: %#v", repository.completion)
	}
}

func TestRequestCodeDoesNotCallSenderWhenDailyQuotaIsUnavailable(t *testing.T) {
	repository := &fakeRepository{
		target: User{ID: "user-1", Email: "person@example.com"}, deliver: true,
		reserveErr: ErrEmailDailyLimit,
	}
	email := &fakeEmail{}
	service, err := NewService(repository, email, "0123456789abcdef0123456789abcdef", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	err = service.RequestCode(context.Background(), "person@example.com", PurposePasswordReset, nil, "127.0.0.1")
	if !errors.Is(err, ErrEmailUnavailable) || !errors.Is(err, ErrEmailDailyLimit) {
		t.Fatalf("expected quota to be exposed only as email unavailability, got %v", err)
	}
	if email.code != "" || repository.completion.Ticket.ID != "" {
		t.Fatal("sender or completion was called without a quota reservation")
	}
}

func TestEmailSendUsesConfiguredTimeoutAndRecordsSafeFailureClass(t *testing.T) {
	repository := &fakeRepository{target: User{ID: "user-1", Email: "person@example.com"}, deliver: true}
	email := &fakeEmail{onSend: func(ctx context.Context) {
		<-ctx.Done()
	}, err: context.DeadlineExceeded}
	policy := DefaultEmailDeliveryPolicy()
	policy.SendTimeout = 5 * time.Millisecond
	service, err := NewServiceWithEmailPolicy(repository, email, "0123456789abcdef0123456789abcdef", policy, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	err = service.RequestCode(context.Background(), "person@example.com", PurposePasswordReset, nil, "127.0.0.1")
	if !errors.Is(err, ErrEmailUnavailable) {
		t.Fatalf("expected email unavailable, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("configured timeout was not enforced: %s", elapsed)
	}
	if repository.completion.Succeeded || repository.completion.ErrorClass != "timeout" {
		t.Fatalf("expected safe timeout completion, got %#v", repository.completion)
	}
}

func TestInvitationUsesDedicatedNotificationType(t *testing.T) {
	repository := &fakeRepository{}
	service, err := NewService(repository, &fakeEmail{}, "0123456789abcdef0123456789abcdef", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateInvitation(context.Background(), "person@example.com", "user", "initial access", "owner-1", "owner", "request-1", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if repository.reservation.NotificationType != "invitation_code" || repository.reservation.UserID != nil {
		t.Fatalf("unexpected invitation delivery reservation: %#v", repository.reservation)
	}
}
