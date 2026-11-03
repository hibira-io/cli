package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hibira-io/cli/internal/config"
)

func run(t *testing.T, env map[string]string, stdin string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	a := &app{
		build:  Build{Version: "test", Commit: "x"},
		stdin:  strings.NewReader(stdin),
		stdout: &out,
		stderr: io.Discard,
		getenv: func(k string) string { return env[k] },
	}
	cmd := a.root()
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestCredentialPrecedence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("HIBIRA_CONFIG", path)
	if err := config.Save(path, &config.File{Profiles: map[string]config.Profile{
		"default": {AppID: "from-file", Key: "k-file", Secret: "s-file"},
	}}); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"HIBIRA_APP_KEY": "k-env", "HIBIRA_PORT": "6001"}
	a := &app{getenv: func(k string) string { return env[k] }, secret: "s-flag"}
	c, err := a.credentials(nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.AppID != "from-file" || c.Key != "k-env" || c.Secret != "s-flag" {
		t.Fatalf("got %+v", c)
	}
	if c.Host != config.DefaultHost+":6001" || c.Scheme != "https" {
		t.Fatalf("host/scheme = %s %s", c.Host, c.Scheme)
	}
}

func TestMissingProfileIsAnError(t *testing.T) {
	t.Setenv("HIBIRA_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if _, err := run(t, nil, "", "--profile", "nope", "trigger", "c", "e"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v", err)
	}
}

func TestTriggerSendsSignedEvent(t *testing.T) {
	t.Setenv("HIBIRA_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	var got struct {
		Name     string   `json:"name"`
		Channels []string `json:"channels"`
		Data     string   `json:"data"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apps/42/events" || r.URL.Query().Get("auth_signature") == "" || r.URL.Query().Get("body_md5") == "" {
			http.Error(w, "bad request "+r.URL.String(), http.StatusBadRequest)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()
	env := map[string]string{
		"HIBIRA_APP_ID": "42", "HIBIRA_APP_KEY": "k", "HIBIRA_APP_SECRET": "s",
		"HIBIRA_HOST": strings.TrimPrefix(srv.URL, "http://"), "HIBIRA_SCHEME": "http",
	}
	out, err := run(t, env, `{"from":"stdin"}`+"\n", "trigger", "a", "evt", "-c", "b", "-")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "evt" || strings.Join(got.Channels, ",") != "a,b" || got.Data != `{"from":"stdin"}` {
		t.Fatalf("server got %+v", got)
	}
	if !strings.Contains(out, `Sent "evt" to a, b`) {
		t.Fatalf("output = %q", out)
	}
	if _, err := run(t, env, "", "trigger", "a", "evt"); err != nil || got.Data != "{}" {
		t.Fatalf("default data = %q, err %v", got.Data, err)
	}
}

func TestTriggerNeedsSecret(t *testing.T) {
	t.Setenv("HIBIRA_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	_, err := run(t, map[string]string{"HIBIRA_APP_ID": "1", "HIBIRA_APP_KEY": "k"}, "", "trigger", "a", "b")
	if err == nil || !strings.Contains(err.Error(), "HIBIRA_APP_SECRET") {
		t.Fatalf("err = %v", err)
	}
}
