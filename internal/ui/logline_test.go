package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestDetectLevel(t *testing.T) {
	cases := map[string]Level{
		"ERROR failed to connect":                                  LevelError,
		"2026/11/19 [warn] slow query":                             LevelWarn,
		"INFO request ok error=nil":                                LevelInfo,
		"debug cache miss":                                         LevelDebug,
		"GET /api 200 12ms":                                        LevelNone,
		"something happened, java.lang.NullPointer Exception here": LevelError,
		"0 errors reported":                                        LevelNone,
	}
	for msg, want := range cases {
		if got := DetectLevel(msg); got != want {
			t.Errorf("DetectLevel(%q) = %v, want %v", msg, got, want)
		}
	}
}

func TestParseEKS(t *testing.T) {
	l := Parse("[api-7d9f/app] 2026-11-19T10:21:00.123456789Z WARN disk almost full")
	if l.Source != "[api-7d9f/app]" || l.Timestamp != "2026-11-19T10:21:00.123456789Z" || l.Message != "disk almost full" || l.Level != LevelWarn {
		t.Fatalf("got %+v", l)
	}
	if got := shortTime(l.Timestamp); got != "10:21:00.123" {
		t.Fatalf("shortTime = %q", got)
	}
}

func TestParseECSJSON(t *testing.T) {
	l := Parse(`2026-11-19T10:21:00.123Z ecs/worker/0123456789abcdef {"level":"error","msg":"boom"}`)
	if l.Source != "worker/01234567" || l.JSON == nil || l.Level != LevelError {
		t.Fatalf("got %+v", l)
	}
	plain := ansi.Strip(Format(l, FormatOptions{PrettyJSON: true}))
	if !strings.Contains(plain, `"msg": "boom"`) || !strings.Contains(plain, "ERR") {
		t.Fatalf("pretty = %q", plain)
	}
	compact := ansi.Strip(Format(l, FormatOptions{}))
	if strings.Contains(compact, "\n") {
		t.Fatalf("compact has newline: %q", compact)
	}
}

func TestFormatWraps(t *testing.T) {
	l := Parse(strings.Repeat("word ", 40))
	out := Format(l, FormatOptions{Width: 30})
	for _, ln := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(ln); w > 30 {
			t.Fatalf("line width %d > 30: %q", w, ln)
		}
	}
}
