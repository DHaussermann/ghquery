package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/DHaussermann/ghquery/internal/output"
	"github.com/DHaussermann/ghquery/internal/pipeline"
)

var runOutputFile string
var runWebhookURL string
var runDryRun bool
var runMode string
var runScheduled bool

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Fetch, analyze, and display results (all-in-one)",
	Long:  "Fetches PRs, runs risk analysis, and displays the results table.\nRun without flags for interactive mode.",
	RunE:  runAll,
}

func init() {
	runCmd.Flags().StringVarP(&runOutputFile, "save-report", "s", "", "save risk report JSON to file")
	runCmd.Flags().StringVar(&runWebhookURL, "webhook-url", "", "Incoming webhook URL (overrides config.yaml)")
	runCmd.Flags().BoolVar(&runDryRun, "dry-run", false, "print webhook payload without sending")
	runCmd.Flags().StringVar(&runMode, "mode", "", "query mode: all or untested")
	runCmd.Flags().BoolVar(&runScheduled, "scheduled", false, "non-interactive mode for OS scheduler (logs to file, reads everything from config.yaml)")
	viper.BindPFlag("webhook_url", runCmd.Flags().Lookup("webhook-url"))
	rootCmd.AddCommand(runCmd)
}

func runAll(cmd *cobra.Command, args []string) error {
	// Prefer the saved query block (query.*) — falls back to top-level for legacy
	repos, authors, days, savedMode := resolveQueryDefaults()
	webhookURL := viper.GetString("webhook_url")
	if runMode == "" && savedMode != "" {
		runMode = savedMode
	}

	// Scheduled mode: skip prompt, read everything from config, log to file
	var logWriter io.Writer = os.Stderr
	if runScheduled {
		fw, err := openScheduledLogFile()
		if err != nil {
			return fmt.Errorf("opening log file: %w", err)
		}
		defer fw.Close()
		// Tee to stderr so launchd/Task Scheduler also captures it
		logWriter = io.MultiWriter(fw, os.Stderr)

		fmt.Fprintf(logWriter, "\n=== ghquery scheduled run started at %s ===\n", time.Now().Format(time.RFC3339))

		if runMode == "" {
			runMode = "all"
		}
	} else {
		// Interactive prompt unless authors supplied via flags
		authorsFlag := cmd.Flags().Lookup("authors")
		if authorsFlag == nil || !authorsFlag.Changed {
			var err error
			repos, authors, days, runMode, err = interactivePrompt(repos, authors, days, runMode)
			if err != nil {
				return err
			}
		}
		if runMode == "" {
			runMode = "all"
		}
	}

	// Build params and execute pipeline. For scheduled runs, honor the saved
	// query.skip_analysis preference so users can configure "list only" daily runs.
	skipAnalysis := false
	if runScheduled {
		skipAnalysis = viper.GetBool("query.skip_analysis")
	}
	params := pipeline.RunParams{
		Repos:        repos,
		Authors:      authors,
		Days:         days,
		Mode:         runMode,
		WebhookURL:   webhookURL,
		DryRun:       runDryRun,
		SaveReport:   runOutputFile,
		SkipAnalysis: skipAnalysis,
		ClaudePath:   viper.GetString("claude_path"),
	}

	report, err := pipeline.Execute(
		context.Background(),
		params,
		logWriter,
		viper.GetString("github_token"),
		viper.GetStringMap("catalog.teams"),
	)
	if err != nil {
		return err
	}

	if report != nil && !runScheduled {
		// Skip terminal table in scheduled mode (no console)
		output.PrintTable(report)
	}

	if runScheduled {
		fmt.Fprintf(logWriter, "=== ghquery scheduled run finished at %s ===\n", time.Now().Format(time.RFC3339))
	}

	return nil
}

// openScheduledLogFile opens an append-mode log file at ~/.ghquery/logs/YYYY-MM-DD.log
// for use by --scheduled runs.
func openScheduledLogFile() (*os.File, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	logDir := filepath.Join(home, ".ghquery", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, err
	}
	filename := time.Now().Format("2006-01-02") + ".log"
	return os.OpenFile(filepath.Join(logDir, filename), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
}

func interactivePrompt(defaultRepos []string, defaultAuthors []string, defaultDays int, defaultMode string) (repos []string, authors []string, days int, mode string, err error) {
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println("=== GitHub PR Risk Query Tool ===")
	fmt.Println()

	// Query mode
	if defaultMode == "" {
		defaultMode = "all"
	}
	for {
		fmt.Printf("Query mode — all or untested [%s]: ", defaultMode)
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(strings.ToLower(scanner.Text()))
		if input == "" {
			mode = defaultMode
			break
		}
		if input == "all" || input == "untested" {
			mode = input
			break
		}
		fmt.Printf("[warning] %q is not a valid mode — type 'all' or 'untested'\n", input)
	}
	fmt.Println()

	// Repos
	defaultReposStr := strings.Join(defaultRepos, ", ")
	fmt.Printf("Repos (comma-separated) [%s]: ", defaultReposStr)
	if scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if input != "" {
			repos = splitAndTrim(input)
		} else {
			repos = defaultRepos
		}
	}

	// Show available teams
	teams := viper.GetStringMap("catalog.teams")
	if len(teams) > 0 {
		var teamNames []string
		for name := range teams {
			teamNames = append(teamNames, name)
		}
		fmt.Printf("Available teams: %s\n", strings.Join(teamNames, ", "))
	}

	// Authors
	if len(defaultAuthors) > 0 {
		defaultAuthorsStr := strings.Join(defaultAuthors, ", ")
		fmt.Printf("Authors or teams (comma-separated) [%s]: ", defaultAuthorsStr)
	} else {
		fmt.Print("Authors or teams (comma-separated): ")
	}
	if scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if input != "" {
			authors = splitAndTrim(input)
		} else if len(defaultAuthors) > 0 {
			authors = defaultAuthors
		} else {
			return nil, nil, 0, "", fmt.Errorf("authors is required")
		}
	}

	// Days
	if defaultDays <= 0 {
		defaultDays = 7
	}
	fmt.Printf("Days to look back [%d]: ", defaultDays)
	if scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if input != "" {
			days, err = strconv.Atoi(input)
			if err != nil || days <= 0 {
				return nil, nil, 0, "", fmt.Errorf("days must be a positive number (got %q)", input)
			}
		} else {
			days = defaultDays
		}
	}
	if days <= 0 {
		days = 7
	}

	// Webhook check
	webhookURL := viper.GetString("webhook_url")
	if webhookURL != "" {
		if err := output.CheckWebhook(webhookURL); err != nil {
			fmt.Printf("\n[warning] Webhook unreachable: %v\n", err)
			fmt.Println("[warning] Results will only be available in console output unless you save to a file")
		} else {
			fmt.Println("\n[webhook] Reachable")
		}
	} else {
		fmt.Println("\n[warning] No webhook URL configured")
	}

	// Save report
	fmt.Print("Save report to file (leave blank to skip): ")
	if scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if input != "" {
			runOutputFile = input
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, 0, "", fmt.Errorf("reading input: %w", err)
	}

	return repos, authors, days, mode, nil
}

// resolveQueryDefaults returns the saved query (repos, authors, days, mode)
// from the config.yaml `query:` block. This is the source of truth for what
// scheduled runs and CLI runs should query.
func resolveQueryDefaults() (repos, authors []string, days int, mode string) {
	repos = viper.GetStringSlice("query.repos")
	authors = viper.GetStringSlice("query.authors")
	days = viper.GetInt("query.days")
	mode = viper.GetString("query.mode")
	return
}

func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
