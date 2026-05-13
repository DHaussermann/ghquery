package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/DHaussermann/ghquery/internal/analysis"
)

const (
	maxPostLength = 16383
	botUsername   = "PR Risk Bot"
	botIconURL    = "https://github.githubassets.com/images/modules/logos_page/GitHub-Mark.png"
)

type webhookPayload struct {
	Text     string `json:"text"`
	Username string `json:"username"`
	IconURL  string `json:"icon_url"`
}

// CheckWebhook verifies the webhook URL is valid.
//
// The server validates the text field BEFORE looking up the hook ID, so an
// empty-text probe returns the same error for both valid and invalid hooks.
// We must send non-empty text to force the hook lookup. This means a small
// identifiable test post appears in the channel for valid hooks — that's the
// trade-off for a reliable check.
//
// Status code interpretation:
//   - 200       → hook is valid (test post landed in channel)
//   - 400/404   → invalid: parse body for the specific error
//   - 5xx       → server error
func CheckWebhook(webhookURL string) error {
	client := &http.Client{Timeout: 5 * time.Second}

	payload := []byte(`{"text":"This is a test post from ghquery","username":"ghquery"}`)
	resp, err := client.Post(webhookURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	bodyStr := string(body)

	// Recognize common webhook error patterns
	if strings.Contains(bodyStr, "IncomingWebhook") && strings.Contains(bodyStr, "not found") {
		return fmt.Errorf("webhook not found — the hook ID is invalid")
	}
	if strings.Contains(bodyStr, "Invalid webhook") {
		return fmt.Errorf("webhook invalid — %s", parseWebhookError(bodyStr))
	}
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("webhook not found (404) — URL path is wrong")
	}
	if resp.StatusCode >= 500 {
		return fmt.Errorf("server error (%d)", resp.StatusCode)
	}

	return fmt.Errorf("unexpected response (HTTP %d): %s", resp.StatusCode, parseWebhookError(bodyStr))
}

