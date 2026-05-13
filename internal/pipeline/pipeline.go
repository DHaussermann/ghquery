package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DHaussermann/ghquery/internal/analysis"
	gh "github.com/DHaussermann/ghquery/internal/github"
	"github.com/DHaussermann/ghquery/internal/output"
)

type RunParams struct {
	Repos         []string
	Authors       []string
	Days          int
	Mode          string // "all" or "untested"
	WebhookURL    string
	DryRun        bool
	SaveReport    string
	SkipAnalysis  bool
	SkipWebhook   bool   // when true, never send to webhook even if WebhookURL is set (UI preview mode)
	PRUrl         string // single-PR mode: when set, fetch only this PR and bypass team/author/date/mode filters
	UseCodeRabbit bool   // when true, attempt to read CodeRabbit's "Change Impact" comment on each PR; PRs that have one skip the agent entirely
}

// Execute runs the full pipeline: fetch → filter → analyze → webhook.
// Returns the risk report for the caller to display however they want (CLI table, HTML, etc).
//
// When params.PRUrl is set, the pipeline runs in single-PR mode: it fetches
// only that one PR (bypassing team/author/date filters and untested-mode
// filtering) and runs the rest of the pipeline against it.
func Execute(ctx context.Context, params RunParams, log io.Writer, token string, teams map[string]interface{}) (*analysis.RiskReport, error) {
	// Phase 1: Fetch PRs — branches on single-PR mode vs. query mode.
	client := gh.NewClient(token, log)

	var result *gh.FetchResult

	if params.PRUrl != "" {
		fmt.Fprintf(log, "[config] Single-PR mode: %s\n", params.PRUrl)
		if params.UseCodeRabbit {
			fmt.Fprintf(log, "[config] CodeRabbit mode: ON — will use CodeRabbit's Change Impact when available\n")
		}
		var err error
		result, err = gh.FetchSinglePR(ctx, client, params.PRUrl, params.UseCodeRabbit)
		if err != nil {
			return nil, fmt.Errorf("fetch failed: %w", err)
		}
	} else {
		// Query mode — resolve authors, validate, then fetch.
		authors := ResolveAuthors(params.Authors, teams, log)
		if len(authors) == 0 {
			return nil, fmt.Errorf("no authors provided")
		}
		if len(params.Repos) == 0 {
			return nil, fmt.Errorf("no repos provided")
		}

		days := params.Days
		if days <= 0 {
			fmt.Fprintf(log, "[guard] days was %d — defaulting to 7\n", days)
			days = 7
		}
		if days > 14 {
			return nil, fmt.Errorf("days cannot exceed 14 (got %d)", days)
		}

		since := time.Now().AddDate(0, 0, -days)
		until := time.Now()

		fmt.Fprintf(log, "[config] Repos: %v\n", params.Repos)
		fmt.Fprintf(log, "[config] Authors: %v\n", authors)
		fmt.Fprintf(log, "[config] Date range: %s to %s (%d days)\n",
			since.Format("2006-01-02"), until.Format("2006-01-02"), days)

		if params.UseCodeRabbit {
			fmt.Fprintf(log, "[config] CodeRabbit mode: ON — will use CodeRabbit's Change Impact when available\n")
		}

		var err error
		result, err = gh.FetchPRs(ctx, client, gh.FetchOptions{
			Repos:           params.Repos,
			Authors:         authors,
			Since:           since,
			Until:           until,
			FetchCodeRabbit: params.UseCodeRabbit,
		})
		if err != nil {
			return nil, fmt.Errorf("fetch failed: %w", err)
		}
	}

	if len(result.PRs) == 0 {
		fmt.Fprintf(log, "No PRs found for the given criteria.\n")
		return nil, nil
	}

	fmt.Fprintf(log, "[fetch] %d PRs found\n", len(result.PRs))

	// Phase 2: Filter for untested mode.
	// Skipped when: single-PR mode (user explicitly asked for that PR),
	// or skip_analysis=true (list mode fetches all PRs and lets markUntested
	// sort them into the two-table split — filtering here would leave no
	// reviewed/open PRs for the second table).
	if params.Mode == "untested" && params.PRUrl == "" && !params.SkipAnalysis {
		qaMembers := GetQATeamMembers(teams)
		var untested []gh.PRData
		for _, pr := range result.PRs {
			if pr.PRState != "merged" {
				continue
			}
			if !HasQAApproval(pr.Approvers, qaMembers) {
				untested = append(untested, pr)
			}
		}
		fmt.Fprintf(log, "[untested] %d merged PRs without QA approval (out of %d total)\n",
			len(untested), len(result.PRs))
		result.PRs = untested

		if len(result.PRs) == 0 {
			fmt.Fprintf(log, "No untested merged PRs found — all merged PRs have QA approval.\n")
			return nil, nil
		}
	}

	// Phase 3: Analyze (or skip)
	var report *analysis.RiskReport
	if params.SkipAnalysis {
		fmt.Fprintf(log, "[analyze] Skipped — listing PRs only\n")
		report = buildSkeletonReport(result.PRs)
	} else {
		var err error
		report, err = analysis.Analyze(ctx, result.PRs, log)
		if err != nil {
			return nil, fmt.Errorf("analysis failed: %w", err)
		}
	}

	// Mark each PR as untested (merged + no QA team approval) so the UI and
	// webhook output can split the report into "Untested" / "Reviewed" sections.
	markUntested(report, result.PRs, teams)

	// Save report if requested
	if params.SaveReport != "" {
		reportJSON, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshaling report: %w", err)
		}
		if err := os.WriteFile(params.SaveReport, reportJSON, 0644); err != nil {
			return nil, fmt.Errorf("writing report: %w", err)
		}
		fmt.Fprintf(log, "[report] Saved to %s\n", params.SaveReport)
	}

	// Phase 4: Webhook delivery (skipped when caller asked for preview)
	if params.SkipWebhook {
		fmt.Fprintf(log, "[webhook] Skipped (preview mode)\n")
	} else if params.DryRun {
		if err := output.SendWebhook(report, params.WebhookURL, true); err != nil {
			fmt.Fprintf(log, "[ERROR] Webhook dry-run failed: %v\n", err)
		}
	} else if params.WebhookURL != "" {
		// Send directly — SendWebhook surfaces the actual server error if delivery fails.
		// (We don't pre-check with CheckWebhook because that posts an extra test message.)
		if err := output.SendWebhook(report, params.WebhookURL, false); err != nil {
			fmt.Fprintf(log, "[ERROR] Webhook delivery failed: %v\n", err)
			fmt.Fprintf(log, "[ERROR] Webhook URL: %s\n", params.WebhookURL)
			fmt.Fprintf(log, "[ERROR] Report was generated but NOT delivered. Check the webhook URL or server status.\n")

			// Failover: save the report to disk so it isn't lost.
			// Only when the caller didn't already ask for SaveReport (avoids duplicate write).
			if params.SaveReport == "" {
				if savedPath, saveErr := SaveReportFallback(report); saveErr == nil {
					fmt.Fprintf(log, "[failover] Report saved to %s\n", savedPath)
				} else {
					fmt.Fprintf(log, "[failover] Could not save fallback report: %v\n", saveErr)
				}
			}
		} else {
			fmt.Fprintf(log, "[webhook] Delivered\n")
		}
	} else {
		fmt.Fprintf(log, "[webhook] Skipped (no webhook URL configured)\n")
	}

	return report, nil
}

