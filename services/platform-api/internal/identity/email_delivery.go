package identity

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"
)

var (
	ErrEmailDailyLimit  = errors.New("email daily limit reached")
	ErrEmailCircuitOpen = errors.New("email delivery circuit open")
)

const emailDeliveryAuditTimeout = 3 * time.Second

type EmailDeliveryPolicy struct {
	DailyLimit       int
	FailureThreshold int
	CircuitCooldown  time.Duration
	SendTimeout      time.Duration
}

func DefaultEmailDeliveryPolicy() EmailDeliveryPolicy {
	return EmailDeliveryPolicy{
		DailyLimit:       500,
		FailureThreshold: 5,
		CircuitCooldown:  15 * time.Minute,
		SendTimeout:      10 * time.Second,
	}
}

func (policy EmailDeliveryPolicy) validate() error {
	if policy.DailyLimit < 1 {
		return errors.New("email daily limit must be positive")
	}
	if policy.FailureThreshold < 1 {
		return errors.New("email failure threshold must be positive")
	}
	if policy.CircuitCooldown <= 0 {
		return errors.New("email circuit cooldown must be positive")
	}
	if policy.SendTimeout <= 0 {
		return errors.New("email send timeout must be positive")
	}
	return nil
}

type EmailDeliveryReservation struct {
	NotificationType string
	UserID           *string
	ReservedAt       time.Time
	DailyLimit       int
}

type EmailDeliveryTicket struct {
	ID         string
	BucketDate string
}

type EmailDeliveryCompletion struct {
	Ticket           EmailDeliveryTicket
	Succeeded        bool
	ErrorClass       string
	CompletedAt      time.Time
	FailureThreshold int
	CircuitCooldown  time.Duration
}

type EmailDeliveryCount struct {
	Purpose string
	Outcome string
	Count   int64
}

type EmailDeliveryMetrics struct {
	DailyReserved      int64
	DailySent          int64
	DailyFailed        int64
	ConsecutiveFailure int64
	CircuitOpen        bool
	Counts             []EmailDeliveryCount
}

type EmailDeliveryGuard interface {
	ReserveEmailDelivery(context.Context, EmailDeliveryReservation) (EmailDeliveryTicket, error)
	CompleteEmailDelivery(context.Context, EmailDeliveryCompletion) error
}

func (service *Service) deliverCode(ctx context.Context, recipient, purpose, code string, userID *string) error {
	notificationType, err := notificationTypeForPurpose(purpose)
	if err != nil {
		return err
	}
	now := service.now().UTC()
	reservationContext, cancelReservation := context.WithTimeout(ctx, service.emailPolicy.SendTimeout)
	ticket, err := service.repository.ReserveEmailDelivery(reservationContext, EmailDeliveryReservation{
		NotificationType: notificationType,
		UserID:           userID,
		ReservedAt:       now,
		DailyLimit:       service.emailPolicy.DailyLimit,
	})
	cancelReservation()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrEmailUnavailable, err)
	}

	sendContext, cancelSend := context.WithTimeout(ctx, service.emailPolicy.SendTimeout)
	sendErr := service.email.SendCode(sendContext, recipient, purpose, code)
	cancelSend()

	completion := EmailDeliveryCompletion{
		Ticket:           ticket,
		Succeeded:        sendErr == nil,
		CompletedAt:      service.now().UTC(),
		FailureThreshold: service.emailPolicy.FailureThreshold,
		CircuitCooldown:  service.emailPolicy.CircuitCooldown,
	}
	if sendErr != nil {
		completion.ErrorClass = emailDeliveryErrorClass(sendErr)
	}
	completionContext, cancelCompletion := context.WithTimeout(context.WithoutCancel(ctx), emailDeliveryAuditTimeout)
	completionErr := service.repository.CompleteEmailDelivery(completionContext, completion)
	cancelCompletion()
	if sendErr != nil {
		if completionErr != nil {
			return fmt.Errorf("%w: provider delivery and audit completion failed", ErrEmailUnavailable)
		}
		return fmt.Errorf("%w: %w", ErrEmailUnavailable, sendErr)
	}
	if completionErr != nil {
		return fmt.Errorf("%w: record successful delivery: %w", ErrEmailUnavailable, completionErr)
	}
	return nil
}

func notificationTypeForPurpose(purpose string) (string, error) {
	switch purpose {
	case PurposeSignup:
		return "signup_code", nil
	case PurposePasswordReset:
		return "password_code", nil
	case PurposeAccountClose:
		return "account_close_code", nil
	case PurposeInvitation:
		return "invitation_code", nil
	default:
		return "", errors.New("unsupported email purpose")
	}
}

func emailDeliveryErrorClass(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return "timeout"
	}
	return "provider"
}
