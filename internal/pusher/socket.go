package pusher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// Message is one frame received on the socket, with its data unwrapped
// from the JSON string Pusher sends it as.
type Message struct {
	Time    time.Time `json:"time"`
	Channel string    `json:"channel,omitempty"`
	Event   string    `json:"event"`
	Data    string    `json:"data"`
	UserID  string    `json:"user_id,omitempty"`
}

// ListenOptions configures a subscriber.
type ListenOptions struct {
	Creds    Credentials
	Channels []string
	// UserID and UserInfo build channel_data for presence channels.
	UserID   string
	UserInfo json.RawMessage
	// ShowInternal also delivers pusher:* and pusher_internal:* frames.
	ShowInternal bool
	// Reconnect keeps reconnecting after a drop until ctx is done.
	Reconnect bool
	Client    string // client name sent in the URL
	Version   string
	// OnMessage is called for every delivered message.
	OnMessage func(Message)
	// OnStatus receives connection status lines (connected, subscribed, ...).
	OnStatus func(string)
}

type frame struct {
	Event   string          `json:"event"`
	Channel string          `json:"channel,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	UserID  string          `json:"user_id,omitempty"`
}

// fatalError marks errors that a reconnect would not fix (bad key, app
// disabled, auth failure).
type fatalError struct{ msg string }

func (e *fatalError) Error() string { return e.msg }

func fatalf(format string, args ...any) error {
	return &fatalError{msg: fmt.Sprintf(format, args...)}
}

// Listen connects, subscribes and delivers messages until ctx is done or a
// fatal error happens.
func Listen(ctx context.Context, o ListenOptions) error {
	if o.Creds.Key == "" {
		return errors.New("app key is required to listen")
	}
	if len(o.Channels) == 0 {
		return errors.New("at least one channel is required")
	}
	for _, ch := range o.Channels {
		if strings.HasPrefix(ch, "private-encrypted-") {
			return fmt.Errorf("%s: encrypted channels are not supported by the CLI yet", ch)
		}
		if needsAuth(ch) && o.Creds.Secret == "" {
			return fmt.Errorf("%s: the app secret is needed to subscribe to private and presence channels", ch)
		}
	}
	backoff := time.Second
	for {
		started := time.Now()
		err := listenOnce(ctx, o)
		if ctx.Err() != nil {
			return nil
		}
		var fe *fatalError
		if errors.As(err, &fe) || !o.Reconnect {
			return err
		}
		if time.Since(started) > 30*time.Second {
			backoff = time.Second
		}
		status(o, fmt.Sprintf("disconnected (%v), reconnecting in %s", err, backoff))
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

func needsAuth(ch string) bool {
	return strings.HasPrefix(ch, "private-") || strings.HasPrefix(ch, "presence-")
}

// SocketURL is the WebSocket endpoint for the app key.
func SocketURL(c Credentials, client, version string) string {
	scheme := "wss"
	if c.Scheme == "http" {
		scheme = "ws"
	}
	q := url.Values{}
	q.Set("protocol", "7")
	q.Set("client", client)
	q.Set("version", version)
	q.Set("flash", "false")
	return scheme + "://" + c.Host + "/app/" + url.PathEscape(c.Key) + "?" + q.Encode()
}

func status(o ListenOptions, s string) {
	if o.OnStatus != nil {
		o.OnStatus(s)
	}
}

func listenOnce(ctx context.Context, o ListenOptions) error {
	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	conn, _, err := websocket.Dial(dialCtx, SocketURL(o.Creds, o.Client, o.Version), nil)
	cancel()
	if err != nil {
		return err
	}
	conn.SetReadLimit(1 << 20)
	defer conn.CloseNow()

	ctx, stop := context.WithCancel(ctx)
	defer stop()

	activity := 120 * time.Second
	write := func(f frame) error {
		b, err := json.Marshal(f)
		if err != nil {
			return err
		}
		wctx, c := context.WithTimeout(ctx, 10*time.Second)
		defer c()
		return conn.Write(wctx, websocket.MessageText, b)
	}

	for {
		rctx, c := context.WithTimeout(ctx, activity+30*time.Second)
		typ, b, err := readWithPing(rctx, conn, activity, write)
		c()
		if err != nil {
			if code := websocket.CloseStatus(err); code >= 4000 && code < 4100 {
				return fatalf("server closed the connection with code %d", code)
			}
			return err
		}
		if typ != websocket.MessageText {
			continue
		}
		var f frame
		if err := json.Unmarshal(b, &f); err != nil {
			continue
		}
		data := unwrapData(f.Data)
		switch f.Event {
		case "pusher:connection_established":
			var est struct {
				SocketID        string `json:"socket_id"`
				ActivityTimeout int    `json:"activity_timeout"`
			}
			_ = json.Unmarshal([]byte(data), &est)
			if est.ActivityTimeout > 0 {
				activity = time.Duration(est.ActivityTimeout) * time.Second
			}
			status(o, "connected, socket id "+est.SocketID)
			for _, ch := range o.Channels {
				sub, err := subscribeData(o, est.SocketID, ch)
				if err != nil {
					return fatalf("%v", err)
				}
				if err := write(frame{Event: "pusher:subscribe", Data: sub}); err != nil {
					return err
				}
			}
		case "pusher:ping":
			if err := write(frame{Event: "pusher:pong", Data: json.RawMessage("{}")}); err != nil {
				return err
			}
			continue
		case "pusher:pong":
			continue
		case "pusher_internal:subscription_succeeded":
			status(o, "subscribed to "+f.Channel)
		case "pusher:subscription_error":
			status(o, fmt.Sprintf("subscription to %s failed: %s", f.Channel, data))
		case "pusher:error":
			var pe struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			}
			_ = json.Unmarshal([]byte(data), &pe)
			if pe.Code >= 4000 && pe.Code < 4100 {
				return fatalf("%s (code %d)", pe.Message, pe.Code)
			}
			status(o, "server error: "+data)
		}
		internal := strings.HasPrefix(f.Event, "pusher:") || strings.HasPrefix(f.Event, "pusher_internal:")
		if internal && !o.ShowInternal {
			continue
		}
		if o.OnMessage != nil {
			o.OnMessage(Message{Time: time.Now(), Channel: f.Channel, Event: f.Event, Data: data, UserID: f.UserID})
		}
	}
}

// readWithPing reads one frame, sending a pusher:ping when the connection
// has been quiet for the activity timeout.
func readWithPing(ctx context.Context, conn *websocket.Conn, activity time.Duration, write func(frame) error) (websocket.MessageType, []byte, error) {
	type result struct {
		typ websocket.MessageType
		b   []byte
		err error
	}
	ch := make(chan result, 1)
	go func() {
		t, b, err := conn.Read(ctx)
		ch <- result{t, b, err}
	}()
	timer := time.NewTimer(activity)
	defer timer.Stop()
	for {
		select {
		case r := <-ch:
			return r.typ, r.b, r.err
		case <-timer.C:
			if err := write(frame{Event: "pusher:ping", Data: json.RawMessage("{}")}); err != nil {
				return 0, nil, err
			}
		}
	}
}

func subscribeData(o ListenOptions, socketID, ch string) (json.RawMessage, error) {
	sub := map[string]string{"channel": ch}
	if needsAuth(ch) {
		channelData := ""
		if strings.HasPrefix(ch, "presence-") {
			uid := o.UserID
			if uid == "" {
				uid = "hibira-cli"
			}
			cd := map[string]any{"user_id": uid}
			if len(o.UserInfo) > 0 {
				cd["user_info"] = o.UserInfo
			}
			b, err := json.Marshal(cd)
			if err != nil {
				return nil, fmt.Errorf("presence user info: %w", err)
			}
			channelData = string(b)
			sub["channel_data"] = channelData
		}
		sub["auth"] = ChannelAuth(o.Creds.Key, o.Creds.Secret, socketID, ch, channelData)
	}
	return json.Marshal(sub)
}

// unwrapData turns the JSON string Pusher wraps event data in back into
// the original text; objects are returned as compact JSON.
func unwrapData(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}
