package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"sac/cas"
	"sac/lineage"
)

var skipCheck bool

var getCmd = &cobra.Command{
	Use:   "get <digest>",
	Short: "stream a stored blob to stdout",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if storeRoot == "" {
			return fmt.Errorf("--store is required")
		}
		digest := args[0]

		if !skipCheck {
			if logPath == "" {
				return fmt.Errorf("--log is required unless --no-check is set")
			}
			l, err := lineage.Open(logPath)
			if err != nil {
				return err
			}
			info, found, err := l.Artifact(digest)
			l.Close()
			if err != nil {
				return err
			}
			if !found {
				return fmt.Errorf("digest %s has no lineage record (use --no-check to fetch anyway)", digest)
			}
			fmt.Fprintf(os.Stderr, "lineage: first seen %s, %d bytes\n", info.FirstSeenUTC, info.ByteSize)
		}

		store, err := cas.Open(storeRoot)
		if err != nil {
			return err
		}
		f, err := store.Get(digest)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(os.Stdout, f)
		return err
	},
}

func init() {
	getCmd.Flags().BoolVar(&skipCheck, "no-check", false, "skip the lineage check, fetch the blob regardless")
	rootCmd.AddCommand(getCmd)
}
