package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

// hibira approvals: Hibira Approvals workspaces through the customer API
// (hibira.io/api/v1/approvals/...), with a Hibira API token instead of an
// app's key and secret. The token acts as its owner's email.

const defaultEndpoint = "https://hibira.io"

func (a *app) approvalsCmd() *cobra.Command {
	var token, endpoint string
	cmd := &cobra.Command{
		Use:   "approvals",
		Short: "Manage Hibira Approvals workspaces: members, test requests",
		Long: `Manage Hibira Approvals workspaces (approvals.hibira.io) from the terminal.

These commands use a Hibira API token, not an app's key and secret: make a
personal token with read and write access on the portal's API tokens page
and set HIBIRA_API_TOKEN (or pass --token). The token acts as its owner's
email, which must be verified: you see the workspaces that email is in and
change the ones where it is an admin.`,
		Example: `  hibira approvals workspaces
  hibira approvals create "Ops approvals"
  hibira approvals add apw_0123456789abcdef dba@example.com --role approver
  hibira approvals test apw_0123456789abcdef`,
	}
	pf := cmd.PersistentFlags()
	pf.StringVar(&token, "token", "", "Hibira API token (env HIBIRA_API_TOKEN)")
	pf.StringVar(&endpoint, "endpoint", "", "Hibira site (env HIBIRA_ENDPOINT, default "+defaultEndpoint+")")
	api := func() (*approvalsAPI, error) {
		t := first(token, a.getenv("HIBIRA_API_TOKEN"))
		if t == "" {
			return nil, fmt.Errorf("missing --token / HIBIRA_API_TOKEN (a personal API token from the portal's API tokens page)")
		}
		return &approvalsAPI{base: strings.TrimRight(first(endpoint, a.getenv("HIBIRA_ENDPOINT"), defaultEndpoint), "/"), token: t, ua: a.userAgent()}, nil
	}
	var asJSON bool
	cmd.PersistentFlags().BoolVar(&asJSON, "json", false, "print the API response as JSON")

	// print writes the response as JSON, or with text when not --json.
	print := func(cmd *cobra.Command, raw []byte, text func(out io.Writer) error) error {
		if asJSON {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), string(compact(raw)))
			return err
		}
		return text(cmd.OutOrStdout())
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "workspaces",
		Short: "List the workspaces you are in",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := api()
			if err != nil {
				return err
			}
			var out struct {
				Items []workspaceSummary `json:"items"`
			}
			raw, err := c.call(cmd.Context(), http.MethodGet, "/approvals/workspaces", nil, &out)
			if err != nil {
				return err
			}
			return print(cmd, raw, func(w io.Writer) error {
				if len(out.Items) == 0 {
					fmt.Fprintln(cmd.ErrOrStderr(), `No workspaces yet. Make one with: hibira approvals create "Ops approvals"`)
					return nil
				}
				tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
				fmt.Fprintln(tw, "ID\tNAME\tPLAN\tROLE\tWAITING")
				for _, ws := range out.Items {
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\n", ws.ID, ws.Name, ws.Plan, ws.Role, ws.Pending)
				}
				return tw.Flush()
			})
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "create NAME",
		Short: "Make a workspace (you are its admin)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := api()
			if err != nil {
				return err
			}
			var ws workspace
			raw, err := c.call(cmd.Context(), http.MethodPost, "/approvals/workspaces", map[string]string{"name": args[0]}, &ws)
			if err != nil {
				return err
			}
			return print(cmd, raw, func(w io.Writer) error {
				fmt.Fprintf(w, "Made workspace %s (%s), app %s.\n", ws.Name, ws.ID, ws.AppID)
				fmt.Fprintf(w, "Next: hibira approvals add %s someone@example.com --role approver\n", ws.ID)
				return nil
			})
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "members WORKSPACE",
		Short: "List a workspace's members and approver groups",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := api()
			if err != nil {
				return err
			}
			var ws workspace
			raw, err := c.call(cmd.Context(), http.MethodGet, wsPath(args[0]), nil, &ws)
			if err != nil {
				return err
			}
			return print(cmd, raw, func(w io.Writer) error { return ws.write(w) })
		},
	})

	var role string
	add := &cobra.Command{
		Use:   "add WORKSPACE EMAIL",
		Short: "Add a member, or change their role",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if role != "admin" && role != "approver" && role != "viewer" {
				return fmt.Errorf("--role is admin, approver or viewer")
			}
			c, err := api()
			if err != nil {
				return err
			}
			var ws workspace
			raw, err := c.call(cmd.Context(), http.MethodPut, wsPath(args[0])+"/members", map[string]string{"email": args[1], "role": role}, &ws)
			if err != nil {
				return err
			}
			return print(cmd, raw, func(w io.Writer) error {
				fmt.Fprintf(w, "%s is now %s in %s. They sign in at approvals.hibira.io with that email.\n", args[1], aOrAn(role), ws.Name)
				return nil
			})
		},
	}
	add.Flags().StringVar(&role, "role", "approver", "admin, approver or viewer")
	cmd.AddCommand(add)

	cmd.AddCommand(&cobra.Command{
		Use:   "remove WORKSPACE EMAIL",
		Short: "Remove a member",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := api()
			if err != nil {
				return err
			}
			if _, err := c.call(cmd.Context(), http.MethodDelete, wsPath(args[0])+"/members/"+url.PathEscape(args[1]), nil, nil); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed %s.\n", args[1])
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "test WORKSPACE",
		Short: "Send yourself a sample request, to see one arrive and answer it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := api()
			if err != nil {
				return err
			}
			var r request
			raw, err := c.call(cmd.Context(), http.MethodPost, wsPath(args[0])+"/test", nil, &r)
			if err != nil {
				return err
			}
			return print(cmd, raw, func(w io.Writer) error {
				fmt.Fprintf(w, "Sent %s: %s\n", r.ID, r.Title)
				if r.InboxURL != "" {
					fmt.Fprintf(w, "Answer it on your phone or at %s\n", r.InboxURL)
				}
				return nil
			})
		},
	})

	var history bool
	reqs := &cobra.Command{
		Use:   "requests WORKSPACE",
		Short: "List waiting requests (or decided ones with --history)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := api()
			if err != nil {
				return err
			}
			status := "pending"
			if history {
				status = "history"
			}
			var out struct {
				Items []request `json:"items"`
			}
			raw, err := c.call(cmd.Context(), http.MethodGet, wsPath(args[0])+"/requests?status="+status, nil, &out)
			if err != nil {
				return err
			}
			return print(cmd, raw, func(w io.Writer) error {
				if len(out.Items) == 0 {
					fmt.Fprintln(cmd.ErrOrStderr(), "Nothing here.")
					return nil
				}
				tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
				fmt.Fprintln(tw, "ID\tSTATUS\tCREATED\tTITLE")
				for _, r := range out.Items {
					status := r.Status
					if r.AnsweredBy != nil && r.AnsweredBy.Email != "" {
						status += " by " + r.AnsweredBy.Email
					}
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.ID, status, r.Created.Local().Format("2006-01-02 15:04"), r.Title)
				}
				return tw.Flush()
			})
		},
	}
	reqs.Flags().BoolVar(&history, "history", false, "decided requests instead of waiting ones")
	cmd.AddCommand(reqs)
	return cmd
}

