package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nseyedtalebi/sac/cas"
	"github.com/nseyedtalebi/sac/catalog"
)

var verifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "verify every artifact known to the SQLite inventory",
	RunE: func(cmd *cobra.Command, args []string) error {
		if storeRoot == "" {
			return fmt.Errorf("--store is required")
		}
		if catalogPath == "" {
			return fmt.Errorf("--catalog is required")
		}
		store, err := cas.Open(storeRoot)
		if err != nil {
			return err
		}
		inventory, err := catalog.Open(catalogPath)
		if err != nil {
			return err
		}
		defer inventory.Close()
		artifacts, err := inventory.List()
		if err != nil {
			return err
		}
		for _, artifact := range artifacts {
			if err := store.Verify(artifact.Digest, artifact.Size); err != nil {
				return fmt.Errorf("verify %s: %w", artifact.Digest, err)
			}
			if err := inventory.MarkVerified(artifact.Digest); err != nil {
				return err
			}
		}
		fmt.Printf("ok: %d artifacts verified\n", len(artifacts))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(verifyCmd)
}
