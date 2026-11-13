package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestApprovalsCommands(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer hkb_pat_x" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"Please sign in."}`))
			return
		}
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/approvals/workspaces":
			_, _ = w.Write([]byte(`{"items":[{"id":"apw_1","name":"Ops","plan":"free","role":"admin","pending":2}]}`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/approvals/workspaces/apw_1/members":
			var in map[string]string
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in["email"] != "dba@example.com" || in["role"] != "viewer" {
				t.Errorf("add %v", in)
			}
			_, _ = w.Write([]byte(`{"id":"apw_1","name":"Ops"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/approvals/workspaces/apw_1/test":
			_, _ = w.Write([]byte(`{"approval_id":"apr_1","title":"Test request","status":"pending","inbox_url":"https://approvals.hibira.io/inbox/apr_1"}`))
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	env := map[string]string{"HIBIRA_API_TOKEN": "hkb_pat_x", "HIBIRA_ENDPOINT": srv.URL}

	out, err := run(t, env, "", "approvals", "workspaces")
	if err != nil || !strings.Contains(out, "apw_1") || !strings.Contains(out, "Ops") {
		t.Fatalf("workspaces %q %v", out, err)
	}
	if out, err = run(t, env, "", "approvals", "add", "apw_1", "dba@example.com", "--role", "viewer"); err != nil || !strings.Contains(out, "a viewer") {
		t.Fatalf("add %q %v", out, err)
	}
	if out, err = run(t, env, "", "approvals", "test", "apw_1"); err != nil || !strings.Contains(out, "approvals.hibira.io/inbox/apr_1") {
		t.Fatalf("test %q %v", out, err)
	}
	if _, err = run(t, env, "", "approvals", "remove", "apw_1", "dba@example.com"); err != nil {
		t.Fatal(err)
	}
	if calls[len(calls)-1] != "DELETE /api/v1/approvals/workspaces/apw_1/members/dba@example.com" {
		t.Fatalf("remove call %v", calls)
	}
	if out, err = run(t, env, "", "approvals", "workspaces", "--json"); err != nil || !strings.HasPrefix(out, `{"items":`) {
		t.Fatalf("json %q %v", out, err)
	}
	if _, err = run(t, map[string]string{"HIBIRA_ENDPOINT": srv.URL}, "", "approvals", "workspaces"); err == nil || !strings.Contains(err.Error(), "HIBIRA_API_TOKEN") {
		t.Fatalf("no token %v", err)
	}
	if _, err = run(t, env, "", "approvals", "add", "apw_1", "x@example.com", "--role", "boss"); err == nil {
		t.Fatal("bad role accepted")
	}
}