// parseWebhookError extracts the human-readable message from a webhook JSON error body.
func parseWebhookError(body string) string {
	var parsed struct {
		Message string `json:"message"`
		Detail  string `json:"detailed_error"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err == nil {
		if parsed.Detail != "" {
			return parsed.Detail
		}
		if parsed.Message != "" {
			return parsed.Message
		}
	}
	if len(body) > 200 {
		return body[:200] + "..."
	}
	return body
}

// SendWebhook formats the risk report and POSTs it to the webhook URL.
// If dryRun is true, it prints the payload instead of sending it.
func SendWebhook(report *analysis.RiskReport, webhookURL string, dryRun bool) error {
	posts := buildPosts(report)

	if dryRun {
		fmt.Println("\n--- Webhook Payload (dry run) ---")
		for i, post := range posts {
			fmt.Printf("\n[Post %d/%d]\n%s\n", i+1, len(posts), post)
		}
		fmt.Println("--- End of payload ---")
		return nil
	}

	for i, post := range posts {
		if err := sendPost(webhookURL, post); err != nil {
			return fmt.Errorf("sending post %d/%d: %w", i+1, len(posts), err)
		}
		if i < len(posts)-1 {
			time.Sleep(500 * time.Millisecond)
		}
	}

	fmt.Printf("[webhook] Sent %d post(s)\n", len(posts))
	return nil
}

func sendPost(webhookURL, text string) error {
	payload := webhookPayload{
		Text:     text,
		Username: botUsername,
		IconURL:  botIconURL,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling payload: %w", err)
	}

	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	bodyStr := string(respBody)

	if strings.Contains(bodyStr, "IncomingWebhook") && strings.Contains(bodyStr, "not found") {
		return fmt.Errorf("webhook not found — the hook ID is invalid (HTTP %d)", resp.StatusCode)
	}
	if strings.Contains(bodyStr, "Invalid webhook") {
		return fmt.Errorf("webhook invalid — %s (HTTP %d)", parseWebhookError(bodyStr), resp.StatusCode)
	}
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("webhook URL not found (HTTP 404)")
	}
	if resp.StatusCode >= 500 {
		return fmt.Errorf("webhook server error (HTTP %d)", resp.StatusCode)
	}
	return fmt.Errorf("webhook server rejected the post (HTTP %d): %s", resp.StatusCode, parseWebhookError(bodyStr))
}

func buildPosts(report *analysis.RiskReport) []string {
	var parts []string

	parts = append(parts, buildHeader(report))
	parts = append(parts, drilldownsForGroup(report.PRs, true)...)
	parts = append(parts, drilldownsForGroup(report.PRs, false)...)

	return mergePosts(parts)
}

// drilldownsForGroup emits HIGH then MEDIUM drilldowns for either the untested
// (untested=true) or reviewed (untested=false) subset.
func drilldownsForGroup(prs []analysis.PRRisk, untested bool) []string {
	var parts []string
	groupLabel := "Open or Tested"
	if untested {
		groupLabel = "Merged — Not QA Tested"
	}

	hasHigh := false
	for _, pr := range prs {
		if pr.IsUntested != untested {
			continue
		}
		if pr.RiskLevel == analysis.RiskHigh {
			if !hasHigh {
				parts = append(parts, fmt.Sprintf("\n---\n\n##### :microscope: %s — High-Risk Drilldowns\n\n", groupLabel))
				hasHigh = true
			}
			parts = append(parts, buildDrilldown(pr))
		}
	}

	hasMedium := false
	for _, pr := range prs {
		if pr.IsUntested != untested {
			continue
		}
		if pr.RiskLevel == analysis.RiskMedium {
			if !hasMedium {
				parts = append(parts, fmt.Sprintf("\n---\n\n##### :large_yellow_circle: %s — Medium-Risk Details\n\n", groupLabel))
				hasMedium = true
			}
			parts = append(parts, buildDrilldown(pr))
		}
	}
	return parts
}

func buildHeader(report *analysis.RiskReport) string {
	var sb strings.Builder

	sb.WriteString("##### PR Risk Report\n\n")
	sb.WriteString(fmt.Sprintf(":red_circle: High: **%d** | :large_yellow_circle: Medium: **%d** | :large_green_circle: Low: **%d** | Total: **%d**\n\n", report.Summary.HighRisk, report.Summary.MediumRisk, report.Summary.LowRisk, report.Summary.TotalPRs))

	if hasCodeRabbitRows(report.PRs) {
		sb.WriteString("_:robot_face: Rows marked **(CR)** are sourced from CodeRabbitAI's Change Impact summary._\n\n")
	}

	var untested, reviewed []analysis.PRRisk
	for _, pr := range report.PRs {
		if pr.IsUntested {
			untested = append(untested, pr)
		} else {
			reviewed = append(reviewed, pr)
		}
	}

	showSplit := len(untested) > 0 && len(reviewed) > 0

	if len(untested) > 0 {
		if showSplit {
			sb.WriteString(fmt.Sprintf("##### :warning: Merged work that was not tested (%d) — needs QA attention\n\n", len(untested)))
			sb.WriteString("_Merged PRs with no QA team approval._\n\n")
		} else {
			sb.WriteString("##### Changes\n\n")
		}
		sb.WriteString(buildChangesTable(untested))
	}

	if len(reviewed) > 0 {
		if showSplit {
			sb.WriteString(fmt.Sprintf("\n##### :white_check_mark: Open or tested work (%d)\n\n", len(reviewed)))
			sb.WriteString("_PRs not yet merged or already tested._\n\n")
		} else if len(untested) == 0 {
			sb.WriteString("##### Changes\n\n")
		}
		sb.WriteString(buildChangesTable(reviewed))
	}

	return sb.String()
}

func buildChangesTable(prs []analysis.PRRisk) string {
	var sb strings.Builder
	sb.WriteString("| Risk | Score | PR | Author | State | Summary |\n")
	sb.WriteString("|:----:|:-----:|:---|:-------|:------|:--------|\n")

	for _, pr := range prs {
		title := pr.Title
		if len(title) > 45 {
			title = title[:42] + "..."
		}
		summary := pr.RiskReason
		if len(summary) > 120 {
			summary = summary[:117] + "..."
		}
		var scoreCell, titleSuffix string
		if pr.Source == "coderabbit" {
			scoreCell = "N/A"
			titleSuffix = " **(CR)**"
		} else {
			scoreCell = fmt.Sprintf("%.1f", pr.RiskScore)
		}
		sb.WriteString(fmt.Sprintf("| **%s** | %s | [#%d: %s](%s)%s | @%s | %s | %s |\n",
			shortLevel(pr.RiskLevel),
			scoreCell,
			pr.PRNumber, title, pr.PRURL, titleSuffix,
			pr.Author,
			pr.PRState,
			summary))
	}
	return sb.String()
}

func buildDrilldown(pr analysis.PRRisk) string {
	var sb strings.Builder

	if pr.Source == "coderabbit" {
		sb.WriteString(fmt.Sprintf("%s **[%s] [#%d: %s](%s)** **(CR)** — @%s\n\n",
			riskIcon(pr.RiskLevel), string(pr.RiskLevel), pr.PRNumber, pr.Title, pr.PRURL, pr.Author))
	} else {
		sb.WriteString(fmt.Sprintf("%s **[%.1f] [#%d: %s](%s)** — @%s\n\n",
			riskIcon(pr.RiskLevel), pr.RiskScore, pr.PRNumber, pr.Title, pr.PRURL, pr.Author))
	}

	if len(pr.AreasAffected) > 0 {
		sb.WriteString("**Problem Areas:**\n")
		for _, area := range pr.AreasAffected {
			sb.WriteString(fmt.Sprintf("- `%s`\n", area))
		}
		sb.WriteString("\n")
	}

	if pr.RiskReason != "" {
		sb.WriteString(fmt.Sprintf("> %s\n\n", pr.RiskReason))
	}

	if len(pr.QARecommendations) > 0 {
		sb.WriteString("**Recommended Test Scenarios:**\n")
		for i, rec := range pr.QARecommendations {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, rec))
		}
		sb.WriteString("\n")
	}

	if pr.TestApproach != "" {
		sb.WriteString(fmt.Sprintf("_Test approach: %s_\n\n", pr.TestApproach))
	}

	if pr.Source == "coderabbit" {
		sb.WriteString("_Generated by CodeRabbitAI_\n\n")
	}

	sb.WriteString("---\n\n")

	return sb.String()
}

func hasCodeRabbitRows(prs []analysis.PRRisk) bool {
	for _, pr := range prs {
		if pr.Source == "coderabbit" {
			return true
		}
	}
	return false
}

func mergePosts(parts []string) []string {
	var posts []string
	current := ""

	for _, part := range parts {
		if len(current)+len(part) > maxPostLength {
			if current != "" {
				posts = append(posts, current)
			}
			current = part
		} else {
			current += part
		}
	}
	if current != "" {
		posts = append(posts, current)
	}

	return posts
}

func riskIcon(level analysis.RiskLevel) string {
	switch level {
	case analysis.RiskHigh:
		return ":red_circle:"
	case analysis.RiskMedium:
		return ":large_yellow_circle:"
	case analysis.RiskUnknown:
		return ":white_question_mark:"
	default:
		return ":large_green_circle:"
	}
}

// shortLevel returns a compact label for the Risk Level column in the
// changes table. MEDIUM is abbreviated to MED so the cell doesn't wrap when
// the wider PR / Summary columns claim most of the row width.
func shortLevel(level analysis.RiskLevel) string {
	if level == analysis.RiskMedium {
		return "MED"
	}
	return string(level)
}
