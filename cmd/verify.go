package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"sac/lineage"
)

var logPath string

var verifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "walk the lineage log and confirm its hash chain is intact",
	RunE: func(cmd *cobra.Command, args []string) error {
		if logPath == "" {
			return fmt.Errorf("--log is required")
		}
		l, err := lineage.Open(logPath)
		if err != nil {
			return err
		}
		defer l.Close()
		count, err := l.Verify()
		if err != nil {
			return fmt.Errorf("chain broken after %d verified events: %w", count, err)
		}
		fmt.Printf("ok: %d events verified\n", count)
		return nil
	},
}

func init() {
	verifyCmd.Flags().StringVar(&logPath, "log", "", "lineage database path (required)")
	rootCmd.AddCommand(verifyCmd)
}
