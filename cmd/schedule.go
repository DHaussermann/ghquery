package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/DHaussermann/ghquery/internal/schedule"
)

var scheduleCmd = &cobra.Command{
	Use:   "schedule",
	Short: "Manage the scheduled daily run",
	Long:  "Install, remove, or check the status of the OS-level scheduled daily run.\nThe UI provides the same controls — this CLI is a fallback for power users.",
}

var scheduleInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the daily schedule from config.yaml",
	RunE:  runScheduleInstall,
}

var scheduleRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove the daily schedule",
	RunE:  runScheduleRemove,
}

var scheduleStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show whether scheduling is currently active",
	RunE:  runScheduleStatus,
}

func init() {
	scheduleCmd.AddCommand(scheduleInstallCmd)
	scheduleCmd.AddCommand(scheduleRemoveCmd)
	scheduleCmd.AddCommand(scheduleStatusCmd)
	rootCmd.AddCommand(scheduleCmd)
}

func runScheduleInstall(cmd *cobra.Command, args []string) error {
	s, err := buildScheduleFromConfig()
	if err != nil {
		return err
	}

	scheduler := schedule.NewScheduler()
	if err := scheduler.Install(s); err != nil {
		return fmt.Errorf("install failed: %w", err)
	}

	fmt.Printf("[schedule] Installed: %s at %s", s.Frequency, s.Time)
	if s.Frequency == schedule.FreqWeekly {
		fmt.Printf(" on %s", s.Weekday)
	}
	fmt.Println()
	return nil
}

func runScheduleRemove(cmd *cobra.Command, args []string) error {
	scheduler := schedule.NewScheduler()
	if err := scheduler.Remove(); err != nil {
		return fmt.Errorf("remove failed: %w", err)
	}
	fmt.Println("[schedule] Removed")
	return nil
}

func runScheduleStatus(cmd *cobra.Command, args []string) error {
	scheduler := schedule.NewScheduler()
	status, err := scheduler.Status()
	if err != nil {
		return fmt.Errorf("status check failed: %w", err)
	}

	if status.Installed {
		fmt.Printf("[schedule] Active — %s\n", status.Detail)
	} else {
		fmt.Printf("[schedule] Inactive — %s\n", status.Detail)
	}
	return nil
}

// buildScheduleFromConfig reads the schedule block from config.yaml and resolves
// the binary + working directory paths needed by the OS scheduler.
func buildScheduleFromConfig() (schedule.Schedule, error) {
	t := viper.GetString("schedule.time")
	freq := schedule.Frequency(viper.GetString("schedule.frequency"))
	weekday := viper.GetString("schedule.weekday")

	if t == "" {
		return schedule.Schedule{}, fmt.Errorf("schedule.time not set in config.yaml")
	}
	if freq == "" {
		freq = schedule.FreqDaily
	}

	binary, err := os.Executable()
	if err != nil {
		return schedule.Schedule{}, fmt.Errorf("resolving binary path: %w", err)
	}
	binary, _ = filepath.Abs(binary)

	workDir, err := os.Getwd()
	if err != nil {
		return schedule.Schedule{}, fmt.Errorf("resolving working directory: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return schedule.Schedule{}, fmt.Errorf("resolving home dir: %w", err)
	}
	logDir := filepath.Join(home, ".ghquery", "logs")

	if err := os.MkdirAll(logDir, 0755); err != nil {
		return schedule.Schedule{}, fmt.Errorf("creating log dir: %w", err)
	}

	return schedule.Schedule{
		Time:      t,
		Frequency: freq,
		Weekday:   weekday,
		Binary:    binary,
		WorkDir:   workDir,
		LogDir:    logDir,
	}, nil
}
