//go:build windows

package schedule

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"os/exec"
	"strings"
)

const taskName = "GhQuery"

type windowsScheduler struct{}

// NewScheduler returns the Windows Task Scheduler-based Scheduler.
func NewScheduler() Scheduler {
	return &windowsScheduler{}
}

func (w *windowsScheduler) Install(s Schedule) error {
	if err := s.Validate(); err != nil {
		return err
	}

	// Remove any existing task with the same name (idempotent install)
	_ = w.Remove()

	// Build the task RUN command. /TR must be a single string the OS executes;
	// we wrap with cmd /C and cd to the project dir so config.yaml is found.
	tr := fmt.Sprintf(`cmd /C "cd /D \"%s\" && \"%s\" run --scheduled"`, s.WorkDir, s.Binary)

	args := []string{
		"/Create",
		"/TN", taskName,
		"/TR", tr,
		"/ST", s.Time,
		"/F", // overwrite if exists
	}

	switch s.Frequency {
	case FreqDaily:
		args = append(args, "/SC", "DAILY")
	case FreqWeekdays:
		args = append(args, "/SC", "WEEKLY", "/D", "MON,TUE,WED,THU,FRI")
	case FreqWeekly:
		short := WeekdayShortUpper(s.Weekday)
		if short == "" {
			return fmt.Errorf("invalid weekday: %s", s.Weekday)
		}
		args = append(args, "/SC", "WEEKLY", "/D", short)
	default:
		return fmt.Errorf("unsupported frequency: %s", s.Frequency)
	}

	cmd := exec.Command("schtasks", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("schtasks /Create failed: %s: %s", err, truncateWin(stderr.String(), 240))
	}

	return nil
}

func (w *windowsScheduler) Remove() error {
	cmd := exec.Command("schtasks", "/Delete", "/TN", taskName, "/F")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// Task may not exist — only error if stderr suggests something else
		s := strings.ToLower(stderr.String())
		if strings.Contains(s, "cannot find") || strings.Contains(s, "does not exist") {
			return nil
		}
		return fmt.Errorf("schtasks /Delete failed: %s: %s", err, truncateWin(stderr.String(), 240))
	}
	return nil
}

func (w *windowsScheduler) Status() (Status, error) {
	cmd := exec.Command("schtasks", "/Query", "/TN", taskName, "/FO", "CSV", "/NH")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Status{Installed: false, Detail: "task not found"}, nil
	}

	r := csv.NewReader(&stdout)
	rec, err := r.Read()
	if err != nil || len(rec) < 3 {
		return Status{Installed: true, Detail: "task exists (parse error)"}, nil
	}
	return Status{
		Installed: true,
		Detail:    fmt.Sprintf("Task Scheduler: %s", strings.Join(rec, " | ")),
	}, nil
}

func truncateWin(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
