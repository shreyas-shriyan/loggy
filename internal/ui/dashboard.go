package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/shreyas-shriyan/loggy/internal/config"
	"github.com/shreyas-shriyan/loggy/internal/stream"
)

// ErrNeedLogin is returned by a StartFunc when the target's AWS session must be
// refreshed interactively.
var ErrNeedLogin = errors.New("AWS sign-in required")

// maxLines caps the log buffer.
const maxLines = 5000

// Runner streams log lines to sink until done or ctx is cancelled.
type Runner func(ctx context.Context, sink func(string)) error

// Request describes what to stream.
type Request struct {
	Since   time.Duration
	Pattern string
	Pod     string
	// ShowHealth starts the stream with health-check lines visible.
	ShowHealth bool
}

func (r Request) describe() string {
	parts := []string{"last " + shortDuration(r.Since)}
	if r.Pattern != "" {
		parts = append(parts, "/"+r.Pattern+"/")
	}
	if r.Pod != "" {
		parts = append(parts, "pod "+r.Pod)
	}
	return strings.Join(parts, " · ")
}

func shortDuration(d time.Duration) string {
	switch {
	case d >= 24*time.Hour && d%(24*time.Hour) == 0:
		return fmt.Sprintf("%dd", d/(24*time.Hour))
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	}
	return d.String()
}

// StartFunc prepares a stream for t. It runs off the UI goroutine.
type StartFunc func(ctx context.Context, t config.Target, req Request) (Runner, error)

// Item is a sidebar row.
type Item struct {
	Target  config.Target
	Region  string
	Unsaved bool
}

// ActionKind is what the dashboard asks its caller to do after it exits.
type ActionKind int

// Dashboard exit actions.
const (
	ActQuit ActionKind = iota
	ActAdd
	ActRemove
	ActSettings
	ActSwitchEnv
	ActCustom
	ActSave
	ActLogin
	ActSaveTarget
)

// Action is returned by Run.
type Action struct {
	Kind    ActionKind
	Target  config.Target
	Request Request
	Cursor  int
}

// Options configures a dashboard run.
type Options struct {
	Env     string
	Profile string
	Items   []Item
	Cursor  int
	Start   StartFunc
	// AutoStart, if set, streams Items[Cursor] immediately.
	AutoStart *Request
	Flash     string
}

// Run shows the dashboard until the user quits or picks an action that needs
// the regular terminal.
func Run(opts Options) (Action, error) {
	m := newModel(opts)
	final, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	if err != nil {
		return Action{Kind: ActQuit}, err
	}
	fm := final.(*model)
	fm.stopStream()
	fm.action.Cursor = fm.cursor
	return fm.action, nil
}

type focus int

const (
	focusSidebar focus = iota
	focusLogs
)

type entry struct {
	raw      string
	health   bool
	rendered string
}

type streamState struct {
	id      int
	target  config.Target
	req     Request
	ctx     context.Context
	cancel  context.CancelFunc
	lines   chan string
	done    chan error
	status  string
	active  bool
	err     error
	started time.Time
}

type model struct {
	opts   Options
	cursor int
	focus  focus

	width, height int
	vp            viewport.Model
	ready         bool

	entries    []entry
	hideHealth bool
	pretty     bool
	follow     bool

	stream *streamState
	nextID int

	flash      string
	flashError bool
	action     Action
}

type startedMsg struct {
	id     int
	runner Runner
	err    error
}

type linesMsg struct {
	id    int
	lines []string
}

type doneMsg struct {
	id    int
	err   error
	lines []string
}

func newModel(opts Options) *model {
	m := &model{opts: opts, cursor: opts.Cursor, hideHealth: true, pretty: true, follow: true, flash: opts.Flash}
	if strings.HasPrefix(opts.Flash, "Error") {
		m.flashError = true
	}
	if m.cursor >= len(opts.Items) {
		m.cursor = max(0, len(opts.Items)-1)
	}
	return m
}

func (m *model) Init() tea.Cmd {
	if m.opts.AutoStart != nil && len(m.opts.Items) > 0 {
		return m.start(*m.opts.AutoStart)
	}
	return nil
}

func (m *model) selected() (Item, bool) {
	if m.cursor < 0 || m.cursor >= len(m.opts.Items) {
		return Item{}, false
	}
	return m.opts.Items[m.cursor], true
}

