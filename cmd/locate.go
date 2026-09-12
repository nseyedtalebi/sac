package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/nseyedtalebi/sac/catalog"
)

var locatorPrefix string

var locateCmd = &cobra.Command{
	Use:   "locate",
	Short: "list cataloged artifacts by observed URI prefix",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if catalogPath == "" {
			return fmt.Errorf("--catalog is required")
		}
		inventory, err := catalog.Open(catalogPath)
		if err != nil {
			return err
		}
		defer inventory.Close()
		artifacts, err := inventory.ListLocators(locatorPrefix)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(artifacts)
	},
}

func init() {
	locateCmd.Flags().StringVar(&locatorPrefix, "prefix", "", "absolute URI prefix to match")
	rootCmd.AddCommand(locateCmd)
}
