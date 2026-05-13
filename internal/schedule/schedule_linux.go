//go:build linux

package schedule

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	cronBeginMarker = "# BEGIN GHQUERY SCHEDULE"
	cronEndMarker   = "# END GHQUERY SCHEDULE"
)

type linuxScheduler struct{}

// NewScheduler returns the Linux crontab-based Scheduler.
func NewScheduler() Scheduler {
	return &linuxScheduler{}
}

func (l *linuxScheduler) Install(s Schedule) error {
	if err := s.Validate(); err != nil {
		return err
	}

	hour, minute, err := s.HourMinute()
	if err != nil {
		return err
	}

	dow := "*"
	switch s.Frequency {
	case FreqDaily:
		dow = "*"
	case FreqWeekdays:
		dow = "1-5"
	case FreqWeekly:
		num := WeekdayNum(s.Weekday)
		if num < 0 {
			return fmt.Errorf("invalid weekday: %s", s.Weekday)
		}
		dow = fmt.Sprintf("%d", num)
	}

	logPath := fmt.Sprintf("%s/cron.log", s.LogDir)
	line := fmt.Sprintf("%d %d * * %s cd %q && %q run --scheduled >> %q 2>&1",
		minute, hour, dow, s.WorkDir, s.Binary, logPath)

	existing := readCrontab()
	updated := replaceManagedBlock(existing, line)
	return writeCrontab(updated)
}

func (l *linuxScheduler) Remove() error {
	existing := readCrontab()
	updated := removeManagedBlock(existing)
	if updated == existing {
		return nil
	}
	return writeCrontab(updated)
}

func (l *linuxScheduler) Status() (Status, error) {
	existing := readCrontab()
	if strings.Contains(existing, cronBeginMarker) {
		// Extract the managed block to show as detail
		start := strings.Index(existing, cronBeginMarker)
		end := strings.Index(existing, cronEndMarker)
		detail := "crontab entry present"
		if start >= 0 && end > start {
			detail = strings.TrimSpace(existing[start : end+len(cronEndMarker)])
		}
		return Status{Installed: true, Detail: detail}, nil
	}
	return Status{Installed: false, Detail: "no managed crontab block"}, nil
}

func readCrontab() string {
	cmd := exec.Command("crontab", "-l")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	_ = cmd.Run() // ignore error: empty crontab returns non-zero
	return stdout.String()
}

func writeCrontab(content string) error {
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(content)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("crontab -: %s: %s", err, truncateLin(stderr.String(), 240))
	}
	return nil
}

func replaceManagedBlock(existing, line string) string {
	stripped := removeManagedBlock(existing)
	if !strings.HasSuffix(stripped, "\n") && stripped != "" {
		stripped += "\n"
	}

	// Capture user's current PATH so cron-spawned processes can find tools
	// like `claude` that live in non-system directories.
	// cron's default PATH is minimal; without this, exec.LookPath fails for
	// anything outside /usr/bin and /bin.
	userPath := os.Getenv("PATH")
	pathLine := ""
	if userPath != "" {
		pathLine = "PATH=" + userPath + "\n"
	}

	return stripped + cronBeginMarker + "\n" + pathLine + line + "\n" + cronEndMarker + "\n"
}

func removeManagedBlock(existing string) string {
	start := strings.Index(existing, cronBeginMarker)
	end := strings.Index(existing, cronEndMarker)
	if start < 0 || end < 0 || end < start {
		return existing
	}
	endLine := end + len(cronEndMarker)
	if endLine < len(existing) && existing[endLine] == '\n' {
		endLine++
	}
	return existing[:start] + existing[endLine:]
}

func truncateLin(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
