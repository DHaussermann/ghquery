package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const starterConfig = `# ghquery configuration
# See config.example.yaml for the full annotated template.

catalog:
  repos:
    - org/repo-name         # replace with your GitHub repos (owner/name)
  teams:
    Engineering:
      - github-handle       # replace with your team's GitHub handles
    QA:
      - qa-github-handle

query:
  repos:
    - org/repo-name
  authors:
    - github-handle
  days: 7
  mode: all                 # all | untested
  skip_analysis: false

schedule:
  enabled: false
  frequency: weekdays       # daily | weekdays | weekly
  time: "08:00"
  tz: "America/New_York"

github_token: ""            # https://github.com/settings/tokens
webhook_url:  ""            # your Slack-compatible incoming webhook URL
`

// claudeSearchPaths returns common locations where Claude Code is installed
// outside of the user's PATH (e.g. npm global prefix not on PATH).
func claudeSearchPaths() []string {
	home, _ := os.UserHomeDir()
	bin := "claude"
	if runtime.GOOS == "windows" {
		bin = "claude.cmd"
	}
	candidates := []string{
		filepath.Join(home, ".local", "bin", bin),
		filepath.Join(home, ".npm-global", "bin", bin),
		filepath.Join(home, "npm", "bin", bin),                          // Windows npm global
		filepath.Join(home, "AppData", "Roaming", "npm", bin),           // Windows
		filepath.Join(home, "AppData", "Local", "npm", bin),             // Windows alt
		"/usr/local/bin/" + bin,
		"/opt/homebrew/bin/" + bin,
		"/usr/local/lib/node_modules/.bin/" + bin,
	}
	return candidates
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Set up ghquery for first use",
	Long:  "Checks prerequisites and creates a starter config.yaml if one does not already exist.",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("ghquery init")
		fmt.Println()

		// ── 1. Check claude CLI ──────────────────────────────────────────
		claudePath, err := exec.LookPath("claude")
		if err != nil {
			// Not on PATH — search common install locations
			found := ""
			for _, p := range claudeSearchPaths() {
				if _, statErr := os.Stat(p); statErr == nil {
					found = p
					break
				}
			}
			if found != "" {
				fmt.Printf("  ✓  claude found at %s (not on PATH)\n", found)
				fmt.Println("     Saving path to config.yaml so ghquery can use it directly.")
				viper.Set("claude_path", found)
				if viper.ConfigFileUsed() != "" {
					_ = viper.WriteConfig()
				}
			} else {
				fmt.Println("  ✗  claude CLI not found")
				fmt.Println("     Install: npm install -g @anthropic-ai/claude-code")
				fmt.Println("     Then run: claude   (to authenticate)")
			}
		} else {
			fmt.Printf("  ✓  claude found at %s\n", claudePath)
		}

		// ── 2. Create config.yaml if missing ────────────────────────────
		fmt.Println()
		if _, statErr := os.Stat("config.yaml"); statErr == nil {
			fmt.Println("  ✓  config.yaml already exists — skipping")
		} else {
			if writeErr := os.WriteFile("config.yaml", []byte(starterConfig), 0644); writeErr != nil {
				return fmt.Errorf("writing config.yaml: %w", writeErr)
			}
			// If we found claude in a non-PATH location, persist it into the new config
			if cp := viper.GetString("claude_path"); cp != "" {
				viper.SetConfigFile("config.yaml")
				_ = viper.ReadInConfig()
				viper.Set("claude_path", cp)
				_ = viper.WriteConfig()
			}
			fmt.Println("  ✓  config.yaml created")
			fmt.Println("     Open it and fill in your github_token, webhook_url,")
			fmt.Println("     catalog.repos, and catalog.teams before running.")
		}

		// ── 3. Next steps ────────────────────────────────────────────────
		fmt.Println()
		fmt.Println("Next steps:")
		fmt.Println("  1. Edit config.yaml with your repos, teams, and tokens")
		fmt.Println("  2. Run:  ghquery ui    (web interface)")
		fmt.Println("     or:   ghquery run   (command line)")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