func (m *model) start(req Request) tea.Cmd {
	it, ok := m.selected()
	if !ok {
		return nil
	}
	if !it.Target.Supported() {
		m.setFlash(fmt.Sprintf("%s: target type %q is not supported", it.Target.Primary(), it.Target.Type), true)
		return nil
	}
	m.stopStream()
	m.nextID++
	ctx, cancel := context.WithCancel(context.Background())
	st := &streamState{id: m.nextID, target: it.Target, req: req, ctx: ctx, cancel: cancel, status: "connecting", active: true, started: time.Now()}
	m.stream = st
	m.entries = nil
	m.hideHealth = !req.ShowHealth
	m.follow = true
	m.flash = ""
	m.refresh()

	startFn := m.opts.Start
	id := st.id
	t := it.Target
	return func() tea.Msg {
		r, err := startFn(ctx, t, req)
		return startedMsg{id: id, runner: r, err: err}
	}
}

func (m *model) stopStream() {
	if m.stream != nil && m.stream.active {
		m.stream.cancel()
		m.stream.active = false
		m.stream.status = "stopped"
	}
}

func waitLines(st *streamState) tea.Cmd {
	return func() tea.Msg {
		select {
		case l := <-st.lines:
			batch := []string{l}
			for len(batch) < 500 {
				select {
				case l := <-st.lines:
					batch = append(batch, l)
				default:
					return linesMsg{id: st.id, lines: batch}
				}
			}
			return linesMsg{id: st.id, lines: batch}
		case err := <-st.done:
			var rest []string
			for {
				select {
				case l := <-st.lines:
					rest = append(rest, l)
				default:
					return doneMsg{id: st.id, err: err, lines: rest}
				}
			}
		}
	}
}

func (m *model) current(id int) bool { return m.stream != nil && m.stream.id == id }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.rerender()
		return m, nil

	case startedMsg:
		if !m.current(msg.id) || !m.stream.active {
			return m, nil
		}
		st := m.stream
		if msg.err != nil {
			if errors.Is(msg.err, ErrNeedLogin) {
				m.action = Action{Kind: ActLogin, Target: st.target, Request: st.req}
				return m, tea.Quit
			}
			st.active, st.status, st.err = false, "failed", msg.err
			m.setFlash("Error: "+msg.err.Error(), true)
			return m, nil
		}
		st.status = "live"
		st.lines = make(chan string, 2048)
		st.done = make(chan error, 1)
		ctx := st.ctx
		lines := st.lines
		go func() {
			st.done <- msg.runner(ctx, func(l string) {
				select {
				case lines <- l:
				case <-ctx.Done():
				}
			})
		}()
		return m, waitLines(st)

	case linesMsg:
		if !m.current(msg.id) {
			return m, nil
		}
		m.appendLines(msg.lines)
		return m, waitLines(m.stream)

	case doneMsg:
		if !m.current(msg.id) {
			return m, nil
		}
		m.appendLines(msg.lines)
		st := m.stream
		if st.active {
			st.active = false
			switch {
			case msg.err != nil && !errors.Is(msg.err, context.Canceled):
				st.status, st.err = "failed", msg.err
				m.setFlash("Error: "+msg.err.Error(), true)
			default:
				st.status = "ended"
			}
		}
		m.refresh()
		return m, nil

	case tea.MouseMsg:
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		m.follow = m.vp.AtBottom()
		return m, cmd

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *model) exit(kind ActionKind) (tea.Model, tea.Cmd) {
	it, _ := m.selected()
	m.action = Action{Kind: kind, Target: it.Target}
	return m, tea.Quit
}

func (m *model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := k.String()
	switch key {
	case "ctrl+c":
		if m.stream != nil && m.stream.active {
			m.stopStream()
			m.refresh()
			return m, nil
		}
		return m.exit(ActQuit)
	case "q":
		return m.exit(ActQuit)
	case "tab", "shift+tab":
		if m.focus == focusSidebar {
			m.focus = focusLogs
		} else {
			m.focus = focusSidebar
		}
		return m, nil
	case "x":
		m.stopStream()
		m.refresh()
		return m, nil
	case "h":
		m.hideHealth = !m.hideHealth
		m.refresh()
		return m, nil
	case "J":
		m.pretty = !m.pretty
		m.rerender()
		return m, nil
	case "G", "end":
		m.follow = true
		m.vp.GotoBottom()
		return m, nil
	case "e":
		return m.exit(ActSwitchEnv)
	case "n":
		return m.exit(ActAdd)
	case "p":
		return m.exit(ActSettings)
	}

	if m.focus == focusLogs {
		switch key {
		case "esc":
			m.focus = focusSidebar
			return m, nil
		case "g", "home":
			m.follow = false
			m.vp.GotoTop()
			return m, nil
		}
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(k)
		m.follow = m.vp.AtBottom()
		return m, cmd
	}

	switch key {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.opts.Items)-1 {
			m.cursor++
		}
	case "pgup", "pgdown":
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(k)
		m.follow = m.vp.AtBottom()
		return m, cmd
	case "enter", "l":
		return m, m.start(Request{Since: 15 * time.Minute})
	case "a":
		return m, m.start(Request{Since: 15 * time.Minute, ShowHealth: true})
	case "c":
		if _, ok := m.selected(); ok {
			return m.exit(ActCustom)
		}
	case "s":
		if _, ok := m.selected(); ok {
			return m.exit(ActSave)
		}
	case "d":
		if len(m.opts.Items) > 0 {
			return m.exit(ActRemove)
		}
	case "S":
		if it, ok := m.selected(); ok && it.Unsaved {
			return m.exit(ActSaveTarget)
		}
	}
	return m, nil
}

