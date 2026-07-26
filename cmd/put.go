package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"sac/cas"
)

var putCmd = &cobra.Command{
	Use:   "put <file>",
	Short: "write a file into the content-addressed store",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if storeRoot == "" {
			return fmt.Errorf("--store is required")
		}
		store, err := cas.Open(storeRoot)
		if err != nil {
			return err
		}
		digest, size, deduped, err := store.WriteFile(args[0])
		if err != nil {
			return err
		}
		status := "stored"
		if deduped {
			status = "deduped"
		}
		fmt.Printf("%s  %d bytes  %s\n", digest, size, status)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(putCmd)
}
