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
	if l.Source != "api-7d9f/app" || l.FullSource != "[api-7d9f/app]" || l.Timestamp != "2026-11-19T10:21:00.123456789Z" || l.Message != "disk almost full" || l.Level != LevelWarn {
		t.Fatalf("got %+v", l)
	}
	if got := shortTime(l.Timestamp); got != "10:21:00.123" {
		t.Fatalf("shortTime = %q", got)
	}
}

func TestCompactPod(t *testing.T) {
	cases := map[string]string{
		"[orders-api-66984c59dc-mjg8g/orders-api]":  "mjg8g",
		"[orders-api-66984c59dc-mjg8g/istio-proxy]": "mjg8g/istio-proxy",
		"[node-agent-x7k2p/agent]":                  "x7k2p/agent",
		"[db-0/postgres]":                           "db-0/postgres",
		"[my-app-users/my-app]":                     "my-app-users/my-app",
	}
	for src, want := range cases {
		if got, _ := compactPod(src); got != want {
			t.Errorf("compactPod(%q) = %q, want %q", src, got, want)
		}
	}
}

func TestParseNest(t *testing.T) {
	raw := "[orders-api-66984c59dc-mjg8g/orders-api] 2026-11-20T05:43:50.754Z " +
		"\x1b[33m[App]\x1b[39m \x1b[32m1\x1b[39m 11/20/2026, 5:43:50 AM \x1b[32m    LOG\x1b[39m \x1b[32mHTTP GET /api/orders -\x1b[39m {\"res\":{\"statusCode\":200}}"
	l := Parse(raw)
	if l.Level != LevelInfo || l.Source != "mjg8g" || l.Message != `HTTP GET /api/orders - {"res":{"statusCode":200}}` {
		t.Fatalf("got %+v", l)
	}
	out := ansi.Strip(Format(l, FormatOptions{}))
	if want := `05:43:50.754 INF mjg8g HTTP GET /api/orders - {"res":{"statusCode":200}}`; out != want {
		t.Fatalf("compact = %q, want %q", out, want)
	}
	raw2 := ansi.Strip(Format(l, FormatOptions{Raw: true}))
	if !strings.Contains(raw2, "[orders-api-66984c59dc-mjg8g/orders-api] [App] 1 11/20/2026") {
		t.Fatalf("raw = %q", raw2)
	}

	for in, want := range map[string]Level{
		"[Nest] 42  - 11/20/2026, 5:43:50 AM   ERROR [OrdersService] lookup failed": LevelError,
		"[Nest] 42  - 11/20/2026, 5:43:50 AM    WARN [OrdersService] slow":          LevelWarn,
		"[Nest] 42  - 11/20/2026, 5:43:50 AM VERBOSE [Cache] miss":                  LevelDebug,
		"[Nest] 42  - 11/20/2026, 5:43:50 AM     LOG [Router] error page mapped":    LevelInfo,
	} {
		if got := Parse(in).Level; got != want {
			t.Errorf("Parse(%q).Level = %v, want %v", in, got, want)
		}
	}
}

func TestWarningsKeepFullLine(t *testing.T) {
	l := Parse("[orders-api-66984c59dc-mjg8g/orders-api] 2026-11-20T05:43:50.754Z [App] 1 11/20/2026, 5:43:50 AM   ERROR [Db] connection refused")
	out := ansi.Strip(Format(l, FormatOptions{}))
	if want := "05:43:50.754 ERR [orders-api-66984c59dc-mjg8g/orders-api] [App] 1 11/20/2026, 5:43:50 AM   ERROR [Db] connection refused"; out != want {
		t.Fatalf("got %q, want %q", out, want)
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
