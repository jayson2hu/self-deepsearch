package account

import "testing"

func TestValidFeedbackReviewTransition(t *testing.T) {
	tests := []struct {
		current string
		target  string
		valid   bool
	}{
		{current: "pending", target: "reviewing", valid: true},
		{current: "pending", target: "accepted", valid: true},
		{current: "pending", target: "rejected", valid: true},
		{current: "pending", target: "closed", valid: true},
		{current: "reviewing", target: "accepted", valid: true},
		{current: "reviewing", target: "rejected", valid: true},
		{current: "reviewing", target: "closed", valid: true},
		{current: "reviewing", target: "reviewing", valid: false},
		{current: "accepted", target: "reviewing", valid: false},
		{current: "rejected", target: "accepted", valid: false},
		{current: "closed", target: "pending", valid: false},
		{current: "pending", target: "unknown", valid: false},
	}
	for _, test := range tests {
		t.Run(test.current+"_to_"+test.target, func(t *testing.T) {
			if actual := ValidFeedbackReviewTransition(test.current, test.target); actual != test.valid {
				t.Fatalf("expected %v, got %v", test.valid, actual)
			}
		})
	}
}
