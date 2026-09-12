package mediauploadcontrol

import (
	"testing"
	"time"
)

func TestTimedCommandStrictWindow(t *testing.T) {
	c := testCommand()
	c.IssuedAt = "2026-09-11T00:00:00Z"
	c.ExpiresAt = "2026-09-11T00:05:00Z"
	if !c.valid() {
		t.Fatal("valid five-minute window refused")
	}
	for _, tc := range []struct{ issued, expires string }{
		{"", c.ExpiresAt}, {c.IssuedAt, ""}, {c.IssuedAt, "2026-09-11T00:05:01Z"}, {c.IssuedAt, c.IssuedAt},
		{c.IssuedAt, "2026-09-10T23:59:59Z"}, {"2026-09-11T00:00:00.1Z", c.ExpiresAt},
		{"2026-09-11T00:00:00+00:00", c.ExpiresAt}, {"2026-02-30T00:00:00Z", c.ExpiresAt},
	} {
		v := c
		v.IssuedAt = tc.issued
		v.ExpiresAt = tc.expires
		if v.valid() {
			t.Fatalf("invalid window accepted %+v", tc)
		}
	}
	r := ackFor(c)
	r.Status = "not_applied"
	if r.valid(time.Now()) {
		t.Fatal("unknown receipt status accepted")
	}
}
