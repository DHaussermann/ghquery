package analysis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	gh "github.com/DHaussermann/ghquery/internal/github"
)

const (
	passATimeout  = 90 * time.Second
	passBTimeout  = 120 * time.Second
	maxConcurrent = 3
)

// passAPrompt is the constrained enumeration prompt — Pass A's only job is
// to list new code branches and what tests cover. No scoring, no recommendations.
// The granularity is intentionally aggressive: each error return, each select arm,
// each platform variant counts as a separate branch.
const passAPrompt = `You are analyzing a single GitHub pull request diff. Your ONLY job is to enumerate new code branches and what test changes cover. Do NOT score risk. Do NOT write recommendations. Do NOT add commentary.

A "new branch" is granular — count each of these as a SEPARATE branch:
- A new function or method declaration
- A new error return path (any new ` + "`return ... err`" + ` not present before)
- A new conditional arm (if/else, switch case, type switch case)
- A new platform-specific build-tagged file or function variant (linux/darwin/windows/etc)
- A new goroutine launch (` + "`go func()`" + `, ` + "`go someFunc()`" + `)
- A new timeout, context cancellation, or select branch
- A new panic, recovery, or defer
- A new public struct field (especially in a serialized type)

A "test change" is any addition or modification to a *_test.go or test framework file. For each, note what scenario or branch it actually exercises (be specific — "tests linux happy path" not "tests linux").

Be granular. A new platform-specific file with a happy path and two error returns has at least 4 branches (the function declaration + happy path + 2 error returns).

Output ONLY a JSON object with this exact shape (no markdown fences, no preamble):

{
  "new_branches": [
    {"path": "server/foo/bar.go", "branch": "short description"}
  ],
  "test_changes": [
    {"path": "server/foo/bar_test.go", "covers": "what scenario is exercised"}
  ]
}

If there are no new branches or no test changes, return empty arrays. Output ONLY the JSON.`

// Analyze runs risk analysis on each PR individually in parallel (max 3 at a time).
// Each PR gets its own isolated Claude call — no context pollution between PRs.
func Analyze(ctx context.Context, prs []gh.PRData, log io.Writer) (*RiskReport, error) {
	agentPrompt, err := loadAgentPrompt()
	if err != nil {
		return nil, err
	}

	// Pre-flight: verify `claude` is on PATH before spawning agents.
	// If not, surface a single clear error at the top of the log so the user
	// notices immediately instead of seeing N identical per-agent failures.
	if claudePath, err := exec.LookPath("claude"); err != nil {
		fmt.Fprintf(log, "[ERROR] claude CLI not found on PATH — risk analysis will FAIL for all %d PRs\n", len(prs))
		fmt.Fprintf(log, "[ERROR] Install: npm install -g @anthropic-ai/claude-code\n")
		fmt.Fprintf(log, "[ERROR] If running via launchd/cron, ensure `ghquery schedule install` was run from a shell where `which claude` works\n")
	} else {
		fmt.Fprintf(log, "[analyze] claude found at %s\n", claudePath)
	}

	// Partition PRs by source. CodeRabbit-sourced rows and closed (never-merged)
	// PRs skip Pass A/B entirely — no tokens spent on abandoned work.
	results := make([]PRRisk, len(prs))
	var crCount, closedCount, agentCount int
	for i, pr := range prs {
		if pr.CodeRabbitImpact != "" {
			results[i] = BuildCodeRabbitPRRisk(pr)
			crCount++
		} else if pr.PRState == "closed" {
			results[i] = PRRisk{
				PRNumber: pr.PRNumber,
				PRURL:    pr.PRURL,
				Repo:     pr.Repo,
				Author:   pr.Author,
				Title:    pr.Title,
				PRState:  pr.PRState,
			}
			closedCount++
		} else {
			agentCount++
		}
	}

	fmt.Fprintf(log, "[analyze] %d PRs total — %d sourced from CodeRabbit, %d closed (skipped), %d to analyze by agent (max %d concurrent)\n",
		len(prs), crCount, closedCount, agentCount, maxConcurrent)

	agentNames := []string{"alpha", "beta", "gamma"}

	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup

	for i, pr := range prs {
		if pr.CodeRabbitImpact != "" || pr.PRState == "closed" {
			continue // already populated above
		}
		wg.Add(1)
		go func(idx int, pr gh.PRData) {
			defer wg.Done()

			sem <- struct{}{}
			slotID := len(sem) - 1
			if slotID < 0 || slotID >= len(agentNames) {
				slotID = idx % len(agentNames)
			}
			agent := agentNames[slotID]
			defer func() { <-sem }()

			prLabel := fmt.Sprintf("PR #%d", pr.PRNumber)
			fmt.Fprintf(log, "[agent:%s] %s — %s — picked up\n", agent, prLabel, pr.Title)

			start := time.Now()
			result, err := analyzeSinglePR(ctx, agentPrompt, pr, log, agent)
			elapsed := time.Since(start).Round(time.Second)

			if err != nil {
				fmt.Fprintf(log, "[agent:%s] %s — FAILED after %v — %v\n", agent, prLabel, elapsed, err)
				results[idx] = PRRisk{
					PRNumber:   pr.PRNumber,
					PRURL:      pr.PRURL,
					Repo:       pr.Repo,
					Author:     pr.Author,
					Title:      pr.Title,
					PRState:    pr.PRState,
					RiskLevel:  RiskUnknown,
					RiskReason: fmt.Sprintf("Analysis failed: %v", err),
					Error:      err.Error(),
				}
				return
			}

			result.PRNumber = pr.PRNumber
			result.PRURL = pr.PRURL
			result.Repo = pr.Repo
			result.Author = pr.Author
			result.Title = pr.Title
			result.PRState = pr.PRState

			fmt.Fprintf(log, "[agent:%s] %s — done in %v — %s\n", agent, prLabel, elapsed, result.RiskLevel)
			results[idx] = *result
		}(i, pr)
	}

	wg.Wait()

	report := buildReport(results)
	return report, nil
}

