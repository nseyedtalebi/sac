package cmd

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/spf13/cobra"

	"sac/cas"
	"sac/lineage"
)

var skipRecord bool

var putCmd = &cobra.Command{
	Use:   "put <file>",
	Short: "write a file into the content-addressed store",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if storeRoot == "" {
			return fmt.Errorf("--store is required")
		}
		if !skipRecord && logPath == "" {
			return fmt.Errorf("--log is required unless --no-record is set")
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

		if skipRecord {
			fmt.Printf("%s  %d bytes  %s\n", digest, size, status)
			return nil
		}

		logStatus, err := recordPut(args[0], digest, size)
		if err != nil {
			return err
		}
		fmt.Printf("%s  %d bytes  %s  %s\n", digest, size, status, logStatus)
		return nil
	},
}

// recordPut writes a lineage event for a put of path (already stored under
// digest/size) and returns "recorded" or "already recorded".
func recordPut(path, digest string, size int64) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	host, _ := os.Hostname()
	actor := "unknown"
	if u, err := user.Current(); err == nil {
		actor = u.Username
	}

	l, err := lineage.Open(logPath)
	if err != nil {
		return "", err
	}
	defer l.Close()

	ev, err := lineage.BuildEvent(
		"put", "sac-cli", "",
		nil,
		[]lineage.Item{{Hash: "sha256:" + digest, ByteSize: size, Locators: []string{"file://" + abs}}},
		actor, host, "",
	)
	if err != nil {
		return "", err
	}
	fresh, err := l.Record(ev)
	if err != nil {
		return "", err
	}
	if fresh {
		return "recorded", nil
	}
	return "already recorded", nil
}

func init() {
	putCmd.Flags().BoolVar(&skipRecord, "no-record", false, "write the blob only, skip the lineage event")
	rootCmd.AddCommand(putCmd)
}
