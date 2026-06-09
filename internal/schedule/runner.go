package schedule

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/spf13/viper"

	"github.com/DHaussermann/ghquery/internal/pipeline"
)

// Runner fires due schedule Records in-process, replacing the OS scheduler in
// hosted mode. It ticks on a short interval, and for each enabled record whose
// timezone-adjusted wall clock matches its time + frequency, runs the pipeline
// and delivers to the record's stored webhook URL.
//
// Unlike the OS scheduler (which ignores tz and fires in machine-local time),
// the Runner evaluates each record against its own IANA timezone.
type Runner struct {
	store *Store
	log   io.Writer

	mu       sync.Mutex
	lastFire map[string]string // uuid -> "2006-01-02 15:04" already fired (dedup)
}

// NewRunner builds a Runner over the given store. Run logs are written to log
// (defaults to stderr).
func NewRunner(store *Store, log io.Writer) *Runner {
	if log == nil {
		log = os.Stderr
	}
	return &Runner{store: store, log: log, lastFire: map[string]string{}}
}

// Start runs the tick loop until stop is closed. Intended to run in a goroutine
// started alongside the web server in hosted mode.
func (r *Runner) Start(stop <-chan struct{}) {
	fmt.Fprintf(r.log, "[scheduler] in-process runner started\n")
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			fmt.Fprintf(r.log, "[scheduler] runner stopped\n")
			return
		case <-ticker.C:
			r.tick(time.Now())
		}
	}
}

// tick evaluates all records once. Exported behavior is driven by now so it is
// straightforward to reason about (and test) without waiting on the clock.
func (r *Runner) tick(now time.Time) {
	records, err := r.store.List()
	if err != nil {
		fmt.Fprintf(r.log, "[scheduler] list failed: %v\n", err)
		return
	}
	for i := range records {
		rec := records[i]
		if !rec.Enabled {
			continue
		}
		due, stamp := r.isDue(rec, now)
		if !due {
			continue
		}
		// De-dup: a given minute can tick more than once.
		r.mu.Lock()
		already := r.lastFire[rec.UUID] == stamp
		if !already {
			r.lastFire[rec.UUID] = stamp
		}
		r.mu.Unlock()
		if already {
			continue
		}
		go r.fire(rec)
	}
}

// isDue reports whether rec should fire at now, and returns a minute-precise
// stamp (in the record's tz) used to de-duplicate firings within the same minute.
func (r *Runner) isDue(rec Record, now time.Time) (bool, string) {
	loc := time.Local
	if rec.TZ != "" {
		if l, err := time.LoadLocation(rec.TZ); err == nil {
			loc = l
		}
	}
	n := now.In(loc)
	stamp := n.Format("2006-01-02 15:04")

	if rec.Time != n.Format("15:04") {
		return false, stamp
	}

	switch Frequency(rec.Frequency) {
	case FreqDaily:
		return true, stamp
	case FreqWeekdays:
		wd := n.Weekday()
		return wd >= time.Monday && wd <= time.Friday, stamp
	case FreqWeekly:
		return int(n.Weekday()) == WeekdayNum(rec.Weekday), stamp
	default:
		// Unknown/unset frequency but the time matched — treat as daily.
		return true, stamp
	}
}

// fire runs the pipeline for one record and delivers to its webhook. Secrets
// and the team map are read from viper at fire time (env vars in hosted mode).
func (r *Runner) fire(rec Record) {
	fmt.Fprintf(r.log, "[scheduler] firing schedule %s (%s %s %s)\n", rec.UUID, rec.Frequency, rec.Time, rec.TZ)

	mode := rec.Mode
	if mode == "" {
		mode = "all"
	}
	params := pipeline.RunParams{
		Repos:         rec.Repos,
		Authors:       rec.Authors,
		Days:          rec.Days,
		Mode:          mode,
		WebhookURL:    rec.WebhookURL,
		SkipAnalysis:  rec.SkipAnalysis,
		UseCodeRabbit: rec.UseCodeRabbit,
		ClaudePath:    viper.GetString("claude_path"),
	}

	if _, err := pipeline.Execute(
		context.Background(),
		params,
		r.log,
		viper.GetString("github_token"),
		viper.GetStringMap("catalog.teams"),
	); err != nil {
		fmt.Fprintf(r.log, "[scheduler] schedule %s failed: %v\n", rec.UUID, err)
		return
	}
	fmt.Fprintf(r.log, "[scheduler] schedule %s completed\n", rec.UUID)
}
