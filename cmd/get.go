package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"sac/cas"
)

var getCmd = &cobra.Command{
	Use:   "get <digest>",
	Short: "stream a stored blob to stdout",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if storeRoot == "" {
			return fmt.Errorf("--store is required")
		}
		store, err := cas.Open(storeRoot)
		if err != nil {
			return err
		}
		f, err := store.Get(args[0])
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(os.Stdout, f)
		return err
	},
}

func init() {
	rootCmd.AddCommand(getCmd)
}
