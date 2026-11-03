// Package tui is the interactive terminal UI: a channel list, a live event
// stream with a detail pane, and forms to subscribe and publish.
package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/hibira-io/cli/internal/pusher"
)

const maxEvents = 2000

type focus int

const (
	focusChannels focus = iota
	focusEvents
)

type mode int

const (
	modeNormal mode = iota
	modeAdd
	modeTrigger
	modeHelp
)

type chanState int

const (
	chanPending chanState = iota
	chanSubscribed
	chanFailed
)

type channel struct {
	name  string
	state chanState
	err   string
	count int
}

// Messages fed into the program.
type (
	eventMsg     pusher.Message
	statusMsg    pusher.Status
	listenEnded  struct{ err error }
	triggeredMsg struct {
		event    string
		channels []string
		err      error
	}
	flashClear struct{ id int }
)

// TriggerFunc publishes an event (injected so tests need no network).
type TriggerFunc func(ctx context.Context, p pusher.TriggerParams) error

// Model is the Bubble Tea model.
type Model struct {
	appLabel string
	canSend  bool
	sendHint string
	control  chan<- pusher.Control
	trigger  TriggerFunc

	width, height int

	connected bool
	socketID  string
	connText  string
	fatal     string

	channels   []channel
	chanCursor int
	filter     string

	events   []pusher.Message
	evCursor int // index into the filtered list
	follow   bool
	evOffset int

	focus focus
	mode  mode

	addInput  textinput.Model
	trigIn    [3]textinput.Model // channel, event, data
	trigField int
	sending   bool

	flash   string
	flashOK bool
	flashID int
}

// Options build a Model.
type Options struct {
	AppLabel string
	Channels []string
	// CanSend is false when the app id or secret is missing; SendHint
	// explains what to set.
	CanSend  bool
	SendHint string
	Control  chan<- pusher.Control
	Trigger  TriggerFunc
}

// NewModel returns the initial model.
func NewModel(o Options) Model {
	m := Model{
		appLabel: o.AppLabel,
		canSend:  o.CanSend,
		sendHint: o.SendHint,
		control:  o.Control,
		trigger:  o.Trigger,
		follow:   true,
		connText: "connecting…",
	}
	for _, ch := range o.Channels {
		m.channels = append(m.channels, channel{name: ch})
	}
	if len(m.channels) > 0 {
		m.focus = focusEvents
	}
	m.addInput = newInput("channel name, such as orders or private-user-7")
	for i, ph := range []string{"channel", "event name", `data, such as {"id":42}`} {
		m.trigIn[i] = newInput(ph)
	}
	m.trigIn[2].SetValue("{}")
	m.trigIn[2].CharLimit = 0
	return m
}

