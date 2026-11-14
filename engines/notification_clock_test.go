package engines

import (
	"testing"
	"time"
)

// 49.12.5: the clock boundary. CERT-In's 6 hours from detection: on time until
// the last quarter (4h30), due from then up to and including 6h exactly,
// overdue the moment it is passed; a closed decision or no deadline shows none.
func TestNotifyClockStateBoundaries(t *testing.T) {
	found := time.Date(2026, 11, 12, 8, 0, 0, 0, time.UTC)
	due := found.Add(6 * time.Hour)
	cases := []struct {
		name   string
		now    time.Time
		closed bool
		want   string
	}{
		{"just noticed", found, false, NotifyClockOnTime},
		{"4h29 in", found.Add(4*time.Hour + 29*time.Minute), false, NotifyClockOnTime},
		{"4h30 in: last quarter starts", found.Add(4*time.Hour + 30*time.Minute), false, NotifyClockDue},
		{"5h59 in", found.Add(5*time.Hour + 59*time.Minute), false, NotifyClockDue},
		{"exactly at the deadline", due, false, NotifyClockDue},
		{"one second past", due.Add(time.Second), false, NotifyClockOverdue},
		{"days later", due.Add(72 * time.Hour), false, NotifyClockOverdue},
		{"closed and past", due.Add(time.Hour), true, NotifyClockNone},
	}
	for _, c := range cases {
		if got := NotifyClockState(found, &due, c.now, c.closed); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	if got := NotifyClockState(found, nil, due.Add(time.Hour), false); got != NotifyClockNone {
		t.Errorf("no deadline set must show none, got %q", got)
	}
	// DPDP's 72 hours: the same rule scaled to the row's own window.
	d72 := found.Add(72 * time.Hour)
	if got := NotifyClockState(found, &d72, found.Add(53*time.Hour), false); got != NotifyClockOnTime {
		t.Errorf("72h row at 53h: %q", got)
	}
	if got := NotifyClockState(found, &d72, found.Add(54*time.Hour), false); got != NotifyClockDue {
		t.Errorf("72h row at 54h: %q", got)
	}
}
