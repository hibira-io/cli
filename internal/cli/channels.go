package cli

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/hibira-io/cli/internal/pusher"
)

func (a *app) channelsCmd() *cobra.Command {
	var (
		prefix string
		info   string
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "channels",
		Short: "List channels that have subscribers right now",
		Example: `  hibira channels
  hibira channels --prefix presence- --info user_count`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			creds, err := a.credentials(cmd)
			if err != nil {
				return err
			}
			if err := missing(creds, "app-id", "key", "secret"); err != nil {
				return err
			}
			res, err := pusher.NewClient(creds, a.userAgent()).Channels(cmd.Context(), prefix, info)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				_, err = fmt.Fprintln(out, string(compact(res)))
				return err
			}
			var body struct {
				Channels map[string]map[string]any `json:"channels"`
			}
			if err := json.Unmarshal(res, &body); err != nil {
				return fmt.Errorf("unexpected response: %w", err)
			}
			if len(body.Channels) == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "No occupied channels.")
				return nil
			}
			names := make([]string, 0, len(body.Channels))
			for n := range body.Channels {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				line := n
				attrs := body.Channels[n]
				keys := make([]string, 0, len(attrs))
				for k := range attrs {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					line += fmt.Sprintf("  %s=%v", k, attrs[k])
				}
				fmt.Fprintln(out, line)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&prefix, "prefix", "", "only channels whose name starts with this")
	f.StringVar(&info, "info", "", "extra attributes, such as user_count (presence only)")
	f.BoolVar(&asJSON, "json", false, "print the API response as JSON")
	return cmd
}