func newInput(placeholder string) textinput.Model {
	t := textinput.New()
	t.Placeholder = placeholder
	t.Prompt = ""
	t.CharLimit = 200
	return t
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		w := max(m.width-24, 10)
		m.addInput.Width = w
		for i := range m.trigIn {
			m.trigIn[i].Width = w
		}
		return m, nil
	case eventMsg:
		m.addEvent(pusher.Message(msg))
		return m, nil
	case statusMsg:
		m.applyStatus(pusher.Status(msg))
		return m, nil
	case listenEnded:
		m.connected = false
		if msg.err != nil {
			m.fatal = msg.err.Error()
			m.connText = "stopped"
		}
		return m, nil
	case triggeredMsg:
		m.sending = false
		if msg.err != nil {
			return m, m.setFlash("Send failed: "+msg.err.Error(), false)
		}
		m.mode = modeNormal
		return m, m.setFlash(fmt.Sprintf("Sent %q to %s", msg.event, strings.Join(msg.channels, ", ")), true)
	case flashClear:
		if msg.id == m.flashID {
			m.flash = ""
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) addEvent(e pusher.Message) {
	m.events = append(m.events, e)
	dropped := 0
	if len(m.events) > maxEvents {
		dropped = len(m.events) - maxEvents
		m.events = slices.Delete(m.events, 0, dropped)
	}
	for i := range m.channels {
		if m.channels[i].name == e.Channel {
			m.channels[i].count++
		}
	}
	vis := m.visible()
	if m.follow {
		m.evCursor = max(len(vis)-1, 0)
	} else if dropped > 0 && m.filter == "" {
		m.evCursor = max(m.evCursor-dropped, 0)
	}
}

func (m *Model) applyStatus(s pusher.Status) {
	switch s.Kind {
	case pusher.StatusConnected:
		m.connected, m.socketID, m.connText, m.fatal = true, s.SocketID, "connected", ""
		for i := range m.channels {
			if m.channels[i].state != chanFailed {
				m.channels[i].state = chanPending
			}
		}
	case pusher.StatusDisconnected:
		m.connected = false
		m.connText = s.Text
	case pusher.StatusSubscribed, pusher.StatusSubscriptionError:
		for i := range m.channels {
			if m.channels[i].name == s.Channel {
				if s.Kind == pusher.StatusSubscribed {
					m.channels[i].state, m.channels[i].err = chanSubscribed, ""
				} else {
					m.channels[i].state, m.channels[i].err = chanFailed, s.Text
				}
			}
		}
	case pusher.StatusError:
		m.flash, m.flashOK = s.Text, false
	}
}

func (m *Model) setFlash(text string, ok bool) tea.Cmd {
	m.flashID++
	id := m.flashID
	m.flash, m.flashOK = text, ok
	return tea.Tick(6*time.Second, func(time.Time) tea.Msg { return flashClear{id: id} })
}

// visible returns the events shown under the current filter.
func (m Model) visible() []pusher.Message {
	if m.filter == "" {
		return m.events
	}
	out := make([]pusher.Message, 0, len(m.events))
	for _, e := range m.events {
		if e.Channel == m.filter {
			out = append(out, e)
		}
	}
	return out
}

func (m Model) sendControl(c pusher.Control) tea.Cmd {
	if m.control == nil {
		return nil
	}
	ch := m.control
	return func() tea.Msg {
		ch <- c
		return nil
	}
}

func (m Model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if k.String() == "ctrl+c" {
		return m, tea.Quit
	}
	switch m.mode {
	case modeHelp:
		m.mode = modeNormal
		return m, nil
	case modeAdd:
		return m.handleAddKey(k)
	case modeTrigger:
		return m.handleTriggerKey(k)
	}
	switch k.String() {
	case "q":
		return m, tea.Quit
	case "?":
		m.mode = modeHelp
	case "tab", "shift+tab":
		if m.focus == focusChannels {
			m.focus = focusEvents
		} else {
			m.focus = focusChannels
		}
	case "left", "h":
		m.focus = focusChannels
	case "right", "l":
		m.focus = focusEvents
	case "a", "s":
		m.mode = modeAdd
		m.addInput.SetValue("")
		return m, m.addInput.Focus()
	case "t", "p":
		return m.openTrigger()
	case "x", "delete", "backspace":
		if m.focus == focusChannels && len(m.channels) > 0 {
			name := m.channels[m.chanCursor].name
			m.channels = slices.Delete(m.channels, m.chanCursor, m.chanCursor+1)
			m.chanCursor = min(m.chanCursor, max(len(m.channels)-1, 0))
			if m.filter == name {
				m.filter = ""
				m.follow = true
				m.evCursor = max(len(m.events)-1, 0)
			}
			return m, tea.Batch(m.sendControl(pusher.Control{Channel: name, Unsubscribe: true}), m.setFlash("Unsubscribed from "+name, true))
		}
	case "c":
		m.events = nil
		m.evCursor, m.evOffset, m.follow = 0, 0, true
		for i := range m.channels {
			m.channels[i].count = 0
		}
	case "enter", " ":
		if m.focus == focusChannels && len(m.channels) > 0 {
			name := m.channels[m.chanCursor].name
			if m.filter == name {
				m.filter = ""
			} else {
				m.filter = name
			}
			m.follow = true
			m.evCursor = max(len(m.visible())-1, 0)
		}
	case "esc":
		if m.filter != "" {
			m.filter = ""
			m.follow = true
			m.evCursor = max(len(m.events)-1, 0)
		}
	case "f", "G", "end":
		m.follow = true
		m.evCursor = max(len(m.visible())-1, 0)
	case "g", "home":
		if m.focus == focusEvents {
			m.follow = false
			m.evCursor = 0
		}
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "pgup":
		m.focus = focusEvents
		m.move(-m.listHeight())
	case "pgdown":
		m.focus = focusEvents
		m.move(m.listHeight())
	}
	return m, nil
}

func (m *Model) move(d int) {
	if m.focus == focusChannels {
		if len(m.channels) > 0 {
			m.chanCursor = min(max(m.chanCursor+d, 0), len(m.channels)-1)
		}
		return
	}
	n := len(m.visible())
	if n == 0 {
		return
	}
	m.evCursor = min(max(m.evCursor+d, 0), n-1)
	m.follow = m.evCursor == n-1
}

func (m Model) handleAddKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.mode = modeNormal
		m.addInput.Blur()
		return m, nil
	case "enter":
		name := strings.TrimSpace(m.addInput.Value())
		m.mode = modeNormal
		m.addInput.Blur()
		if name == "" {
			return m, nil
		}
		for i, c := range m.channels {
			if c.name == name {
				m.chanCursor = i
				return m, m.setFlash("Already subscribed to "+name, true)
			}
		}
		m.channels = append(m.channels, channel{name: name})
		m.chanCursor = len(m.channels) - 1
		m.focus = focusChannels
		return m, m.sendControl(pusher.Control{Channel: name})
	}
	var cmd tea.Cmd
	m.addInput, cmd = m.addInput.Update(k)
	return m, cmd
}