func (m *model) setFlash(s string, isErr bool) {
	m.flash, m.flashError = s, isErr
}

func (m *model) appendLines(lines []string) {
	w := m.vp.Width
	for _, l := range lines {
		e := entry{raw: l, health: stream.IsHealthCheck(l)}
		e.rendered = Format(Parse(l), FormatOptions{Width: w, PrettyJSON: m.pretty})
		m.entries = append(m.entries, e)
	}
	if over := len(m.entries) - maxLines; over > 0 {
		m.entries = append(m.entries[:0:0], m.entries[over:]...)
	}
	m.refresh()
}

func (m *model) rerender() {
	w := m.vp.Width
	for i := range m.entries {
		m.entries[i].rendered = Format(Parse(m.entries[i].raw), FormatOptions{Width: w, PrettyJSON: m.pretty})
	}
	m.refresh()
}

func (m *model) visibleCount() int {
	n := 0
	for _, e := range m.entries {
		if !(m.hideHealth && e.health) {
			n++
		}
	}
	return n
}

// refresh rebuilds the viewport content from the buffer.
func (m *model) refresh() {
	if !m.ready {
		return
	}
	var b strings.Builder
	for _, e := range m.entries {
		if m.hideHealth && e.health {
			continue
		}
		b.WriteString(e.rendered)
		b.WriteByte('\n')
	}
	content := strings.TrimSuffix(b.String(), "\n")
	if content == "" {
		content = m.emptyState()
	}
	m.vp.SetContent(content)
	if m.follow {
		m.vp.GotoBottom()
	}
}

func (m *model) emptyState() string {
	msg := "Select a server and press " + sKey.Render("enter") + sMuted.Render(" to stream its logs.")
	switch {
	case len(m.opts.Items) == 0:
		msg = "No saved servers yet. Press " + sKey.Render("n") + sMuted.Render(" to add one.")
	case m.stream != nil && m.stream.status == "connecting":
		msg = "Connecting to " + m.stream.target.Primary() + "..."
	case m.stream != nil && m.stream.active:
		msg = "Waiting for log lines..."
	case m.stream != nil:
		msg = "No log lines."
	}
	return lipgloss.Place(m.vp.Width, m.vp.Height, lipgloss.Center, lipgloss.Center, sMuted.Render(msg))
}

const (
	headerHeight = 1
	footerHeight = 2
)

func (m *model) sidebarWidth() int {
	w := m.width * 32 / 100
	return min(max(w, 30), 48)
}

func (m *model) bodyHeight() int { return max(m.height-headerHeight-footerHeight, 5) }

func (m *model) layout() {
	logW := m.width - m.sidebarWidth()
	innerW := max(logW-sPane.GetHorizontalFrameSize(), 10)
	innerH := max(m.bodyHeight()-sPane.GetVerticalFrameSize()-1, 1)
	if !m.ready {
		m.vp = viewport.New(innerW, innerH)
		m.vp.MouseWheelEnabled = true
		m.ready = true
		return
	}
	m.vp.Width, m.vp.Height = innerW, innerH
}

func (m *model) View() string {
	if !m.ready {
		return ""
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, m.viewSidebar(), m.viewLogs())
	return lipgloss.JoinVertical(lipgloss.Left, m.viewHeader(), body, m.viewFooter())
}

func (m *model) viewHeader() string {
	left := sAccent.Render(" loggy ") + " " + sEnvPill.Render(m.opts.Env)
	if m.opts.Profile != "" {
		left += sMuted.Render("  profile " + m.opts.Profile)
	}
	right := sMuted.Render(fmt.Sprintf("%d servers ", len(m.opts.Items)))
	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return left + strings.Repeat(" ", gap) + right
}

