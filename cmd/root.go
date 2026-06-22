package cmd

import (
	"os"

	"github.com/spf13/cobra"
	// Import the mv command package
)

// Global commands
var (
	projectDir string // Global variable to store the flag's target string

	rootCmd = &cobra.Command{
		Use:   "asted",
		Short: "asted is a lightning-fast Go source code refactoring engine",
		Long:  `A highly specialized abstract syntax tree (AST) manipulation utility designed to move and rename declarations flawlessly across package boundaries.`,
	}
)

func init() {
	// Register the --dir / -d flag persistently across all application subcommands.
	rootCmd.PersistentFlags().StringVarP(&projectDir, "dir", "d", ".", "Path to the target project workspace directory")

	// Register mv command
	rootCmd.AddCommand(mvCmd)
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