func (m Model) openTrigger() (tea.Model, tea.Cmd) {
	if !m.canSend {
		return m, m.setFlash(m.sendHint, false)
	}
	if len(m.channels) > 0 && (m.trigIn[0].Value() == "" || m.focus == focusChannels) {
		m.trigIn[0].SetValue(m.channels[m.chanCursor].name)
	}
	m.mode = modeTrigger
	m.trigField = 0
	if m.trigIn[0].Value() != "" {
		m.trigField = 1
	}
	return m, m.focusTrig()
}

func (m *Model) focusTrig() tea.Cmd {
	var cmd tea.Cmd
	for i := range m.trigIn {
		if i == m.trigField {
			cmd = m.trigIn[i].Focus()
			m.trigIn[i].CursorEnd()
		} else {
			m.trigIn[i].Blur()
		}
	}
	return cmd
}

func (m Model) handleTriggerKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.mode = modeNormal
		for i := range m.trigIn {
			m.trigIn[i].Blur()
		}
		return m, nil
	case "tab", "down":
		m.trigField = (m.trigField + 1) % len(m.trigIn)
		return m, m.focusTrig()
	case "shift+tab", "up":
		m.trigField = (m.trigField + len(m.trigIn) - 1) % len(m.trigIn)
		return m, m.focusTrig()
	case "enter":
		if m.sending {
			return m, nil
		}
		var chans []string
		for _, c := range strings.Split(m.trigIn[0].Value(), " ") {
			if c = strings.TrimSpace(c); c != "" {
				chans = append(chans, c)
			}
		}
		event := strings.TrimSpace(m.trigIn[1].Value())
		data := m.trigIn[2].Value()
		switch {
		case len(chans) == 0:
			m.trigField = 0
			return m, tea.Batch(m.focusTrig(), m.setFlash("Enter a channel", false))
		case event == "":
			m.trigField = 1
			return m, tea.Batch(m.focusTrig(), m.setFlash("Enter an event name", false))
		}
		if data == "" {
			data = "{}"
		}
		m.sending = true
		fn := m.trigger
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			err := fn(ctx, pusher.TriggerParams{Name: event, Channels: chans, Data: data})
			return triggeredMsg{event: event, channels: chans, err: err}
		}
	}
	var cmd tea.Cmd
	m.trigIn[m.trigField], cmd = m.trigIn[m.trigField].Update(k)
	return m, cmd
}

// ---- view ----