// markUntested flags each PR in the report as untested if it is merged AND
// no member of the QA team has approved it. Approver data lives on the original
// gh.PRData (not on analysis.PRRisk), so we correlate by PR number.
func markUntested(report *analysis.RiskReport, prData []gh.PRData, teams map[string]interface{}) {
	if report == nil {
		return
	}
	qaMembers := GetQATeamMembers(teams)
	if len(qaMembers) == 0 {
		return // no QA team configured — nothing to mark
	}
	approverMap := make(map[int][]string, len(prData))
	for _, p := range prData {
		approverMap[p.PRNumber] = p.Approvers
	}
	for i := range report.PRs {
		pr := &report.PRs[i]
		if pr.PRState != "merged" {
			continue
		}
		approvers := approverMap[pr.PRNumber]
		if !HasQAApproval(approvers, qaMembers) {
			pr.IsUntested = true
		}
	}
}

// SaveReportFallback writes the risk report as JSON to ~/.ghquery/reports/
// when webhook delivery fails. Returns the absolute path on success.
func SaveReportFallback(report *analysis.RiskReport) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home dir: %w", err)
	}
	dir := filepath.Join(home, ".ghquery", "reports")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("creating reports dir: %w", err)
	}
	filename := time.Now().Format("2006-01-02-150405") + ".json"
	path := filepath.Join(dir, filename)

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling report: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", fmt.Errorf("writing report: %w", err)
	}
	return path, nil
}

