package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func selectionModel(t *testing.T) (*model, *[]string) {
	t.Helper()
	var copied []string
	prev := CopyFunc
	CopyFunc = func(text string) (string, error) {
		copied = append(copied, text)
		return "test", nil
	}
	t.Cleanup(func() { CopyFunc = prev })

	m := newModel(Options{Env: "dev", Items: testItems()})
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	m.stream = &streamState{id: 1, target: testItems()[0].Target, status: "live", active: true}
	m.appendLines([]string{
		"[pod-a/app] 2026-11-19T10:21:00.123456789Z INFO started",
		"[pod-a/app] 2026-11-19T10:21:01.000000000Z ERROR db down",
	})
	return m, &copied
}

func mouse(action tea.MouseAction, x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: action, Button: tea.MouseButtonLeft}
}

// drag selects from (x1,y1) to (x2,y2) and returns the copied text.
func drag(t *testing.T, m *model, copied *[]string, x1, y1, x2, y2 int) string {
	t.Helper()
	m.Update(mouse(tea.MouseActionPress, x1, y1))
	m.Update(mouse(tea.MouseActionMotion, x2, y2))
	_, cmd := m.Update(mouse(tea.MouseActionRelease, x2, y2))
	if cmd == nil {
		t.Fatal("release did not trigger a copy")
	}
	m.Update(cmd())
	return (*copied)[len(*copied)-1]
}

func TestMouseSelectionCopiesPartOfALine(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	m, copied := selectionModel(t)
	ox, oy := m.logOrigin()
	if !strings.HasPrefix(m.plain[0], "10:21:00.123 INF pod-a/app started") {
		t.Fatalf("unexpected first line %q", m.plain[0])
	}

	if got := drag(t, m, copied, ox, oy, ox+11, oy); got != "10:21:00.123" {
		t.Fatalf("copied %q", got)
	}
	if !strings.Contains(m.flash, "Copied 12 characters") {
		t.Fatalf("flash = %q", m.flash)
	}
	if !strings.Contains(m.vp.View(), "\x1b[7m") {
		t.Error("selection is not highlighted")
	}
}

func TestMouseSelectionAcrossLinesBackwards(t *testing.T) {
	m, copied := selectionModel(t)
	ox, oy := m.logOrigin()
	// Drag from line 2 col 4 back to line 1 col 13.
	got := drag(t, m, copied, ox+4, oy+1, ox+13, oy)
	if got != "INF pod-a/app started\n10:21" {
		t.Fatalf("copied %q", got)
	}
}

func TestCopyAllAndClear(t *testing.T) {
	m, copied := selectionModel(t)
	_, cmd := m.Update(key("y"))
	m.Update(cmd())
	want := "[pod-a/app] 2026-11-19T10:21:00.123456789Z INFO started\n[pod-a/app] 2026-11-19T10:21:01.000000000Z ERROR db down"
	if got := (*copied)[0]; got != want {
		t.Fatalf("copy all = %q", got)
	}

	ox, oy := m.logOrigin()
	drag(t, m, copied, ox, oy, ox+3, oy)
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.sel.has || strings.Contains(m.vp.View(), "\x1b[7m") {
		t.Fatal("esc did not clear the selection")
	}
}

func TestClickWithoutDragDoesNotCopy(t *testing.T) {
	m, copied := selectionModel(t)
	ox, oy := m.logOrigin()
	m.Update(mouse(tea.MouseActionPress, ox+2, oy))
	if _, cmd := m.Update(mouse(tea.MouseActionRelease, ox+2, oy)); cmd != nil || len(*copied) != 0 {
		t.Fatal("plain click copied text")
	}
}

func TestClickSidebarSelectsItem(t *testing.T) {
	m, _ := selectionModel(t)
	m.Update(mouse(tea.MouseActionPress, 3, headerHeight+1+2+itemHeight))
	if m.cursor != 1 {
		t.Fatalf("cursor = %d", m.cursor)
	}
}

func TestSelectionKeepsLayout(t *testing.T) {
	m, copied := selectionModel(t)
	ox, oy := m.logOrigin()
	drag(t, m, copied, ox, oy, ox+20, oy+1)
	for _, ln := range strings.Split(m.View(), "\n") {
		if w := ansi.StringWidth(ln); w > 140 {
			t.Fatalf("line wider than terminal (%d): %q", w, ansi.Strip(ln))
		}
	}
}
