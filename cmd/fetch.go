package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	gh "github.com/DHaussermann/ghquery/internal/github"
	"github.com/DHaussermann/ghquery/internal/pipeline"
)

var fetchOutput string

var fetchCmd = &cobra.Command{
	Use:   "fetch",
	Short: "Fetch PRs from GitHub repos",
	Long:  "Fetches pull requests filtered by author and date range, outputs JSON with PR data and diffs.",
	RunE:  runFetch,
}

func init() {
	fetchCmd.Flags().StringVarP(&fetchOutput, "output", "o", "", "output file path (default: stdout)")
	rootCmd.AddCommand(fetchCmd)
}

func runFetch(cmd *cobra.Command, args []string) error {
	// Prefer query.* over top-level for the saved scheduled query
	repos, authors, days, _ := resolveQueryDefaults()

	authors = pipeline.ResolveAuthors(authors, viper.GetStringMap("catalog.teams"), os.Stderr)

	if len(authors) == 0 {
		return fmt.Errorf("--authors is required (comma-separated GitHub usernames or team names)")
	}
	if len(repos) == 0 {
		return fmt.Errorf("--repos is required or must be set in config.yaml")
	}

	since := time.Now().AddDate(0, 0, -days)
	until := time.Now()

	fmt.Fprintf(os.Stderr, "[config] Repos: %v\n", repos)
	fmt.Fprintf(os.Stderr, "[config] Authors: %v\n", authors)
	fmt.Fprintf(os.Stderr, "[config] Date range: %s to %s (%d days)\n",
		since.Format("2006-01-02"), until.Format("2006-01-02"), days)

	client := gh.NewClient(viper.GetString("github_token"), os.Stderr)
	ctx := context.Background()

	result, err := gh.FetchPRs(ctx, client, gh.FetchOptions{
		Repos:   repos,
		Authors: authors,
		Since:   since,
		Until:   until,
	})
	if err != nil {
		return fmt.Errorf("fetch failed: %w", err)
	}

	fmt.Fprintf(os.Stderr, "[result] %d PRs fetched\n", len(result.PRs))

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling JSON: %w", err)
	}

	if fetchOutput != "" {
		if err := os.WriteFile(fetchOutput, data, 0644); err != nil {
			return fmt.Errorf("writing output file: %w", err)
		}
		fmt.Fprintf(os.Stderr, "[output] Written to %s\n", fetchOutput)
	} else {
		fmt.Println(string(data))
	}

	return nil
}
