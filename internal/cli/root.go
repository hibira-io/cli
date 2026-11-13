// Package cli wires the hibira commands together.
package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hibira-io/cli/internal/config"
	"github.com/hibira-io/cli/internal/pusher"
)

// Build information, set by main.
type Build struct {
	Version string
	Commit  string
}

type app struct {
	build  Build
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	getenv func(string) string

	profile string
	appID   string
	key     string
	secret  string
	host    string
	scheme  string
}

// New returns the root command.
func New(b Build) *cobra.Command {
	a := &app{build: b, stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr, getenv: os.Getenv}
	return a.root()
}

func (a *app) root() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hibira",
		Short: "Send and watch realtime events on Hibira from your terminal",
		Long: `hibira is a small debugging tool for Hibira apps.

  hibira trigger orders order.updated '{"id":42}'   publish an event
  hibira listen orders                              print events as they arrive
  hibira                                            interactive view (same as hibira tui)

Credentials come from flags, then environment variables (HIBIRA_APP_ID,
HIBIRA_APP_KEY, HIBIRA_APP_SECRET, HIBIRA_HOST), then the profile saved
with "hibira config set". Find them on your app's page in the Hibira portal.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.SetIn(a.stdin)
	cmd.SetOut(a.stdout)
	cmd.SetErr(a.stderr)
	pf := cmd.PersistentFlags()
	pf.StringVarP(&a.profile, "profile", "p", "", "saved profile to use (env HIBIRA_PROFILE)")
	pf.StringVar(&a.appID, "app-id", "", "app ID (env HIBIRA_APP_ID)")
	pf.StringVar(&a.key, "key", "", "app key (env HIBIRA_APP_KEY)")
	pf.StringVar(&a.secret, "secret", "", "app secret (env HIBIRA_APP_SECRET; prefer the env var or a profile, flags end up in shell history)")
	pf.StringVar(&a.host, "host", "", "API and WebSocket host, with optional :port (env HIBIRA_HOST, default "+config.DefaultHost+")")
	pf.StringVar(&a.scheme, "scheme", "", "https or http (env HIBIRA_SCHEME, default https)")
	cmd.AddCommand(a.tuiCmd(), a.triggerCmd(), a.listenCmd(), a.channelsCmd(), a.configCmd(), a.approvalsCmd(), a.versionCmd())
	cmd.Args = cobra.NoArgs
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if !a.interactive() {
			return cmd.Help()
		}
		if c, err := a.credentials(cmd); err != nil || c.Key == "" {
			fmt.Fprintln(a.stderr, "No app key yet. Save your credentials with \"hibira config set\", or see hibira --help.")
			return nil
		}
		return a.runTUI(cmd, nil, "", "", false)
	}
	return cmd
}

func (a *app) userAgent() string {
	return "hibira-cli/" + a.build.Version
}

// credentials resolves each field from flag, then env, then profile.
func (a *app) credentials(cmd *cobra.Command) (pusher.Credentials, error) {
	path, err := config.Path()
	if err != nil {
		return pusher.Credentials{}, err
	}
	f, err := config.Load(path)
	if err != nil {
		return pusher.Credentials{}, err
	}
	name := first(a.profile, a.getenv("HIBIRA_PROFILE"))
	explicit := name != ""
	if name == "" {
		name = f.Active()
	}
	p, ok := f.Profiles[name]
	if explicit && !ok {
		return pusher.Credentials{}, fmt.Errorf("profile %q not found in %s", name, path)
	}
	c := pusher.Credentials{
		AppID:  first(a.appID, a.getenv("HIBIRA_APP_ID"), p.AppID),
		Key:    first(a.key, a.getenv("HIBIRA_APP_KEY"), p.Key),
		Secret: first(a.secret, a.getenv("HIBIRA_APP_SECRET"), p.Secret),
		Host:   first(a.host, a.getenv("HIBIRA_HOST"), p.Host, config.DefaultHost),
		Scheme: strings.ToLower(first(a.scheme, a.getenv("HIBIRA_SCHEME"), p.Scheme, "https")),
	}
	if port := a.getenv("HIBIRA_PORT"); port != "" && a.host == "" && !strings.Contains(c.Host, ":") {
		if !(c.Scheme == "https" && port == "443") && !(c.Scheme == "http" && port == "80") {
			c.Host += ":" + port
		}
	}
	c.Host = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(c.Host, "https://"), "http://"), "/")
	if c.Scheme != "https" && c.Scheme != "http" {
		return c, fmt.Errorf("scheme must be https or http, not %q", c.Scheme)
	}
	return c, nil
}

func first(vals ...string) string {
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

func missing(c pusher.Credentials, fields ...string) error {
	var out []string
	for _, f := range fields {
		switch {
		case f == "app-id" && c.AppID == "":
			out = append(out, "--app-id / HIBIRA_APP_ID")
		case f == "key" && c.Key == "":
			out = append(out, "--key / HIBIRA_APP_KEY")
		case f == "secret" && c.Secret == "":
			out = append(out, "--secret / HIBIRA_APP_SECRET")
		}
	}
	if len(out) == 0 {
		return nil
	}
	return fmt.Errorf("missing %s (or save them once with \"hibira config set\")", strings.Join(out, ", "))
}

func (a *app) versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the CLI version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "hibira %s (%s)\n", a.build.Version, a.build.Commit)
		},
	}
}