var (
	brand   = lipgloss.AdaptiveColor{Light: "#e5484d", Dark: "#ff6369"}
	muted   = lipgloss.AdaptiveColor{Light: "#6f6d66", Dark: "#9b9990"}
	faint   = lipgloss.AdaptiveColor{Light: "#c8c6bf", Dark: "#3b3a37"}
	okColor = lipgloss.AdaptiveColor{Light: "#218358", Dark: "#3dd68c"}
	warn    = lipgloss.AdaptiveColor{Light: "#ab6400", Dark: "#ffca16"}
	selBg   = lipgloss.AdaptiveColor{Light: "#ece9e2", Dark: "#2a2927"}
	palette = []lipgloss.AdaptiveColor{
		{Light: "#0d74ce", Dark: "#70b8ff"},
		{Light: "#8145b5", Dark: "#d19dff"},
		{Light: "#218358", Dark: "#3dd68c"},
		{Light: "#cc4e00", Dark: "#ffa057"},
		{Light: "#c2298a", Dark: "#ff8dcc"},
		{Light: "#107d98", Dark: "#4ccce6"},
	}
	sMuted  = lipgloss.NewStyle().Foreground(muted)
	sTitle  = lipgloss.NewStyle().Bold(true)
	sBrand  = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Background(brand).Bold(true).Padding(0, 1)
	sKey    = lipgloss.NewStyle().Bold(true)
	sCursor = lipgloss.NewStyle().Foreground(brand).Bold(true)
	sBorder = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(faint).Padding(0, 1)
	sActive = sBorder.BorderForeground(brand)
)

func chanColor(name string) lipgloss.AdaptiveColor {
	h := fnv.New32a()
	h.Write([]byte(name))
	return palette[h.Sum32()%uint32(len(palette))]
}

const chanWidth = 26

func (m Model) listHeight() int {
	return max(m.height-2-2-m.detailHeight()-2, 1)
}

func (m Model) detailHeight() int {
	if m.mode == modeTrigger {
		return 4
	}
	if m.mode == modeAdd {
		return 2
	}
	return min(max(m.height/4, 4), 12)
}

