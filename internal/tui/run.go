package tui

import (
	"context"
	"encoding/json"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hibira-io/cli/internal/pusher"
)

// RunOptions configure Run.
type RunOptions struct {
	Creds    pusher.Credentials
	Channels []string
	UserID   string
	UserInfo json.RawMessage
	Internal bool
	Version  string
	AppLabel string
	CanSend  bool
	SendHint string
	Client   *pusher.Client
}

// Run starts the full-screen UI and blocks until the user quits.
func Run(ctx context.Context, o RunOptions) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	control := make(chan pusher.Control, 64)
	m := NewModel(Options{
		AppLabel: o.AppLabel,
		Channels: o.Channels,
		CanSend:  o.CanSend,
		SendHint: o.SendHint,
		Control:  control,
		Trigger: func(ctx context.Context, p pusher.TriggerParams) error {
			_, err := o.Client.Trigger(ctx, p)
			return err
		},
	})
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx))
	go func() {
		err := pusher.Listen(ctx, pusher.ListenOptions{
			Creds:        o.Creds,
			Channels:     o.Channels,
			UserID:       o.UserID,
			UserInfo:     o.UserInfo,
			ShowInternal: o.Internal,
			Reconnect:    true,
			Client:       "hibira-cli",
			Version:      o.Version,
			Control:      control,
			OnMessage:    func(e pusher.Message) { p.Send(eventMsg(e)) },
			OnStatus:     func(s pusher.Status) { p.Send(statusMsg(s)) },
		})
		p.Send(listenEnded{err: err})
	}()
	_, err := p.Run()
	if err == tea.ErrProgramKilled && ctx.Err() != nil {
		return nil
	}
	return err
}