func analyzeSinglePR(ctx context.Context, agentPrompt string, pr gh.PRData, log io.Writer, agent string) (*PRRisk, error) {
	prLabel := fmt.Sprintf("PR #%d", pr.PRNumber)

	// Pass A: enumerate new branches in the diff. If this fails, we degrade
	// gracefully — Pass B runs without the pre-computed branch context.
	branchContext := ""
	passAStart := time.Now()
	enum, enumErr := enumerateBranches(ctx, pr)
	passAElapsed := time.Since(passAStart).Round(time.Second)
	if enumErr != nil {
		fmt.Fprintf(log, "[agent:%s] %s — pass A FAILED after %v (continuing without enumeration): %v\n", agent, prLabel, passAElapsed, enumErr)
	} else {
		fmt.Fprintf(log, "[agent:%s] %s — pass A done in %v (%d branches, %d test cases)\n", agent, prLabel, passAElapsed, len(enum.NewBranches), len(enum.TestChanges))
		enumJSON, _ := json.MarshalIndent(enum, "", "  ")
		branchContext = fmt.Sprintf("\n\nPRE-COMPUTED BRANCH ENUMERATION (use this as your AUTHORITATIVE list for Step 5a/5b — do not re-derive):\n%s\n", string(enumJSON))
	}

	// Pass B: score the PR. The branchContext (if Pass A succeeded) anchors
	// the scoring agent's enumeration step on a concrete pre-computed list.
	inputJSON, err := json.Marshal(pr)
	if err != nil {
		return nil, fmt.Errorf("marshaling PR: %w", err)
	}

	prompt := fmt.Sprintf("%s\n\nAnalyze the following single pull request and return ONLY a JSON object with these fields: risk_level, risk_score, dimensions, risk_reason, areas_affected, qa_recommendations, test_approach. No markdown fences, no preamble.%s\n\n%s", agentPrompt, branchContext, string(inputJSON))

	passBCtx, cancel := context.WithTimeout(ctx, passBTimeout)
	defer cancel()

	cmd := exec.CommandContext(passBCtx, "claude", "-p", "--output-format", "json")
	cmd.Stdin = strings.NewReader(prompt)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if passBCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("pass B timed out after %v", passBTimeout)
		}
		return nil, fmt.Errorf("pass B claude CLI: %w\nstderr: %s", err, truncate(stderr.String(), 200))
	}

	return parsePRResult(stdout.Bytes())
}

func enumerateBranches(ctx context.Context, pr gh.PRData) (*BranchEnumeration, error) {
	inputJSON, err := json.Marshal(pr)
	if err != nil {
		return nil, fmt.Errorf("marshaling PR: %w", err)
	}

	prompt := fmt.Sprintf("%s\n\n%s", passAPrompt, string(inputJSON))

	passACtx, cancel := context.WithTimeout(ctx, passATimeout)
	defer cancel()

	cmd := exec.CommandContext(passACtx, "claude", "-p", "--output-format", "json")
	cmd.Stdin = strings.NewReader(prompt)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if passACtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("pass A timed out after %v", passATimeout)
		}
		return nil, fmt.Errorf("pass A claude CLI: %w\nstderr: %s", err, truncate(stderr.String(), 200))
	}

	return parseBranchEnumeration(stdout.Bytes())
}

