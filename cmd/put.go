package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nseyedtalebi/sac/cas"
	"github.com/nseyedtalebi/sac/catalog"
)

var putLocators []string

var putCmd = &cobra.Command{
	Use:   "put <file>",
	Short: "write a file into the content-addressed store",
	Long:  "Write a file into the content-addressed store and record it in the SQLite inventory.",
	Args:  cobra.ExactArgs(1),
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

		digest, size, deduped, err := store.WriteFile(args[0])
		if err != nil {
			return err
		}
		status := "stored"
		if deduped {
			status = "deduped"
		}

		fresh, err := inventory.Record(digest, size, putLocators...)
		if err != nil {
			return err
		}
		catalogStatus := "already cataloged"
		if fresh {
			catalogStatus = "cataloged"
		}
		fmt.Printf("%s  %d bytes  %s  %s\n", digest, size, status, catalogStatus)
		return nil
	},
}

func init() {
	putCmd.Flags().StringArrayVar(&putLocators, "locator", nil, "observed absolute URI for this artifact (repeatable)")
	rootCmd.AddCommand(putCmd)
}
