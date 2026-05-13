//go:build darwin

package schedule

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

const launchdLabel = "com.ghquery.daily"

type darwinScheduler struct{}

// NewScheduler returns the macOS launchd-based Scheduler.
func NewScheduler() Scheduler {
	return &darwinScheduler{}
}

func plistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist"), nil
}

func (d *darwinScheduler) Install(s Schedule) error {
	if err := s.Validate(); err != nil {
		return err
	}

	plistFile, err := plistPath()
	if err != nil {
		return fmt.Errorf("resolving plist path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(plistFile), 0755); err != nil {
		return fmt.Errorf("creating LaunchAgents dir: %w", err)
	}

	plistBytes, err := renderPlist(s)
	if err != nil {
		return fmt.Errorf("rendering plist: %w", err)
	}

	// If a previous schedule is loaded, unload it first so we can replace cleanly.
	_ = d.unload()

	if err := os.WriteFile(plistFile, plistBytes, 0644); err != nil {
		return fmt.Errorf("writing plist: %w", err)
	}

	if err := d.load(plistFile); err != nil {
		// Roll back: leave the file in place so the user can debug,
		// but surface the launchctl error.
		return fmt.Errorf("loading via launchctl: %w", err)
	}

	return nil
}

func (d *darwinScheduler) Remove() error {
	plistFile, err := plistPath()
	if err != nil {
		return err
	}

	// Unload first (ignore errors if it wasn't loaded)
	_ = d.unload()

	if _, err := os.Stat(plistFile); err == nil {
		if err := os.Remove(plistFile); err != nil {
			return fmt.Errorf("deleting plist: %w", err)
		}
	}
	return nil
}

func (d *darwinScheduler) Status() (Status, error) {
	plistFile, err := plistPath()
	if err != nil {
		return Status{}, err
	}

	plistExists := false
	if _, err := os.Stat(plistFile); err == nil {
		plistExists = true
	}

	loaded := false
	cmd := exec.Command("launchctl", "list", launchdLabel)
	if err := cmd.Run(); err == nil {
		loaded = true
	}

	installed := plistExists && loaded
	var detail string
	switch {
	case installed:
		detail = fmt.Sprintf("launchd label %s, plist at %s", launchdLabel, plistFile)
	case plistExists && !loaded:
		detail = fmt.Sprintf("plist exists at %s but not loaded — run schedule install to fix", plistFile)
	default:
		detail = "no schedule installed"
	}

	return Status{
		Installed: installed,
		Detail:    detail,
	}, nil
}

// load runs `launchctl bootstrap gui/$(id -u) <plist>` to activate the agent.
// Falls back to the older `launchctl load` if bootstrap fails (older macOS).
func (d *darwinScheduler) load(plistFile string) error {
	uid := os.Getuid()
	target := fmt.Sprintf("gui/%d", uid)

	cmd := exec.Command("launchctl", "bootstrap", target, plistFile)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// Fallback for older macOS that doesn't support bootstrap
		fallback := exec.Command("launchctl", "load", "-w", plistFile)
		var fallbackStderr bytes.Buffer
		fallback.Stderr = &fallbackStderr
		if fbErr := fallback.Run(); fbErr != nil {
			return fmt.Errorf("bootstrap failed (%s): %s; fallback load also failed: %s",
				err, truncate(stderr.String(), 240), truncate(fallbackStderr.String(), 240))
		}
	}
	return nil
}

// unload runs `launchctl bootout gui/$(id -u)/<label>`. Errors are non-fatal
// since the agent may not be loaded.
func (d *darwinScheduler) unload() error {
	uid := os.Getuid()
	target := fmt.Sprintf("gui/%d/%s", uid, launchdLabel)

	cmd := exec.Command("launchctl", "bootout", target)
	if err := cmd.Run(); err != nil {
		// Try old-style unload as fallback
		plistFile, perr := plistPath()
		if perr != nil {
			return err
		}
		_ = exec.Command("launchctl", "unload", plistFile).Run()
	}
	return nil
}

// renderPlist generates the launchd property list XML for the given Schedule.
func renderPlist(s Schedule) ([]byte, error) {
	hour, minute, err := s.HourMinute()
	if err != nil {
		return nil, err
	}

	type intervalEntry struct {
		Hour    int
		Minute  int
		Weekday *int // pointer so we can omit when nil
	}

	var intervals []intervalEntry
	switch s.Frequency {
	case FreqDaily:
		intervals = []intervalEntry{{Hour: hour, Minute: minute}}
	case FreqWeekdays:
		// Monday-Friday: launchd weekday 1-5
		for wd := 1; wd <= 5; wd++ {
			day := wd
			intervals = append(intervals, intervalEntry{Hour: hour, Minute: minute, Weekday: &day})
		}
	case FreqWeekly:
		num := WeekdayNum(s.Weekday)
		if num < 0 {
			return nil, fmt.Errorf("invalid weekday: %s", s.Weekday)
		}
		day := num
		intervals = []intervalEntry{{Hour: hour, Minute: minute, Weekday: &day}}
	default:
		return nil, fmt.Errorf("unsupported frequency: %s", s.Frequency)
	}

	stdoutPath := filepath.Join(s.LogDir, "launchd.out")
	stderrPath := filepath.Join(s.LogDir, "launchd.err")

	tmpl := template.Must(template.New("plist").Parse(plistTemplate))

	// Capture the user's current PATH so launchd-spawned processes can find
	// tools like `claude` that live in non-system directories (Homebrew, npm-global, nvm).
	// launchd's default PATH is just "/usr/bin:/bin:/usr/sbin:/sbin" — too minimal.
	userPath := os.Getenv("PATH")
	if userPath == "" {
		userPath = "/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin"
	}

	data := struct {
		Label      string
		Binary     string
		WorkDir    string
		StdoutPath string
		StderrPath string
		PathEnv    string
		Intervals  []intervalEntry
		Single     bool
	}{
		Label:      launchdLabel,
		Binary:     s.Binary,
		WorkDir:    s.WorkDir,
		StdoutPath: stdoutPath,
		StderrPath: stderrPath,
		PathEnv:    userPath,
		Intervals:  intervals,
		Single:     len(intervals) == 1,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>{{.Label}}</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.Binary}}</string>
        <string>run</string>
        <string>--scheduled</string>
    </array>
    <key>WorkingDirectory</key>
    <string>{{.WorkDir}}</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>{{.PathEnv}}</string>
    </dict>
    <key>StandardOutPath</key>
    <string>{{.StdoutPath}}</string>
    <key>StandardErrorPath</key>
    <string>{{.StderrPath}}</string>
{{- if .Single }}
    <key>StartCalendarInterval</key>
    <dict>
        <key>Hour</key><integer>{{(index .Intervals 0).Hour}}</integer>
        <key>Minute</key><integer>{{(index .Intervals 0).Minute}}</integer>
{{- if (index .Intervals 0).Weekday }}
        <key>Weekday</key><integer>{{(index .Intervals 0).Weekday}}</integer>
{{- end }}
    </dict>
{{- else }}
    <key>StartCalendarInterval</key>
    <array>
{{- range .Intervals }}
        <dict>
            <key>Hour</key><integer>{{.Hour}}</integer>
            <key>Minute</key><integer>{{.Minute}}</integer>
{{- if .Weekday }}
            <key>Weekday</key><integer>{{.Weekday}}</integer>
{{- end }}
        </dict>
{{- end }}
    </array>
{{- end }}
</dict>
</plist>
`

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

// next time-related functions are reserved for future Status enrichment;
// returning zero time keeps the API stable for now.
var _ = time.Time{}
