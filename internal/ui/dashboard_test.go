package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/shreyas-shriyan/loggy/internal/config"
)

func testItems() []Item {
	return []Item{
		{Target: config.Target{Name: "orders-api", Env: "dev", Type: config.TypeEKS, Cluster: "c1", Namespace: "ns", Service: "orders-api"}, Region: "us-east-1"},
		{Target: config.Target{Name: "legacy-box", Env: "dev", Type: "vm"}, Region: "us-east-1"},
	}
}

// drive runs cmd and feeds resulting messages back into m until it settles.
func drive(t *testing.T, m *model, cmd tea.Cmd) {
	t.Helper()
	for i := 0; cmd != nil && i < 20; i++ {
		msg := cmd()
		if msg == nil {
			return
		}
		var next tea.Cmd
		_, next = m.Update(msg)
		cmd = next
	}
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestDashboardStreamsAndRenders(t *testing.T) {
	lines := []string{
		"[pod-a/app] 2026-11-19T10:21:00.123456789Z INFO started",
		"[pod-a/app] 2026-11-19T10:21:01.000000000Z ERROR db down",
		`[pod-a/app] 2026-11-19T10:21:02.000000000Z 10.0.0.1 "GET /health HTTP/1.1" 200`,
		`[pod-a/app] 2026-11-19T10:21:03.000000000Z {"level":"warn","msg":"slow"}`,
	}
	var gotReq Request
	start := func(ctx context.Context, tg config.Target, req Request) (Runner, error) {
		gotReq = req
		return func(ctx context.Context, sink func(string)) error {
			for _, l := range lines {
				sink(l)
			}
			return nil
		}, nil
	}
	m := newModel(Options{Env: "dev", Profile: "example-dev", Items: testItems(), Start: start})
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})

	_, cmd := m.Update(key("enter"))
	drive(t, m, cmd)
	// The runner goroutine may still be delivering; wait for the buffer.
	for i := 0; i < 50 && len(m.entries) < len(lines); i++ {
		time.Sleep(10 * time.Millisecond)
		drive(t, m, waitLines(m.stream))
	}

	if gotReq.Since != 15*time.Minute || gotReq.ShowHealth {
		t.Fatalf("request = %+v", gotReq)
	}
	view := ansi.Strip(m.View())
	if testing.Verbose() {
		t.Log("\n" + view)
	}
	for _, want := range []string{"loggy", "dev", "orders-api", "ERR", "db down", "WRN", `"msg": "slow"`, "health hidden", "3 lines"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "GET /health") {
		t.Error("health check line shown while hidden")
	}

	m.Update(key("h"))
	if view := ansi.Strip(m.View()); !strings.Contains(view, "GET /health") {
		t.Error("health check line hidden after toggle")
	}

	m.Update(key("J"))
	if view := ansi.Strip(m.View()); !strings.Contains(view, `{"level":"warn","msg":"slow"}`) {
		t.Error("compact JSON not shown after toggle")
	}

	for _, ln := range strings.Split(m.View(), "\n") {
		if w := ansi.StringWidth(ln); w > 140 {
			t.Fatalf("line wider than terminal (%d): %q", w, ansi.Strip(ln))
		}
	}
}

func TestDashboardActions(t *testing.T) {
	m := newModel(Options{Env: "dev", Items: testItems(), Start: func(context.Context, config.Target, Request) (Runner, error) {
		return nil, ErrNeedLogin
	}})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})

	_, cmd := m.Update(key("enter"))
	drive(t, m, cmd)
	if m.action.Kind != ActLogin || m.action.Target.Name != "orders-api" {
		t.Fatalf("action = %+v", m.action)
	}

	m = newModel(Options{Env: "dev", Items: testItems()})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m.Update(key("down"))
	if cmd := m.start(Request{Since: time.Minute}); cmd != nil || !strings.Contains(m.flash, "not supported") {
		t.Fatalf("unsupported start: cmd=%v flash=%q", cmd != nil, m.flash)
	}
	m.Update(key("c"))
	if m.action.Kind != ActCustom || m.action.Target.Name != "legacy-box" {
		t.Fatalf("custom action = %+v", m.action)
	}

	m = newModel(Options{Env: "dev"})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	if view := ansi.Strip(m.View()); !strings.Contains(view, "No saved servers yet") {
		t.Fatalf("empty state missing:\n%s", view)
	}
}
