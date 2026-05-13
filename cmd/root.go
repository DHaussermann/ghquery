package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			fmt.Fprintf(os.Stderr, "Warning: error reading config: %v\n", err)
		}
	}
}
