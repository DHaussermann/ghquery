package output

import (
	"fmt"
	"strings"

	"github.com/rodaine/table"

	"github.com/DHaussermann/ghquery/internal/analysis"
)

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
	colorGreen  = "\033[32m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
)

func PrintTable(report *analysis.RiskReport) {
	fmt.Println()
	fmt.Printf("%s%s PR Risk Report %s\n", colorBold, "===", colorReset)
	fmt.Printf("%sOverall Risk: %s%s\n\n",
		colorBold, colorizeRisk(report.Summary.OverallRisk), colorReset)

	tbl := table.New("PR", "Repo", "Author", "State", "Score", "Risk", "Title")

	for _, pr := range report.PRs {
		repo := shortRepo(pr.Repo)
		risk := colorizeRisk(pr.RiskLevel)

		title := pr.Title
		if len(title) > 50 {
			title = title[:47] + "..."
		}

		score := fmt.Sprintf("%.1f", pr.RiskScore)

		tbl.AddRow(
			fmt.Sprintf("#%d", pr.PRNumber),
			repo, pr.Author, pr.PRState, score, risk, title,
		)
	}

	tbl.Print()

	// Summary counts
	fmt.Printf("\n%sHigh: %s%d%s | Medium: %s%d%s | Low: %s%d%s | Total PRs: %d\n",
		colorBold,
		colorRed, report.Summary.HighRisk, colorReset+colorBold,
		colorYellow, report.Summary.MediumRisk, colorReset+colorBold,
		colorGreen, report.Summary.LowRisk, colorReset,
		report.Summary.TotalPRs)

	// QA focus areas for HIGH and MEDIUM
	printQAFocus(report)
}

func printQAFocus(report *analysis.RiskReport) {
	var highItems, medItems []analysis.PRRisk
	for _, pr := range report.PRs {
		switch pr.RiskLevel {
		case analysis.RiskHigh:
			highItems = append(highItems, pr)
		case analysis.RiskMedium:
			medItems = append(medItems, pr)
		}
	}

	if len(highItems) == 0 && len(medItems) == 0 {
		return
	}

	fmt.Printf("\n%s%sQA Focus Areas:%s\n", colorBold, "--- ", colorReset)

	for _, pr := range highItems {
		fmt.Printf("\n  %s[HIGH]%s #%d %s (%s)\n", colorRed, colorReset, pr.PRNumber, pr.Title, shortRepo(pr.Repo))
		fmt.Printf("  %s%s%s\n", colorDim, pr.RiskReason, colorReset)
		for _, rec := range pr.QARecommendations {
			fmt.Printf("    - %s\n", rec)
		}
		if pr.TestApproach != "" {
			fmt.Printf("  %sTest: %s%s\n", colorDim, pr.TestApproach, colorReset)
		}
	}

	for _, pr := range medItems {
		fmt.Printf("\n  %s[MEDIUM]%s #%d %s (%s)\n", colorYellow, colorReset, pr.PRNumber, pr.Title, shortRepo(pr.Repo))
		fmt.Printf("  %s%s%s\n", colorDim, pr.RiskReason, colorReset)
		for _, rec := range pr.QARecommendations {
			fmt.Printf("    - %s\n", rec)
		}
	}

	fmt.Println()
}

func colorizeRisk(level analysis.RiskLevel) string {
	switch level {
	case analysis.RiskHigh:
		return colorRed + "HIGH" + colorReset
	case analysis.RiskMedium:
		return colorYellow + "MEDIUM" + colorReset
	case analysis.RiskLow:
		return colorGreen + "LOW" + colorReset
	default:
		return string(level)
	}
}

func shortRepo(repo string) string {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return repo
}

func wordWrap(text string, width int) string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return ""
	}

	var lines []string
	currentLine := words[0]

	for _, word := range words[1:] {
		if len(currentLine)+1+len(word) > width {
			lines = append(lines, currentLine)
			currentLine = word
		} else {
			currentLine += " " + word
		}
	}
	lines = append(lines, currentLine)

	return strings.Join(lines, "\n")
}
