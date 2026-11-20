package ui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/shreyas-shriyan/loggy/internal/clipboard"
)

var sSelection = lipgloss.NewStyle().Reverse(true)

// pos is a cell in the log content: a visual line and a column.
type pos struct{ line, col int }

func (p pos) before(q pos) bool { return p.line < q.line || (p.line == q.line && p.col < q.col) }

// selection is a mouse text selection over the log content.
type selection struct {
	dragging bool
	has      bool
	anchor   pos
	head     pos
}

// bounds returns the selection's start and end in reading order; end is inclusive.
func (s selection) bounds() (pos, pos) {
	if s.head.before(s.anchor) {
		return s.head, s.anchor
	}
	return s.anchor, s.head
}

// copiedMsg reports the result of a clipboard copy.
type copiedMsg struct {
	what string
	via  string
	err  error
}

// CopyFunc copies text to the clipboard; it is replaceable in tests.
var CopyFunc = func(text string) (string, error) { return clipboard.Copy(text, os.Stderr) }

func copyCmd(text, what string) tea.Cmd {
	return func() tea.Msg {
		via, err := CopyFunc(text)
		return copiedMsg{what: what, via: via, err: err}
	}
}

// logOrigin is the screen cell of the viewport's top-left corner.
func (m *model) logOrigin() (x, y int) {
	return m.sidebarWidth() + 2, headerHeight + 2
}

// contentPos maps a screen cell to a content position, clamped to the content.
func (m *model) contentPos(x, y int) pos {
	ox, oy := m.logOrigin()
	row := min(max(y-oy, 0), m.vp.Height-1)
	line := min(m.vp.YOffset+row, max(len(m.plain)-1, 0))
	col := max(x-ox, 0)
	if line < len(m.plain) {
		col = min(col, max(ansi.StringWidth(m.plain[line])-1, 0))
	}
	return pos{line: line, col: col}
}

func (m *model) inLogs(x, y int) bool {
	ox, oy := m.logOrigin()
	return x >= ox-1 && y >= oy && y < oy+m.vp.Height
}

func (m *model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if tea.MouseEvent(msg).IsWheel() {
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		m.follow = m.vp.AtBottom()
		return m, cmd
	}

	switch msg.Action {
	case tea.MouseActionPress:
		if msg.Button != tea.MouseButtonLeft {
			return m, nil
		}
		if msg.X < m.sidebarWidth() {
			m.clickSidebar(msg.Y)
			return m, nil
		}
		hadSelection := m.sel.has
		m.sel = selection{}
		if m.inLogs(msg.X, msg.Y) && len(m.plain) > 0 {
			m.focus = focusLogs
			p := m.contentPos(msg.X, msg.Y)
			m.sel = selection{dragging: true, anchor: p, head: p}
		}
		if hadSelection {
			m.applyContent()
		}

	case tea.MouseActionMotion:
		if !m.sel.dragging {
			return m, nil
		}
		_, oy := m.logOrigin()
		switch {
		case msg.Y < oy:
			m.vp.ScrollUp(1)
		case msg.Y >= oy+m.vp.Height:
			m.vp.ScrollDown(1)
		}
		m.sel.head = m.contentPos(msg.X, msg.Y)
		m.sel.has = m.sel.head != m.sel.anchor
		m.follow = false
		m.applyContent()

	case tea.MouseActionRelease:
		if !m.sel.dragging {
			return m, nil
		}
		m.sel.dragging = false
		if !m.sel.has {
			return m, nil
		}
		text := m.selectedText()
		return m, copyCmd(text, describeCopy(text))
	}
	return m, nil
}

func (m *model) clickSidebar(y int) {
	m.focus = focusSidebar
	const firstItemRow = headerHeight + 1 + 2
	if y < firstItemRow {
		return
	}
	first, capacity := m.sidebarWindow()
	i := first + (y-firstItemRow)/itemHeight
	if i < first+capacity && i < len(m.opts.Items) {
		m.cursor = i
	}
}

// selectedText returns the plain text under the selection, one visual line per line.
func (m *model) selectedText() string {
	start, end := m.sel.bounds()
	var out []string
	for i := start.line; i <= end.line && i < len(m.plain); i++ {
		line := m.plain[i]
		w := ansi.StringWidth(line)
		from, to := 0, w
		if i == start.line {
			from = min(start.col, w)
		}
		if i == end.line {
			to = min(end.col+1, w)
		}
		if from > to {
			from = to
		}
		out = append(out, strings.TrimRight(ansi.Cut(line, from, to), " "))
	}
	return strings.Join(out, "\n")
}

// visibleText returns every visible log line as plain, unwrapped text.
func (m *model) visibleText() string {
	var b strings.Builder
	for _, e := range m.entries {
		if m.hideHealth && e.health {
			continue
		}
		b.WriteString(e.raw)
		b.WriteByte('\n')
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func describeCopy(text string) string {
	if n := strings.Count(text, "\n") + 1; n > 1 {
		return fmt.Sprintf("%d lines", n)
	}
	return fmt.Sprintf("%d characters", len([]rune(text)))
}

// copyAll copies the selection if there is one, otherwise all visible lines.
func (m *model) copyAll() tea.Cmd {
	if m.sel.has {
		text := m.selectedText()
		return copyCmd(text, describeCopy(text))
	}
	text := m.visibleText()
	if text == "" {
		m.setFlash("Nothing to copy yet.", true)
		return nil
	}
	return copyCmd(text, fmt.Sprintf("%d log lines", m.visibleCount()))
}

func (m *model) clearSelection() {
	if m.sel.has || m.sel.dragging {
		m.sel = selection{}
		m.applyContent()
	}
}

// applyContent pushes the visual lines, with any selection highlighted, to the viewport.
func (m *model) applyContent() {
	if m.visual == nil {
		return
	}
	lines := m.visual
	if m.sel.has {
		lines = append([]string(nil), m.visual...)
		start, end := m.sel.bounds()
		for i := start.line; i <= end.line && i < len(lines); i++ {
			w := ansi.StringWidth(m.plain[i])
			from, to := 0, w
			if i == start.line {
				from = min(start.col, w)
			}
			if i == end.line {
				to = min(end.col+1, w)
			}
			lines[i] = highlight(lines[i], m.plain[i], from, to)
		}
	}
	m.vp.SetContent(strings.Join(lines, "\n"))
}

func highlight(styled, plain string, from, to int) string {
	if from >= to {
		return styled
	}
	return ansi.Truncate(styled, from, "") + "\x1b[0m" + sSelection.Render(ansi.Cut(plain, from, to)) + ansi.TruncateLeft(styled, to, "")
}
