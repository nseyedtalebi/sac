package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	storeRoot string
	logPath   string
)

var rootCmd = &cobra.Command{
	Use:   "sac",
	Short: "sac is a content-addressed store with a hash-chained lineage log",
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&storeRoot, "store", os.Getenv("SAC_STORE"), "content-addressed store root")
	rootCmd.PersistentFlags().StringVar(&logPath, "log", os.Getenv("SAC_LOG"), "lineage database path")
}
