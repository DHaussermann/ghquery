package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/DHaussermann/ghquery/internal/config"
)

var rootCmd = &cobra.Command{
	Use:   "ghquery",
	Short: "GitHub commit query and risk analysis tool",
	Long:  "Fetches commits from GitHub repos, analyzes risk via Claude, and displays results.",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	// CLI flags are still defined so users can pass them, but they are NOT
	// bound to viper — binding would write empty defaults into config.yaml on
	// every save. The actual query values come from the catalog.* / query.*
	// blocks in config.yaml.
	rootCmd.PersistentFlags().StringSlice("repos", nil, "repos to query (owner/name), comma-separated")
	rootCmd.PersistentFlags().StringSlice("authors", nil, "GitHub usernames to filter by, comma-separated")
	rootCmd.PersistentFlags().Int("days", 0, "number of days to look back (0 = use config.yaml query.days)")
}

func initConfig() {
	// AutomaticEnv lets any key be overridden by an environment variable. The
	// replacer maps nested keys (catalog.teams) and bare keys (github_token)
	// to their env-var forms (CATALOG_TEAMS, GITHUB_TOKEN). In hosted mode this
	// is how secrets (GITHUB_TOKEN, ANTHROPIC_API_KEY) arrive — never on disk.
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	if config.IsHosted() {
		// Hosted mode: read the global catalog from a read-only mounted file.
		// Per-user preferences live in the browser, not here, so there is no
		// config.yaml to read or write.
		catalogPath := config.CatalogPath()
		viper.SetConfigFile(catalogPath)
		viper.SetConfigType("yaml")
		if err := viper.ReadInConfig(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: hosted mode could not read catalog %q: %v\n", catalogPath, err)
		}
		return
	}

	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			fmt.Fprintf(os.Stderr, "Warning: error reading config: %v\n", err)
		}
	}
}