func parseBranchEnumeration(raw []byte) (*BranchEnumeration, error) {
	var claudeResp struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(raw, &claudeResp); err == nil && claudeResp.Result != "" {
		raw = []byte(claudeResp.Result)
	}

	var enum BranchEnumeration
	if err := json.Unmarshal(raw, &enum); err == nil && (len(enum.NewBranches) > 0 || len(enum.TestChanges) > 0) {
		return &enum, nil
	}

	text := string(raw)
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("no JSON in pass A response:\n%s", truncate(text, 300))
	}

	jsonStr := text[start : end+1]
	if err := json.Unmarshal([]byte(jsonStr), &enum); err != nil {
		return nil, fmt.Errorf("parsing pass A JSON: %w", err)
	}
	return &enum, nil
}

func parsePRResult(raw []byte) (*PRRisk, error) {
	var claudeResp struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(raw, &claudeResp); err == nil && claudeResp.Result != "" {
		raw = []byte(claudeResp.Result)
	}

	var risk PRRisk
	if err := json.Unmarshal(raw, &risk); err == nil && risk.RiskLevel != "" {
		return &risk, nil
	}

	text := string(raw)
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("no JSON in response:\n%s", truncate(text, 300))
	}

	jsonStr := text[start : end+1]
	if err := json.Unmarshal([]byte(jsonStr), &risk); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}

	return &risk, nil
}

// BuildCodeRabbitPRRisk builds a PRRisk row directly from a PR's parsed
// CodeRabbit Change Impact data — no agent passes are run.
//
// Score and dimensions are intentionally left zero. CodeRabbit produces a
// tier (HIGH/MEDIUM/LOW) but no numeric score and no 6-dimension breakdown.
// Renderers should display these as "N/A" for CodeRabbit-sourced rows
// rather than synthesizing fake numbers.
//
// Exported so the pipeline's skip-analysis path can reuse the same mapping.
func BuildCodeRabbitPRRisk(pr gh.PRData) PRRisk {
	level := RiskUnknown
	switch pr.CodeRabbitImpact {
	case "HIGH":
		level = RiskHigh
	case "MEDIUM":
		level = RiskMedium
	case "LOW":
		level = RiskLow
	}

	var qa []string
	if pr.CodeRabbitQA != "" {
		qa = []string{pr.CodeRabbitQA}
	}

	return PRRisk{
		PRNumber:          pr.PRNumber,
		PRURL:             pr.PRURL,
		Repo:              pr.Repo,
		Author:            pr.Author,
		Title:             pr.Title,
		PRState:           pr.PRState,
		RiskLevel:         level,
		RiskScore:         0, // not synthesized — UI shows N/A
		RiskReason:        pr.CodeRabbitReason,
		QARecommendations: qa,
		Source:            "coderabbit",
	}
}

func buildReport(results []PRRisk) *RiskReport {
	summary := RiskSummary{
		TotalPRs:    len(results),
		OverallRisk: RiskLow,
	}

	repoSet := make(map[string]bool)

	for _, r := range results {
		repoSet[r.Repo] = true

		switch r.RiskLevel {
		case RiskHigh:
			summary.HighRisk++
			summary.OverallRisk = RiskHigh
		case RiskMedium:
			summary.MediumRisk++
			if summary.OverallRisk != RiskHigh {
				summary.OverallRisk = RiskMedium
			}
		case RiskUnknown:
			summary.Unknown++
		default:
			summary.LowRisk++
		}
	}

	for repo := range repoSet {
		summary.ReposAffected = append(summary.ReposAffected, repo)
	}

	return &RiskReport{
		Summary: summary,
		PRs:     results,
	}
}

func loadAgentPrompt() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting working directory: %w", err)
	}

	path := filepath.Join(wd, ".claude", "agents", "risk-analyzer.md")
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("risk-analyzer.md not found at .claude/agents/risk-analyzer.md")
	}

	text := string(content)
	if strings.HasPrefix(text, "---") {
		if end := strings.Index(text[3:], "---"); end != -1 {
			text = strings.TrimSpace(text[end+6:])
		}
	}

	return text, nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
