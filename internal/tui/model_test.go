package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hibira-io/cli/internal/pusher"
)

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func step(t *testing.T, m Model, msgs ...tea.Msg) (Model, []tea.Cmd) {
	t.Helper()
	var cmds []tea.Cmd
	for _, msg := range msgs {
		next, cmd := m.Update(msg)
		m = next.(Model)
		cmds = append(cmds, cmd)
	}
	return m, cmds
}

func typeText(s string) []tea.Msg {
	var out []tea.Msg
	for _, r := range s {
		out = append(out, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return out
}

func TestEventsAndFilter(t *testing.T) {
	m := NewModel(Options{Channels: []string{"orders", "chat"}})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30},
		statusMsg{Kind: pusher.StatusConnected, SocketID: "1.2"},
		statusMsg{Kind: pusher.StatusSubscribed, Channel: "orders"},
		eventMsg{Time: time.Now(), Channel: "orders", Event: "order.created", Data: `{"id":1}`},
		eventMsg{Time: time.Now(), Channel: "chat", Event: "message", Data: `{"text":"hi"}`},
	)
	if m.channels[0].count != 1 || m.channels[0].state != chanSubscribed || m.channels[1].state != chanPending {
		t.Fatalf("channels = %+v", m.channels)
	}
	if m.evCursor != 1 || !m.follow {
		t.Fatalf("cursor %d follow %v", m.evCursor, m.follow)
	}
	view := m.View()
	for _, want := range []string{"connected", "order.created", "message", `"text": "hi"`} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	m, _ = step(t, m, key("tab"), key("enter"))
	if m.filter != "orders" || len(m.visible()) != 1 {
		t.Fatalf("filter %q visible %d", m.filter, len(m.visible()))
	}
	m, _ = step(t, m, key("esc"))
	if m.filter != "" {
		t.Fatal("esc should clear the filter")
	}
}

func TestSubscribeAndUnsubscribe(t *testing.T) {
	ctl := make(chan pusher.Control, 4)
	m := NewModel(Options{Control: ctl})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30}, key("a"))
	if m.mode != modeAdd {
		t.Fatal("a should open the subscribe form")
	}
	msgs := append(typeText("orders"), key("enter"))
	m, cmds := step(t, m, msgs...)
	runCmd(cmds[len(cmds)-1])
	if got := <-ctl; got.Channel != "orders" || got.Unsubscribe {
		t.Fatalf("control = %+v", got)
	}
	if len(m.channels) != 1 || m.mode != modeNormal {
		t.Fatalf("channels %+v mode %v", m.channels, m.mode)
	}
	m, cmds = step(t, m, key("x"))
	runCmd(cmds[0])
	if got := <-ctl; got.Channel != "orders" || !got.Unsubscribe {
		t.Fatalf("control = %+v", got)
	}
	if len(m.channels) != 0 {
		t.Fatal("channel not removed")
	}
}

func TestPublishForm(t *testing.T) {
	var sent pusher.TriggerParams
	m := NewModel(Options{
		Channels: []string{"orders"},
		CanSend:  true,
		Trigger: func(_ context.Context, p pusher.TriggerParams) error {
			sent = p
			return nil
		},
	})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30}, key("t"))
	if m.mode != modeTrigger || m.trigIn[0].Value() != "orders" || m.trigField != 1 {
		t.Fatalf("mode %v channel %q field %d", m.mode, m.trigIn[0].Value(), m.trigField)
	}
	msgs := append(typeText("order.updated"), key("enter"))
	m, cmds := step(t, m, msgs...)
	res := cmds[len(cmds)-1]()
	m, _ = step(t, m, res)
	if sent.Name != "order.updated" || sent.Channels[0] != "orders" || sent.Data != "{}" {
		t.Fatalf("sent %+v", sent)
	}
	if m.mode != modeNormal || !strings.Contains(m.flash, "Sent") {
		t.Fatalf("mode %v flash %q", m.mode, m.flash)
	}
}

func TestPublishNeedsSecret(t *testing.T) {
	m := NewModel(Options{SendHint: "need secret"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24}, key("t"))
	if m.mode != modeNormal || m.flash != "need secret" {
		t.Fatalf("mode %v flash %q", m.mode, m.flash)
	}
}

func TestViewFitsSmallTerminal(t *testing.T) {
	m := NewModel(Options{Channels: []string{"a-very-long-channel-name-indeed"}})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 60, Height: 16},
		eventMsg{Time: time.Now(), Channel: "a-very-long-channel-name-indeed", Event: "e", Data: strings.Repeat(`{"k":"v"}`, 30)})
	lines := strings.Split(m.View(), "\n")
	if len(lines) > 16 {
		t.Fatalf("view is %d lines tall, want <= 16", len(lines))
	}
}

// runCmd runs a command and any commands batched inside it, skipping
// timers (flash clearing) so tests don't wait.
func runCmd(c tea.Cmd) {
	if c == nil {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- c() }()
	select {
	case msg := <-done:
		if b, ok := msg.(tea.BatchMsg); ok {
			for _, sub := range b {
				runCmd(sub)
			}
		}
	case <-time.After(100 * time.Millisecond):
	}
}
