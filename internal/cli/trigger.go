package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/hibira-io/cli/internal/pusher"
)

func (a *app) triggerCmd() *cobra.Command {
	var (
		extra    []string
		socketID string
		asJSON   bool
	)
	cmd := &cobra.Command{
		Use:   "trigger CHANNEL EVENT [DATA]",
		Short: "Publish an event to a channel",
		Long: `Publish an event through the HTTP API, the same call your backend makes.

DATA is sent as is. Pass "-" to read it from stdin. Without DATA the event
carries "{}".`,
		Example: `  hibira trigger orders order.updated '{"id":42,"status":"shipped"}'
  hibira trigger orders order.updated -c dashboard -c audit '{"id":42}'
  cat payload.json | hibira trigger private-user-7 notify -`,
		Args: cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			creds, err := a.credentials(cmd)
			if err != nil {
				return err
			}
			if err := missing(creds, "app-id", "key", "secret"); err != nil {
				return err
			}
			data, err := a.eventData(args)
			if err != nil {
				return err
			}
			channels := append([]string{args[0]}, extra...)
			res, err := pusher.NewClient(creds, a.userAgent()).Trigger(cmd.Context(), pusher.TriggerParams{
				Name: args[1], Channels: channels, Data: data, SocketID: socketID,
			})
			if err != nil {
				return err
			}
			if asJSON {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), string(compact(res)))
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Sent %q to %s (%d bytes)\n", args[1], strings.Join(channels, ", "), len(data))
			return nil
		},
	}
	f := cmd.Flags()
	f.StringArrayVarP(&extra, "channel", "c", nil, "also send to this channel (repeatable)")
	f.StringVar(&socketID, "socket-id", "", "exclude this connection from receiving the event")
	f.BoolVar(&asJSON, "json", false, "print the API response as JSON")
	return cmd
}

func (a *app) eventData(args []string) (string, error) {
	if len(args) < 3 {
		return "{}", nil
	}
	if args[2] != "-" {
		return args[2], nil
	}
	b, err := io.ReadAll(io.LimitReader(a.stdin, 10<<20))
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(b), "\r\n"), nil
}

func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

func compact(b []byte) []byte {
	var buf bytes.Buffer
	if json.Compact(&buf, b) != nil {
		return b
	}
	return buf.Bytes()
}
