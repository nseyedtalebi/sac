package cmd

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"sac/cas"
	"sac/lineage"
)

var (
	skipRecord bool
	runKind    string
	inputs     []string
)

var putCmd = &cobra.Command{
	Use:   "put <file>",
	Short: "write a file into the content-addressed store",
	Long: `Write a file into the content-addressed store and, by default, record
a lineage event for it. With no --input flags this is a plain ingest
(an event with no inputs). Pass --input for each existing artifact
this file was derived from to record a transform instead — --run-kind
is just a label, the event shape is identical either way.`,
	Args: cobra.ExactArgs(1),
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

		var inputItems []lineage.Item
		if !skipRecord {
			for _, in := range inputs {
				bare := strings.TrimPrefix(in, "sha256:")
				if !store.Has(bare) {
					return fmt.Errorf("--input %s not found in the store", in)
				}
				size, err := store.Size(bare)
				if err != nil {
					return err
				}
				inputItems = append(inputItems, lineage.Item{Hash: "sha256:" + bare, ByteSize: size})
			}
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

		logStatus, err := recordPut(args[0], digest, size, runKind, inputItems)
		if err != nil {
			return err
		}
		fmt.Printf("%s  %d bytes  %s  %s\n", digest, size, status, logStatus)
		return nil
	},
}

// recordPut writes a lineage event for a put of path (already stored under
// digest/size) and returns "recorded" or "already recorded".
func recordPut(path, digest string, size int64, runKind string, inputItems []lineage.Item) (string, error) {
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
		runKind, "sac-cli", "",
		inputItems,
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
	putCmd.Flags().StringVar(&runKind, "run-kind", "put", "lineage event label, e.g. \"transform\"")
	putCmd.Flags().StringArrayVar(&inputs, "input", nil, "hash of an existing artifact this file was derived from (repeatable)")
	rootCmd.AddCommand(putCmd)
}