func (m *model) viewSidebar() string {
	w := m.sidebarWidth()
	style := sPane
	if m.focus == focusSidebar {
		style = sPaneFocused
	}
	innerW := w - style.GetHorizontalFrameSize()
	innerH := m.bodyHeight() - style.GetVerticalFrameSize()

	var rows []string
	rows = append(rows, sBold.Render("Servers"), "")

	const itemHeight = 3
	capacity := max((innerH-4)/itemHeight, 1)
	first := 0
	if m.cursor >= capacity {
		first = m.cursor - capacity + 1
	}
	for i := first; i < len(m.opts.Items) && i < first+capacity; i++ {
		rows = append(rows, m.viewItem(m.opts.Items[i], i == m.cursor, innerW)...)
	}
	if len(m.opts.Items) == 0 {
		rows = append(rows, sMuted.Render("Nothing saved for "+m.opts.Env+"."))
	}

	content := lipgloss.JoinVertical(lipgloss.Left, rows...)
	hints := sMuted.Render(keyHint("n", "add") + "  " + keyHint("d", "remove") + "\n" + keyHint("p", "profile") + "  " + keyHint("e", "switch env"))
	gap := max(innerH-lipgloss.Height(content)-lipgloss.Height(hints), 0)
	content += strings.Repeat("\n", gap) + "\n" + hints
	return style.Width(w - style.GetHorizontalBorderSize()).Height(innerH).Render(content)
}

func (m *model) viewItem(it Item, selected bool, width int) []string {
	t := it.Target
	marker := "  "
	if m.stream != nil && m.stream.target.Key() == t.Key() {
		switch {
		case m.stream.active:
			marker = sOK.Render("● ")
		case m.stream.err != nil:
			marker = sError.Render("● ")
		default:
			marker = sMuted.Render("○ ")
		}
	}
	name := t.Primary()
	if it.Unsaved {
		name += sMuted.Render(" (unsaved)")
	}
	line1 := marker + sBold.Render(name)
	line2 := "  " + sMuted.Render(itemDetail(t))
	line1 = ansi.Truncate(line1, width, "…")
	line2 = ansi.Truncate(line2, width, "…")
	if selected {
		bar := lipgloss.NewStyle().Foreground(colAccent).Render("▌")
		line1 = sSelected.Width(width).Render(bar + ansi.Truncate(line1, width-1, "…"))
		line2 = sSelected.Width(width).Render(bar + ansi.Truncate(line2, width-1, "…"))
	}
	return []string{line1, line2, ""}
}

func itemDetail(t config.Target) string {
	switch t.Type {
	case config.TypeEKS:
		return "EKS · " + t.Namespace + " · " + t.Cluster
	case config.TypeECS:
		return "ECS · " + config.ShortName(t.Cluster)
	}
	return t.Type + " (not supported)"
}

func (m *model) viewLogs() string {
	w := m.width - m.sidebarWidth()
	style := sPane
	if m.focus == focusLogs {
		style = sPaneFocused
	}
	title := sMuted.Render("Logs")
	if st := m.stream; st != nil {
		title = sBold.Render(st.target.Primary()) + "  " + statusBadge(st) + sMuted.Render("  "+st.req.describe())
	}
	meta := sMuted.Render(fmt.Sprintf("%d lines", m.visibleCount()))
	if m.hideHealth {
		meta += sMuted.Render(" · health hidden")
	}
	if !m.follow {
		meta += "  " + sWarn.Render("paused · G to follow")
	}
	innerW := w - style.GetHorizontalFrameSize()
	gap := max(innerW-lipgloss.Width(title)-lipgloss.Width(meta), 1)
	head := ansi.Truncate(title+strings.Repeat(" ", gap)+meta, innerW, "…")
	return style.Width(w - style.GetHorizontalBorderSize()).Render(head + "\n" + m.vp.View())
}

func statusBadge(st *streamState) string {
	switch st.status {
	case "live":
		return sOK.Render("● live")
	case "connecting":
		return sWarn.Render("◌ connecting")
	case "failed":
		return sError.Render("✕ failed")
	case "ended":
		return sMuted.Render("■ ended")
	}
	return sMuted.Render("■ stopped")
}

func keyHint(k, label string) string { return sKey.Render(k) + sMuted.Render(" "+label) }

func (m *model) viewFooter() string {
	var hints []string
	if m.focus == focusLogs {
		hints = []string{keyHint("↑↓/pgup/pgdn", "scroll"), keyHint("g/G", "top/follow"), keyHint("esc", "servers")}
	} else {
		hints = []string{keyHint("enter", "live"), keyHint("a", "all logs"), keyHint("c", "custom"), keyHint("s", "save")}
		if it, ok := m.selected(); ok && it.Unsaved {
			hints = append(hints, keyHint("S", "save server"))
		}
	}
	hints = append(hints, keyHint("x", "stop"), keyHint("h", "health"), keyHint("J", "json"), keyHint("tab", "focus"), keyHint("q", "quit"))
	line := ansi.Truncate(" "+strings.Join(hints, "  "), m.width, "…")

	flash := ""
	if m.flash != "" {
		s := sOK
		if m.flashError {
			s = sError
		}
		flash = ansi.Truncate(" "+s.Render(m.flash), m.width, "…")
	}
	return flash + "\n" + line
}