func wsPath(id string) string { return "/approvals/workspaces/" + url.PathEscape(id) }

func aOrAn(role string) string {
	return map[string]string{"admin": "an admin", "approver": "an approver", "viewer": "a viewer"}[role]
}

type workspaceSummary struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Plan    string `json:"plan"`
	Role    string `json:"role"`
	Pending int    `json:"pending"`
}

type workspace struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Plan    string `json:"plan"`
	AppID   string `json:"app_id"`
	Members []struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	} `json:"members"`
	Groups []struct {
		Slug    string   `json:"slug"`
		Name    string   `json:"name"`
		Members []string `json:"members"`
	} `json:"groups"`
}

func (ws workspace) write(w io.Writer) error {
	fmt.Fprintf(w, "%s (%s, %s plan)\n\n", ws.Name, ws.ID, ws.Plan)
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "EMAIL\tROLE")
	for _, m := range ws.Members {
		fmt.Fprintf(tw, "%s\t%s\n", m.Email, m.Role)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(ws.Groups) > 0 {
		fmt.Fprintln(w)
		tw = tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "GROUP\tNAME\tMEMBERS")
		for _, g := range ws.Groups {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", g.Slug, g.Name, strings.Join(g.Members, ", "))
		}
		return tw.Flush()
	}
	return nil
}

type request struct {
	ID         string    `json:"approval_id"`
	Title      string    `json:"title"`
	Status     string    `json:"status"`
	Created    time.Time `json:"created_at"`
	InboxURL   string    `json:"inbox_url"`
	AnsweredBy *struct {
		Email string `json:"email"`
	} `json:"answered_by"`
}

// approvalsAPI calls the customer API with an API token.
type approvalsAPI struct {
	base, token, ua string
}

func (c *approvalsAPI) call(ctx context.Context, method, path string, in, out any) ([]byte, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, c.base+"/api/v1"+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "en")
	req.Header.Set("User-Agent", c.ua)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		msg := http.StatusText(res.StatusCode)
		if json.Unmarshal(raw, &e) == nil && e.Error != "" {
			msg = e.Error
		}
		if res.StatusCode == http.StatusUnauthorized {
			msg += " (check HIBIRA_API_TOKEN: a personal token from the portal's API tokens page)"
		}
		return nil, fmt.Errorf("%s (HTTP %d)", msg, res.StatusCode)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return nil, fmt.Errorf("unexpected response: %w", err)
		}
	}
	return raw, nil
}
