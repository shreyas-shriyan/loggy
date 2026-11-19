// Package stream filters log lines and writes them to the terminal or a file.
package stream

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// DefaultHealthPatterns match common load-balancer and Kubernetes probe requests.
var DefaultHealthPatterns = []string{
	`GET /health(z|check)? HTTP/1\.[01]`,
	`GET /(livez|readyz) HTTP/1\.[01]`,
	`ELB-HealthChecker`,
	`kube-probe/`,
}

var healthCheck atomic.Pointer[regexp.Regexp]

func init() {
	if err := SetHealthPatterns(nil); err != nil {
		panic(err)
	}
}

// SetHealthPatterns replaces the health-check regexes; empty restores the defaults.
func SetHealthPatterns(patterns []string) error {
	if len(patterns) == 0 {
		patterns = DefaultHealthPatterns
	}
	re, err := regexp.Compile("(?:" + strings.Join(patterns, ")|(?:") + ")")
	if err != nil {
		return fmt.Errorf("invalid health_patterns: %w", err)
	}
	healthCheck.Store(re)
	return nil
}

// IsHealthCheck reports whether line is load-balancer or health-probe noise.
func IsHealthCheck(line string) bool { return healthCheck.Load().MatchString(line) }

// Filter decides which log lines are kept.
type Filter struct {
	HideHealth bool
	Pattern    *regexp.Regexp
}

// NewFilter builds a Filter; pattern may be empty.
func NewFilter(hideHealth bool, pattern string) (Filter, error) {
	f := Filter{HideHealth: hideHealth}
	if pattern != "" {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return f, fmt.Errorf("invalid filter regex: %w", err)
		}
		f.Pattern = re
	}
	return f, nil
}

// Keep reports whether line passes the filter.
func (f Filter) Keep(line string) bool {
	if f.HideHealth && IsHealthCheck(line) {
		return false
	}
	if f.Pattern != nil && !f.Pattern.MatchString(line) {
		return false
	}
	return true
}

// Sink writes filtered lines to w and is safe for concurrent use.
type Sink struct {
	mu     sync.Mutex
	w      io.Writer
	filter Filter
	lines  int
	err    error
}

// NewSink returns a Sink writing to w.
func NewSink(w io.Writer, f Filter) *Sink { return &Sink{w: w, filter: f} }

// Write emits line if it passes the filter.
func (s *Sink) Write(line string) {
	if !s.filter.Keep(line) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return
	}
	if _, err := io.WriteString(s.w, line+"\n"); err != nil {
		s.err = err
		return
	}
	s.lines++
}

// Lines returns how many lines were written.
func (s *Sink) Lines() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lines
}

// Err returns the first write error.
func (s *Sink) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// SanitizeLabel makes s safe to use in a file name.
func SanitizeLabel(s string) string {
	s = strings.Trim(unsafeChars.ReplaceAllString(s, "_"), "_.")
	if s == "" {
		return "logs"
	}
	return s
}

// CreateLogFile creates dir/<label>-<timestamp>.log.
func CreateLogFile(dir, label string, now time.Time) (*os.File, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	name := fmt.Sprintf("%s-%s.log", SanitizeLabel(label), now.Format("20060102-150405"))
	return os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
}

// Interruptible returns a context cancelled by Ctrl+C or SIGTERM. Call stop
// to restore default signal handling.
func Interruptible(parent context.Context) (ctx context.Context, stop context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
}

// ParseSince parses durations such as 15m, 2h or 3d.
func ParseSince(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid duration %q (use e.g. 45m, 6h, 2d)", s)
	}
	return d, nil
}