// View implements tea.Model.
func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.mode == modeHelp {
		return m.helpView()
	}
	header := m.headerView()
	footer := m.footerView()
	bodyH := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
	rightW := m.width - chanWidth
	left := m.channelsView(chanWidth-2, bodyH-2)
	listH := bodyH - 2 - m.detailHeight() - 2
	events := m.eventsView(rightW-2, max(listH, 1))
	bottom := m.bottomView(rightW-2, m.detailHeight())
	right := lipgloss.JoinVertical(lipgloss.Left, events, bottom)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m Model) headerView() string {
	left := sBrand.Render("hibira") + " " + sMuted.Render(m.appLabel)
	var state string
	switch {
	case m.fatal != "":
		state = lipgloss.NewStyle().Foreground(brand).Render("✕ " + m.fatal)
	case m.connected:
		state = lipgloss.NewStyle().Foreground(okColor).Render("● connected") + sMuted.Render("  socket "+m.socketID)
	default:
		state = lipgloss.NewStyle().Foreground(warn).Render("○ " + m.connText)
	}
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(state) - 1
	if gap < 1 {
		state = truncate(state, max(m.width-lipgloss.Width(left)-2, 0))
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + state
}

func (m Model) channelsView(outer, h int) string {
	w := outer - 2
	style := sBorder
	if m.focus == focusChannels && m.mode == modeNormal {
		style = sActive
	}
	lines := []string{sTitle.Render("Channels")}
	if len(m.channels) == 0 {
		lines = append(lines, "", sMuted.Render("None yet."), sMuted.Render("Press a to subscribe."))
	}
	start := max(0, m.chanCursor-(h-2))
	for i := start; i < len(m.channels) && len(lines) < h; i++ {
		c := m.channels[i]
		var dot string
		switch c.state {
		case chanSubscribed:
			dot = lipgloss.NewStyle().Foreground(okColor).Render("●")
		case chanFailed:
			dot = lipgloss.NewStyle().Foreground(brand).Render("✕")
		default:
			dot = lipgloss.NewStyle().Foreground(warn).Render("○")
		}
		count := fmt.Sprint(c.count)
		name := truncate(c.name, w-4-len(count))
		pad := max(w-3-runewidth.StringWidth(name)-len(count), 1)
		nameStyle := lipgloss.NewStyle().Foreground(chanColor(c.name))
		if m.filter == c.name {
			nameStyle = nameStyle.Bold(true).Underline(true)
		}
		lead := " "
		if i == m.chanCursor && m.focus == focusChannels {
			lead = sCursor.Render("▌")
		}
		line := lead + dot + " " + nameStyle.Render(name) + strings.Repeat(" ", pad) + sMuted.Render(count)
		lines = append(lines, line)
	}
	if len(m.channels) > 0 && m.chanCursor < len(m.channels) {
		if c := m.channels[m.chanCursor]; c.state == chanFailed && len(lines) < h-1 {
			lines = append(lines, "", lipgloss.NewStyle().Foreground(brand).Render(truncate(c.err, w)))
		}
	}
	return style.Width(outer).Height(h).Render(strings.Join(lines, "\n"))
}

func (m Model) eventsView(outer, h int) string {
	w := outer - 2
	style := sBorder
	if m.focus == focusEvents && m.mode == modeNormal {
		style = sActive
	}
	vis := m.visible()
	title := "Events"
	if m.filter != "" {
		title += sMuted.Render(" in ") + lipgloss.NewStyle().Foreground(chanColor(m.filter)).Render(m.filter) + sMuted.Render("  esc shows all")
	}
	if !m.follow && len(vis) > 0 {
		title += sMuted.Render(fmt.Sprintf("  %d/%d · f follows new events", m.evCursor+1, len(vis)))
	}
	lines := []string{truncate(sTitle.Render(title), w)}
	rows := h - 1
	if len(vis) == 0 {
		hint := "Waiting for events…"
		if len(m.channels) == 0 {
			hint = "Subscribe to a channel with a, then publish with t."
		}
		lines = append(lines, "", sMuted.Render(hint))
	}
	start := 0
	if len(vis) > rows {
		start = min(max(m.evCursor-rows+1, 0), len(vis)-rows)
		if m.evCursor < start {
			start = m.evCursor
		}
	}
	chW, evW := 0, 0
	for i := start; i < len(vis) && i < start+rows; i++ {
		chW = max(chW, runewidth.StringWidth(orDash(vis[i].Channel)))
		evW = max(evW, runewidth.StringWidth(vis[i].Event))
	}
	chW, evW = min(chW, 22), min(evW, 26)
	for i := start; i < len(vis) && i < start+rows; i++ {
		e := vis[i]
		ts := sMuted.Render(e.Time.Format("15:04:05.000"))
		ch := lipgloss.NewStyle().Foreground(chanColor(e.Channel)).Render(runewidth.FillRight(truncate(orDash(e.Channel), chW), chW))
		ev := sTitle.Render(runewidth.FillRight(truncate(e.Event, evW), evW))
		lead := "  "
		if i == m.evCursor {
			if m.focus == focusEvents {
				lead = sCursor.Render("▌") + " "
			} else {
				lead = sMuted.Render("›") + " "
			}
		}
		prefix := lead + ts + "  " + ch + "  " + ev + "  "
		rest := max(w-lipgloss.Width(prefix), 0)
		data := sMuted.Render(truncate(oneLine(e.Data), rest))
		lines = append(lines, prefix+data)
	}
	return style.Width(outer).Height(h).Render(strings.Join(lines, "\n"))
}

func (m Model) bottomView(outer, h int) string {
	w := outer - 2
	switch m.mode {
	case modeAdd:
		body := sTitle.Render("Subscribe") + "\n" + label("Channel") + m.addInput.View()
		return sActive.Width(outer).Height(h).Render(body)
	case modeTrigger:
		names := []string{"Channel", "Event", "Data"}
		lines := []string{sTitle.Render("Publish an event") + sMuted.Render("  separate several channels with spaces")}
		for i, in := range m.trigIn {
			l := label(names[i])
			if i == m.trigField {
				l = lipgloss.NewStyle().Foreground(brand).Bold(true).Width(10).Render(names[i])
			}
			lines = append(lines, l+in.View())
		}
		return sActive.Width(outer).Height(h).Render(strings.Join(lines, "\n"))
	}
	vis := m.visible()
	if len(vis) == 0 || m.evCursor >= len(vis) {
		return sBorder.Width(outer).Height(h).Render(sMuted.Render("Select an event to see its data."))
	}
	e := vis[m.evCursor]
	head := lipgloss.NewStyle().Foreground(chanColor(e.Channel)).Render(orDash(e.Channel)) + "  " + sTitle.Render(e.Event) +
		sMuted.Render("  "+e.Time.Format("2006-01-02 15:04:05.000"))
	if e.UserID != "" {
		head += sMuted.Render("  user " + e.UserID)
	}
	head = truncate(head, w)
	body := prettyJSON(e.Data)
	bl := strings.Split(body, "\n")
	if len(bl) > h-1 {
		bl = append(bl[:h-2], sMuted.Render(fmt.Sprintf("… %d more lines", len(bl)-(h-2))))
	}
	for i := range bl {
		bl[i] = truncate(bl[i], w)
	}
	return sBorder.Width(outer).Height(h).Render(head + "\n" + strings.Join(bl, "\n"))
}

func (m Model) footerView() string {
	if m.flash != "" {
		c := okColor
		if !m.flashOK {
			c = brand
		}
		return truncate(lipgloss.NewStyle().Foreground(c).Render(" "+m.flash), m.width)
	}
	var keys [][2]string
	switch m.mode {
	case modeAdd:
		keys = [][2]string{{"enter", "subscribe"}, {"esc", "cancel"}}
	case modeTrigger:
		keys = [][2]string{{"enter", "send"}, {"tab", "next field"}, {"esc", "cancel"}}
	default:
		keys = [][2]string{{"a", "subscribe"}, {"t", "publish"}, {"tab", "switch pane"}, {"↑↓", "move"}, {"enter", "filter"}, {"x", "unsubscribe"}, {"c", "clear"}, {"?", "help"}, {"q", "quit"}}
	}
	var parts []string
	for _, k := range keys {
		parts = append(parts, sKey.Render(k[0])+" "+sMuted.Render(k[1]))
	}
	return truncate(" "+strings.Join(parts, sMuted.Render(" · ")), m.width)
}

func (m Model) helpView() string {
	rows := [][2]string{
		{"a / s", "subscribe to a channel (private- and presence- are signed with the app secret)"},
		{"t / p", "publish an event; the channel is prefilled from the selected one"},
		{"tab, ← →", "switch between the channel list and the event stream"},
		{"↑ ↓, j k", "move; PgUp/PgDn page through events"},
		{"enter", "show only the selected channel's events (again, or esc, to show all)"},
		{"f / G", "jump to the newest event and follow new ones"},
		{"g", "jump to the oldest event"},
		{"x", "unsubscribe from the selected channel"},
		{"c", "clear the event list"},
		{"q, ctrl+c", "quit"},
	}
	lines := []string{sBrand.Render("hibira") + " " + sTitle.Render("keys"), ""}
	for _, r := range rows {
		lines = append(lines, lipgloss.NewStyle().Bold(true).Width(12).Render(r[0])+sMuted.Render(r[1]))
	}
	lines = append(lines, "", sMuted.Render("Press any key to go back."))
	return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func label(s string) string {
	return sMuted.Width(10).Render(s)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func oneLine(s string) string {
	var b bytes.Buffer
	if json.Valid([]byte(s)) && json.Compact(&b, []byte(s)) == nil {
		return b.String()
	}
	return strings.Join(strings.Fields(s), " ")
}

func prettyJSON(s string) string {
	var b bytes.Buffer
	if json.Valid([]byte(s)) && json.Indent(&b, []byte(s), "", "  ") == nil {
		return b.String()
	}
	return s
}

// truncate shortens s (which may hold ANSI styling) to w cells.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	if !strings.Contains(s, "\x1b") {
		return runewidth.Truncate(s, w, "…")
	}
	return lipgloss.NewStyle().MaxWidth(w).Render(s)
}