// ResolveAuthors expands team names and validates authors against config.
func ResolveAuthors(input []string, teams map[string]interface{}, log io.Writer) []string {
	knownUsers := make(map[string]string)
	for _, val := range teams {
		if members, ok := val.([]interface{}); ok {
			for _, m := range members {
				if s, ok := m.(string); ok {
					knownUsers[strings.ToLower(s)] = s
				}
			}
		}
	}

	seen := make(map[string]bool)
	var resolved []string
	var rejected []string

	for _, entry := range input {
		lower := strings.ToLower(strings.TrimSpace(entry))

		if members, ok := GetTeamMembers(teams, lower); ok {
			fmt.Fprintf(log, "[teams] Resolved %q → %v\n", entry, members)
			for _, m := range members {
				key := strings.ToLower(m)
				if !seen[key] {
					seen[key] = true
					resolved = append(resolved, m)
				}
			}
			continue
		}

		if original, ok := knownUsers[lower]; ok {
			if !seen[lower] {
				seen[lower] = true
				resolved = append(resolved, original)
			}
			continue
		}

		rejected = append(rejected, entry)
	}

	if len(rejected) > 0 {
		fmt.Fprintf(log, "[warning] Skipped unknown authors (not in config): %s\n", strings.Join(rejected, ", "))
	}

	return resolved
}

// GetQATeamMembers returns the lowercased usernames of the QA team.
func GetQATeamMembers(teams map[string]interface{}) map[string]bool {
	members, ok := GetTeamMembers(teams, "qa")
	if !ok {
		return nil
	}
	set := make(map[string]bool)
	for _, m := range members {
		set[strings.ToLower(m)] = true
	}
	return set
}

// HasQAApproval returns true if any approver is on the QA team.
func HasQAApproval(approvers []string, qaMembers map[string]bool) bool {
	for _, a := range approvers {
		if qaMembers[strings.ToLower(a)] {
			return true
		}
	}
	return false
}

// buildSkeletonReport creates a report from PR data without risk analysis.
// PRs that have a parsed CodeRabbit Change Impact still get their risk
// fields populated from CodeRabbit — we already paid the API call to fetch
// the comment, so there's no reason to drop the data on the floor.
func buildSkeletonReport(prs []gh.PRData) *analysis.RiskReport {
	var prRisks []analysis.PRRisk
	repoSet := make(map[string]bool)

	for _, pr := range prs {
		repoSet[pr.Repo] = true
		if pr.CodeRabbitImpact != "" {
			prRisks = append(prRisks, analysis.BuildCodeRabbitPRRisk(pr))
			continue
		}
		prRisks = append(prRisks, analysis.PRRisk{
			PRNumber: pr.PRNumber,
			PRURL:    pr.PRURL,
			Repo:     pr.Repo,
			Author:   pr.Author,
			Title:    pr.Title,
			PRState:  pr.PRState,
		})
	}

	var repos []string
	for r := range repoSet {
		repos = append(repos, r)
	}

	return &analysis.RiskReport{
		Summary: analysis.RiskSummary{
			TotalPRs:      len(prs),
			ReposAffected: repos,
		},
		PRs: prRisks,
	}
}

// GetTeamMembers returns the members of a team by name (case-insensitive).
func GetTeamMembers(teams map[string]interface{}, name string) ([]string, bool) {
	lower := strings.ToLower(name)
	val, ok := teams[lower]
	if !ok {
		return nil, false
	}

	switch v := val.(type) {
	case []interface{}:
		var members []string
		for _, m := range v {
			if s, ok := m.(string); ok {
				members = append(members, s)
			}
		}
		if len(members) > 0 {
			return members, true
		}
	}

	return nil, false
}
