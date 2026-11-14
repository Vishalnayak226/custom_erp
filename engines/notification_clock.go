package engines

import "time"

// 49.12.5: the legal clocks of a notification decision, checked against the
// server's clock. The matrix gives the hours (CERT-In 6 hours to tell, for
// example); OpenNotifyIncident turns them into deadlines from the detection
// time; this says where a deadline stands now. The system reports the clock,
// it does not decide anything legal.
const (
	NotifyClockNone    = ""        // no deadline set, or the decision is closed
	NotifyClockOnTime  = "on_time" // more than a quarter of the window left
	NotifyClockDue     = "due"     // the last quarter of the window, up to and including the deadline
	NotifyClockOverdue = "overdue" // past the deadline
)

// NotifyClockState is where a deadline stands at now, for an open decision.
// The window runs from discovered to due; the last quarter of it is "due".
// A deadline exactly at now is still "due": it is overdue only once passed.
func NotifyClockState(discovered time.Time, due *time.Time, now time.Time, closed bool) string {
	if due == nil || closed {
		return NotifyClockNone
	}
	if now.After(*due) {
		return NotifyClockOverdue
	}
	window := due.Sub(discovered)
	if window <= 0 || due.Sub(now) <= window/4 {
		return NotifyClockDue
	}
	return NotifyClockOnTime
}
