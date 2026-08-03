package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	cfgFile    string
	verbose    bool
	historyDir string
)

// rootCmd is the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:   "caliper",
	Short: "Caliper — Enterprise AI Evaluation Framework",
	Long: `Caliper is an offline-first, Git-backed AI evaluation framework.

It parses a YAML configuration, resolves evaluator dependencies into a
Directed Acyclic Graph (DAG), and executes them concurrently against
a configured provider.`,
}

// Execute adds all child commands to the root command and sets flags
// appropriately. This is called by main.main().
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// Persistent flags are inherited by all sub-commands.
	rootCmd.PersistentFlags().StringVarP(
		&cfgFile, "config", "c", "./caliper.yml",
		"path to the caliper configuration file",
	)
	rootCmd.PersistentFlags().BoolVarP(
		&verbose, "verbose", "v", false,
		"enable verbose output",
	)
	rootCmd.PersistentFlags().StringVar(
		&historyDir, "history-dir", ".caliper",
		"root directory for local history storage (date-sharded JSON)",
	)

	// PersistentPreRunE runs before every sub-command, giving us a single
	// place to initialise shared concerns (logging, config validation, etc.)
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if verbose {
			fmt.Fprintln(os.Stderr, "[caliper] verbose mode enabled")
		}
		return nil
	}

	// Register sub-commands.
	rootCmd.AddCommand(evaluateCmd)
	rootCmd.AddCommand(syncCmd)
}
