package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/hibira-io/cli/internal/pusher"
	"github.com/hibira-io/cli/internal/tui"
)

func (a *app) tuiCmd() *cobra.Command {
	var (
		userID   string
		userInfo string
		internal bool
	)
	cmd := &cobra.Command{
		Use:     "tui [CHANNEL...]",
		Aliases: []string{"ui"},
		Short:   "Open the interactive view: watch channels and publish events",
		Long: `Open a full-screen view with your channels on the left, events streaming
in on the right and the selected event's data below. Press a to subscribe,
t to publish, enter to show one channel, ? for all keys.

Running hibira with no command in a terminal opens this view too.`,
		Example: `  hibira tui
  hibira tui orders private-user-7 presence-room-1 --user-id alice`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runTUI(cmd, args, userID, userInfo, internal)
		},
	}
	f := cmd.Flags()
	f.StringVar(&userID, "user-id", "", "user id for presence channels (default hibira-cli)")
	f.StringVar(&userInfo, "user-info", "", "user info JSON for presence channels")
	f.BoolVar(&internal, "internal", false, "also show pusher:* protocol events (member_added, ...)")
	return cmd
}

func (a *app) runTUI(cmd *cobra.Command, channels []string, userID, userInfo string, internal bool) error {
	if !a.interactive() {
		return errors.New("the interactive view needs a terminal; use hibira listen or hibira trigger in scripts")
	}
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
	for _, ch := range channels {
		if err := pusher.CheckChannel(creds, ch); err != nil {
			return err
		}
	}
	label := creds.Host
	if creds.AppID != "" {
		label = "app " + creds.AppID + " · " + creds.Host
	}
	hint := ""
	if err := missing(creds, "app-id", "secret"); err != nil {
		hint = "Publishing needs the app id and secret: " + err.Error()
	}
	return tui.Run(cmd.Context(), tui.RunOptions{
		Creds:    creds,
		Channels: channels,
		UserID:   userID,
		UserInfo: info,
		Internal: internal,
		Version:  a.build.Version,
		AppLabel: label,
		CanSend:  hint == "",
		SendHint: hint,
		Client:   pusher.NewClient(creds, a.userAgent()),
	})
}

func (a *app) interactive() bool {
	out, ok := a.stdout.(*os.File)
	return ok && isTerminal(a.stdin) && term.IsTerminal(int(out.Fd()))
}
