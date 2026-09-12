package operations

import (
	"errors"
	"testing"
)

func TestCanManageUserStatus(t *testing.T) {
	tests := []struct {
		name       string
		actorRole  string
		targetRole string
		sameUser   bool
		allowed    bool
	}{
		{name: "admin ordinary user", actorRole: "admin", targetRole: "user", allowed: true},
		{name: "admin editor", actorRole: "admin", targetRole: "editor", allowed: false},
		{name: "admin administrator", actorRole: "admin", targetRole: "admin", allowed: false},
		{name: "owner administrator", actorRole: "owner", targetRole: "admin", allowed: true},
		{name: "owner editor", actorRole: "owner", targetRole: "editor", allowed: true},
		{name: "owner ordinary user", actorRole: "owner", targetRole: "user", allowed: true},
		{name: "owner owner", actorRole: "owner", targetRole: "owner", allowed: false},
		{name: "self", actorRole: "owner", targetRole: "admin", sameUser: true, allowed: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := CanManageUserStatus(test.actorRole, test.targetRole, test.sameUser); actual != test.allowed {
				t.Fatalf("expected %v, got %v", test.allowed, actual)
			}
		})
	}
}

func TestCanManageUserRole(t *testing.T) {
	tests := []struct {
		name       string
		actorRole  string
		targetRole string
		sameUser   bool
		allowed    bool
	}{
		{name: "owner manages admin", actorRole: "owner", targetRole: "admin", allowed: true},
		{name: "owner manages editor", actorRole: "owner", targetRole: "editor", allowed: true},
		{name: "owner manages ordinary user", actorRole: "owner", targetRole: "user", allowed: true},
		{name: "owner target is immutable", actorRole: "owner", targetRole: "owner", allowed: false},
		{name: "admin cannot change roles", actorRole: "admin", targetRole: "user", allowed: false},
		{name: "self", actorRole: "owner", targetRole: "admin", sameUser: true, allowed: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := CanManageUserRole(test.actorRole, test.targetRole, test.sameUser); actual != test.allowed {
				t.Fatalf("CanManageUserRole(%q, %q, %t) = %t, want %t", test.actorRole, test.targetRole, test.sameUser, actual, test.allowed)
			}
		})
	}
}

func TestReviewDecisionAccess(t *testing.T) {
	tests := []struct {
		name       string
		status     string
		assigneeID string
		actorID    string
		want       error
	}{
		{name: "assignee may decide", status: "claimed", assigneeID: "reviewer-1", actorID: "reviewer-1"},
		{name: "pending task conflicts", status: "pending", actorID: "reviewer-1", want: ErrConflict},
		{name: "claimed task without assignee conflicts", status: "claimed", actorID: "reviewer-1", want: ErrConflict},
		{name: "different operator is forbidden", status: "claimed", assigneeID: "reviewer-1", actorID: "reviewer-2", want: ErrForbidden},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ReviewDecisionAccess(test.status, test.assigneeID, test.actorID); !errors.Is(err, test.want) {
				t.Fatalf("ReviewDecisionAccess(%q, %q, %q) = %v, want %v", test.status, test.assigneeID, test.actorID, err, test.want)
			}
		})
	}
}

func TestCanReceiveReviewTask(t *testing.T) {
	for _, test := range []struct {
		role, status string
		want         bool
	}{
		{role: "owner", status: "active", want: true},
		{role: "admin", status: "active", want: true},
		{role: "editor", status: "active", want: false},
		{role: "user", status: "active", want: false},
		{role: "admin", status: "suspended", want: false},
		{role: "owner", status: "locked", want: false},
	} {
		if got := CanReceiveReviewTask(test.role, test.status); got != test.want {
			t.Fatalf("CanReceiveReviewTask(%q, %q) = %v, want %v", test.role, test.status, got, test.want)
		}
	}
}
