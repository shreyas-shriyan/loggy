package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestSidebarItemDesign(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	m := newModel(Options{Env: "dev", Items: testItems()})
	m.stream = &streamState{id: 1, target: testItems()[0].Target, active: true}
	item := testItems()[0]
	item.Unsaved = true

	for _, width := range []int{40, 12} {
		sel := m.viewItem(item, true, width)
		other := m.viewItem(item, false, width)
		for i := range 2 {
			if s := ansi.Strip(sel[i]); !strings.HasPrefix(s, "┃ ") {
				t.Errorf("width %d: selected line %d = %q, want a bar", width, i, s)
			}
			if s := ansi.Strip(other[i]); !strings.HasPrefix(s, "  ") {
				t.Errorf("width %d: unselected line %d = %q, want indent", width, i, s)
			}
			for _, l := range []string{sel[i], other[i]} {
				if w := ansi.StringWidth(l); w > width {
					t.Errorf("width %d: line %q is %d wide", width, ansi.Strip(l), w)
				}
				if strings.Contains(l, "[48;") {
					t.Errorf("line %q paints a background", ansi.Strip(l))
				}
			}
		}
	}

	if s := ansi.Strip(m.viewItem(item, false, 40)[0]); !strings.Contains(s, "◌") {
		t.Errorf("connecting item = %q, want ◌", s)
	}
	m.stream.status = "live"
	green := sBold.Foreground(colOK).Render(item.Target.Primary())
	for _, sel := range []bool{false, true} {
		if l := m.viewItem(item, sel, 40)[0]; !strings.Contains(l, green) {
			t.Errorf("live item (selected=%v) name is not green: %q", sel, l)
		}
	}
	if l := m.viewItem(testItems()[1], false, 40)[0]; strings.Contains(l, sBold.Foreground(colOK).Render(testItems()[1].Target.Primary())) {
		t.Errorf("idle item is green: %q", l)
	}

	focused := m.viewItem(item, true, 40)[0]
	m.focus = focusLogs
	if unfocused := m.viewItem(item, true, 40)[0]; unfocused == focused {
		t.Error("selected item looks the same with and without sidebar focus")
	}
}
