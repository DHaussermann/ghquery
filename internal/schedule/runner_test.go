package schedule

import (
	"testing"
	"time"
)

func TestIsDue(t *testing.T) {
	r := NewRunner(nil, nil)
	ny, _ := time.LoadLocation("America/New_York")

	// 2026-05-25 is a Monday.
	monday0830NY := time.Date(2026, 5, 25, 8, 30, 0, 0, ny)
	saturday0830NY := time.Date(2026, 5, 23, 8, 30, 0, 0, ny)

	cases := []struct {
		name string
		rec  Record
		now  time.Time
		want bool
	}{
		{"daily match", Record{Time: "08:30", Frequency: "daily", TZ: "America/New_York"}, monday0830NY, true},
		{"daily wrong minute", Record{Time: "08:31", Frequency: "daily", TZ: "America/New_York"}, monday0830NY, false},
		{"weekdays on monday", Record{Time: "08:30", Frequency: "weekdays", TZ: "America/New_York"}, monday0830NY, true},
		{"weekdays on saturday", Record{Time: "08:30", Frequency: "weekdays", TZ: "America/New_York"}, saturday0830NY, false},
		{"weekly matching day", Record{Time: "08:30", Frequency: "weekly", Weekday: "monday", TZ: "America/New_York"}, monday0830NY, true},
		{"weekly wrong day", Record{Time: "08:30", Frequency: "weekly", Weekday: "friday", TZ: "America/New_York"}, monday0830NY, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _ := r.isDue(c.rec, c.now)
			if got != c.want {
				t.Fatalf("isDue = %v, want %v", got, c.want)
			}
		})
	}
}

func TestIsDueTimezoneShift(t *testing.T) {
	r := NewRunner(nil, nil)
	// A record set for 08:30 in New York should match when the absolute instant
	// is 08:30 NY, regardless of the server's own timezone.
	ny, _ := time.LoadLocation("America/New_York")
	la, _ := time.LoadLocation("America/Los_Angeles")

	rec := Record{Time: "08:30", Frequency: "daily", TZ: "America/New_York"}

	instant := time.Date(2026, 5, 25, 8, 30, 0, 0, ny)
	// Express the same instant in LA — isDue must still evaluate it as 08:30 NY.
	if due, _ := r.isDue(rec, instant.In(la)); !due {
		t.Fatal("expected due when instant is 08:30 in the record's tz, viewed from LA")
	}
	if due, _ := r.isDue(rec, instant.Add(time.Hour)); due {
		t.Fatal("expected not due an hour later")
	}
}
