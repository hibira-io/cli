package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/hibira-io/cli/internal/config"
)

func (a *app) configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Save app credentials so you don't have to pass them each time",
	}
	cmd.AddCommand(a.configSetCmd(), a.configShowCmd(), a.configUseCmd(), a.configRemoveCmd(), a.configPathCmd())
	return cmd
}

func (a *app) profileName(f *config.File) string {
	return first(a.profile, a.getenv("HIBIRA_PROFILE"), f.Active())
}

func (a *app) configSetCmd() *cobra.Command {
	var secretStdin bool
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Create or update a profile",
		Long: `Save credentials to a profile ("default" unless you pass --profile).

Only the values you pass are changed. When --secret is not given and you
are at a terminal, you are asked for it without echo; press Enter to keep
the saved one. The file is readable by your user only.`,
		Example: `  hibira config set --app-id 123 --key abc
  hibira config set --profile staging --app-id 456 --key def --host ws-edge.hibira.io
  echo "$SECRET" | hibira config set --profile ci --secret-stdin`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := config.Path()
			if err != nil {
				return err
			}
			f, err := config.Load(path)
			if err != nil {
				return err
			}
			name := a.profileName(f)
			p := f.Profiles[name]
			fl := cmd.Flags()
			set := func(flag string, dst *string, v string) {
				if fl.Changed(flag) {
					*dst = strings.TrimSpace(v)
				}
			}
			set("app-id", &p.AppID, a.appID)
			set("key", &p.Key, a.key)
			set("secret", &p.Secret, a.secret)
			set("host", &p.Host, a.host)
			set("scheme", &p.Scheme, a.scheme)
			switch {
			case secretStdin:
				line, err := bufio.NewReader(a.stdin).ReadString('\n')
				if err != nil && line == "" {
					return errors.New("no secret on stdin")
				}
				p.Secret = strings.TrimSpace(line)
			case !fl.Changed("secret") && isTerminal(a.stdin):
				fmt.Fprint(cmd.ErrOrStderr(), "App secret (Enter to keep the current one): ")
				b, err := term.ReadPassword(int(a.stdin.(*os.File).Fd()))
				fmt.Fprintln(cmd.ErrOrStderr())
				if err != nil {
					return err
				}
				if s := strings.TrimSpace(string(b)); s != "" {
					p.Secret = s
				}
			}
			if p.Scheme != "" && p.Scheme != "https" && p.Scheme != "http" {
				return fmt.Errorf("scheme must be https or http")
			}
			f.Profiles[name] = p
			if f.Default == "" {
				f.Default = name
			}
			if err := config.Save(path, f); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Saved profile %q to %s\n", name, path)
			return nil
		},
	}
	cmd.Flags().BoolVar(&secretStdin, "secret-stdin", false, "read the app secret from stdin")
	return cmd
}

func (a *app) configShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show the credentials that would be used (secret masked)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := config.Path()
			if err != nil {
				return err
			}
			f, err := config.Load(path)
			if err != nil {
				return err
			}
			c, err := a.credentials(cmd)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "config:   %s\n", path)
			if names := f.Names(); len(names) > 0 {
				fmt.Fprintf(out, "profiles: %s (default %s)\n", strings.Join(names, ", "), f.Active())
			}
			fmt.Fprintf(out, "profile:  %s\n", a.profileName(f))
			fmt.Fprintf(out, "app id:   %s\n", orNone(c.AppID))
			fmt.Fprintf(out, "key:      %s\n", orNone(c.Key))
			fmt.Fprintf(out, "secret:   %s\n", mask(c.Secret))
			fmt.Fprintf(out, "endpoint: %s\n", c.BaseURL())
			return nil
		},
	}
}

func (a *app) configUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use PROFILE",
		Short: "Make a profile the default",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := config.Path()
			if err != nil {
				return err
			}
			f, err := config.Load(path)
			if err != nil {
				return err
			}
			if _, ok := f.Profiles[args[0]]; !ok {
				return fmt.Errorf("no profile %q (have: %s)", args[0], strings.Join(f.Names(), ", "))
			}
			f.Default = args[0]
			if err := config.Save(path, f); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Default profile is now %q\n", args[0])
			return nil
		},
	}
}

func (a *app) configRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove PROFILE",
		Short: "Delete a saved profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := config.Path()
			if err != nil {
				return err
			}
			f, err := config.Load(path)
			if err != nil {
				return err
			}
			if _, ok := f.Profiles[args[0]]; !ok {
				return fmt.Errorf("no profile %q", args[0])
			}
			delete(f.Profiles, args[0])
			if f.Default == args[0] {
				f.Default = ""
			}
			if err := config.Save(path, f); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed profile %q\n", args[0])
			return nil
		},
	}
}

func (a *app) configPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print where the config file lives",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := config.Path()
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), path)
			return nil
		},
	}
}

func orNone(s string) string {
	if s == "" {
		return "(not set)"
	}
	return s
}

func mask(s string) string {
	switch {
	case s == "":
		return "(not set)"
	case len(s) <= 4:
		return "****"
	default:
		return "****" + s[len(s)-4:]
	}
}
