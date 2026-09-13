package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/nseyedtalebi/sac/cas"
	"github.com/nseyedtalebi/sac/catalog"
)

var pruneMissingApply bool

var pruneMissingCmd = &cobra.Command{
	Use:   "prune-missing",
	Short: "remove catalog entries whose blobs are absent",
	Args:  cobra.NoArgs,
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

		missing := make([]catalog.Artifact, 0)
		for _, artifact := range artifacts {
			if _, err := os.Stat(store.Path(artifact.Digest)); err == nil {
				continue
			} else if errors.Is(err, os.ErrNotExist) {
				missing = append(missing, artifact)
			} else {
				return fmt.Errorf("check %s: %w", artifact.Digest, err)
			}
		}
		if !pruneMissingApply {
			fmt.Printf("dry-run: %d missing artifacts; rerun with --apply to remove catalog entries\n", len(missing))
			return nil
		}

		// ponytail: do not run concurrently with put; add a store-wide maintenance lock if that becomes necessary.
		removed := 0
		for _, artifact := range missing {
			if _, err := os.Stat(store.Path(artifact.Digest)); err == nil {
				continue
			} else if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("recheck %s: %w", artifact.Digest, err)
			}
			if err := inventory.Remove(artifact.Digest); err != nil {
				return fmt.Errorf("remove %s: %w", artifact.Digest, err)
			}
			removed++
		}
		fmt.Printf("ok: %d missing artifacts removed\n", removed)
		return nil
	},
}

func init() {
	pruneMissingCmd.Flags().BoolVar(&pruneMissingApply, "apply", false, "remove missing artifact entries from the catalog")
	rootCmd.AddCommand(pruneMissingCmd)
}
