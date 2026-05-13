package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/DHaussermann/ghquery/internal/analysis"
)

var askInput string
var askInteractive bool

var askCmd = &cobra.Command{
	Use:   "ask [question]",
	Short: "Ask follow-up questions about a risk analysis",
	Long:  "Load a risk report and ask questions about it using Claude.",
	Args:  cobra.ArbitraryArgs,
	RunE:  runAsk,
}

func init() {
	askCmd.Flags().StringVarP(&askInput, "input", "i", "", "risk report JSON file (required)")
	askCmd.Flags().BoolVar(&askInteractive, "interactive", false, "enter interactive question loop")
	askCmd.MarkFlagRequired("input")
	rootCmd.AddCommand(askCmd)
}

func runAsk(cmd *cobra.Command, args []string) error {
	data, err := os.ReadFile(askInput)
	if err != nil {
		return fmt.Errorf("reading report: %w", err)
	}

	var report analysis.RiskReport
	if err := json.Unmarshal(data, &report); err != nil {
		return fmt.Errorf("parsing report: %w", err)
	}

	reportContext, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling report: %w", err)
	}

	if askInteractive {
		return interactiveAskLoop(string(reportContext))
	}

	if len(args) == 0 {
		return fmt.Errorf("provide a question as arguments, or use --interactive")
	}

	question := strings.Join(args, " ")
	return askQuestion(string(reportContext), question)
}

func askQuestion(reportContext, question string) error {
	prompt := fmt.Sprintf("You are a QA risk analysis assistant. You have already analyzed a set of GitHub pull requests. Here is the risk report:\n\n%s\n\nThe user asks: %s\n\nAnswer based on the risk report data. Be specific and actionable.", reportContext, question)

	cmd := exec.Command("claude", "-p", prompt)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("claude CLI failed: %w\nstderr: %s", err, stderr.String())
	}

	fmt.Println(stdout.String())
	return nil
}

func interactiveAskLoop(reportContext string) error {
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println("Risk report loaded. Ask questions (type 'exit' or 'quit' to stop):")
	fmt.Println()

	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}

		question := strings.TrimSpace(scanner.Text())
		if question == "" {
			continue
		}
		if question == "exit" || question == "quit" {
			fmt.Println("Bye.")
			break
		}

		if err := askQuestion(reportContext, question); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		fmt.Println()
	}

	return scanner.Err()
}
