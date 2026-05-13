package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/DHaussermann/ghquery/internal/analysis"
	gh "github.com/DHaussermann/ghquery/internal/github"
)

var analyzeInput string
var analyzeOutput string

var analyzeCmd = &cobra.Command{
	Use:   "analyze",
	Short: "Analyze PRs for risk using Claude",
	Long:  "Reads PR JSON (from fetch output) and runs risk analysis via Claude agent.",
	RunE:  runAnalyze,
}

func init() {
	analyzeCmd.Flags().StringVarP(&analyzeInput, "input", "i", "", "input JSON file from fetch (required)")
	analyzeCmd.Flags().StringVarP(&analyzeOutput, "output", "o", "", "output file for risk report (default: stdout)")
	analyzeCmd.MarkFlagRequired("input")
	rootCmd.AddCommand(analyzeCmd)
}

func runAnalyze(cmd *cobra.Command, args []string) error {
	data, err := os.ReadFile(analyzeInput)
	if err != nil {
		return fmt.Errorf("reading input: %w", err)
	}

	var result gh.FetchResult
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("parsing input JSON: %w", err)
	}

	if len(result.PRs) == 0 {
		return fmt.Errorf("no PRs found in input file")
	}

	fmt.Fprintf(os.Stderr, "[analyze] Processing %d PRs...\n", len(result.PRs))

	report, err := analysis.Analyze(context.Background(), result.PRs, os.Stderr)
	if err != nil {
		return fmt.Errorf("analysis failed: %w", err)
	}

	reportJSON, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling report: %w", err)
	}

	if analyzeOutput != "" {
		if err := os.WriteFile(analyzeOutput, reportJSON, 0644); err != nil {
			return fmt.Errorf("writing output: %w", err)
		}
		fmt.Fprintf(os.Stderr, "[analyze] Risk report written to %s\n", analyzeOutput)
	} else {
		fmt.Println(string(reportJSON))
	}

	return nil
}
