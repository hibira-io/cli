package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/hibira-io/cli/internal/pusher"
)

func (a *app) listenCmd() *cobra.Command {
	var (
		userID   string
		userInfo string
		internal bool
		noRetry  bool
		asJSON   bool
		pretty   bool
		quiet    bool
	)
	cmd := &cobra.Command{
		Use:   "listen CHANNEL [CHANNEL...]",
		Short: "Subscribe to channels and print events as they arrive",
		Long: `Open a WebSocket connection like a browser would, subscribe to the
channels and print every event until you press Ctrl+C.

Public channels only need the app key. Private and presence channels are
signed locally with the app secret, so you don't need your auth endpoint.`,
		Example: `  hibira listen orders
  hibira listen private-user-7 presence-room-1 --user-id alice
  hibira listen orders --json | jq .data`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			creds, err := a.credentials(cmd)
			if err != nil {
				return err
			}
			if err := missing(creds, "key"); err != nil {
				return err
			}
			var info json.RawMessage
			if userInfo != "" {
				if !json.Valid([]byte(userInfo)) {
					return fmt.Errorf("--user-info must be JSON, such as '{\"name\":\"Alice\"}'")
				}
				info = json.RawMessage(userInfo)
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			out, errw := cmd.OutOrStdout(), cmd.ErrOrStderr()
			enc := json.NewEncoder(out)
			enc.SetEscapeHTML(false)
			return pusher.Listen(ctx, pusher.ListenOptions{
				Creds:        creds,
				Channels:     args,
				UserID:       userID,
				UserInfo:     info,
				ShowInternal: internal,
				Reconnect:    !noRetry,
				Client:       "hibira-cli",
				Version:      a.build.Version,
				OnStatus: func(s pusher.Status) {
					if !quiet {
						fmt.Fprintln(errw, "· "+s.Text)
					}
				},
				OnMessage: func(m pusher.Message) {
					if asJSON {
						_ = enc.Encode(m)
						return
					}
					data := m.Data
					if pretty {
						var buf bytes.Buffer
						if json.Indent(&buf, []byte(data), "  ", "  ") == nil {
							data = "\n  " + buf.String()
						}
					}
					ch := m.Channel
					if ch == "" {
						ch = "-"
					}
					fmt.Fprintf(out, "%s  %s  %s  %s\n", m.Time.Format("15:04:05.000"), ch, m.Event, data)
				},
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&userID, "user-id", "", "user id for presence channels (default hibira-cli)")
	f.StringVar(&userInfo, "user-info", "", "user info JSON for presence channels")
	f.BoolVar(&internal, "internal", false, "also print pusher:* protocol events (member_added, ...)")
	f.BoolVar(&noRetry, "no-reconnect", false, "exit instead of reconnecting when the connection drops")
	f.BoolVar(&asJSON, "json", false, "print one JSON object per event (for jq and scripts)")
	f.BoolVar(&pretty, "pretty", false, "indent JSON event data")
	f.BoolVarP(&quiet, "quiet", "q", false, "hide connection status lines")
	return cmd
}
