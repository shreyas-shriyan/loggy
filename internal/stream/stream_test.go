package stream

import (
	"strings"
	"testing"
	"time"
)

func TestFilter(t *testing.T) {
	f, err := NewFilter(true, "")
	if err != nil {
		t.Fatal(err)
	}
	drop := []string{
		`10.0.0.1 - - "GET /health HTTP/1.1" 200`,
		`"GET /healthz HTTP/1.0" 200`,
		`"GET /readyz HTTP/1.1" 200 kube-probe/1.30`,
		`agent ELB-HealthChecker/2.0`,
	}
	for _, l := range drop {
		if f.Keep(l) {
			t.Errorf("kept %q", l)
		}
	}
	for _, l := range []string{`"GET /api/users HTTP/1.1" 200`, `"GET /v2/health HTTP/1.1" 200`} {
		if !f.Keep(l) {
			t.Errorf("dropped %q", l)
		}
	}

	f, _ = NewFilter(false, "(?i)error")
	if !f.Keep("ERROR boom") || f.Keep("all good") || !f.Keep(`GET /health HTTP/1.1 error`) {
		t.Error("regex filter mismatch")
	}
	if _, err := NewFilter(false, "("); err == nil {
		t.Error("expected regex error")
	}
}

func TestSetHealthPatterns(t *testing.T) {
	t.Cleanup(func() { _ = SetHealthPatterns(nil) })
	if err := SetHealthPatterns([]string{`GET /v2/health`, `GET /ping`}); err != nil {
		t.Fatal(err)
	}
	if !IsHealthCheck(`"GET /v2/health HTTP/1.1"`) || !IsHealthCheck("GET /ping") || IsHealthCheck("GET /health HTTP/1.1") {
		t.Error("custom patterns not applied")
	}
	if err := SetHealthPatterns([]string{"("}); err == nil {
		t.Error("expected error for invalid pattern")
	}
	if !IsHealthCheck("GET /ping") {
		t.Error("invalid patterns replaced the previous ones")
	}
}

func TestSink(t *testing.T) {
	var b strings.Builder
	f, _ := NewFilter(true, "")
	s := NewSink(&b, f)
	s.Write("a")
	s.Write("GET /health HTTP/1.1")
	s.Write("b")
	if b.String() != "a\nb\n" || s.Lines() != 2 {
		t.Fatalf("got %q (%d lines)", b.String(), s.Lines())
	}
}

func TestParseSince(t *testing.T) {
	cases := map[string]time.Duration{"15m": 15 * time.Minute, "2h": 2 * time.Hour, "3d": 72 * time.Hour, " 45M ": 45 * time.Minute}
	for in, want := range cases {
		got, err := ParseSince(in)
		if err != nil || got != want {
			t.Errorf("ParseSince(%q) = %v, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "abc", "-5m", "0d"} {
		if _, err := ParseSince(bad); err == nil {
			t.Errorf("ParseSince(%q) succeeded", bad)
		}
	}
}

func TestSanitizeLabel(t *testing.T) {
	if got := SanitizeLabel("dev/orders api:svc"); got != "dev_orders_api_svc" {
		t.Fatalf("got %q", got)
	}
	if got := SanitizeLabel("///"); got != "logs" {
		t.Fatalf("got %q", got)
	}
}
