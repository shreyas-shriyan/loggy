package ui

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Level is a detected log severity.
type Level int

// Log levels, from least to most severe.
const (
	LevelNone Level = iota
	LevelDebug
	LevelInfo
	LevelWarn
	LevelError
)

var (
	// [pod/container] 2026-11-19T10:21:00.123456789Z message
	eksLine = regexp.MustCompile(`^(\[[^\]]+\]) (\d{4}-\d\d-\d\dT[0-9:.]+Z) ?(.*)$`)
	// 2026-11-19T10:21:00.123Z stream-name message
	ecsLine = regexp.MustCompile(`^(\d{4}-\d\d-\d\dT[0-9:.]+Z) (\S+) ?(.*)$`)

	levelToken = regexp.MustCompile(`(?i)\b(fatal|panic|crit(?:ical)?|error|err|warn(?:ing)?|info|debug|trace)\b`)
	// leadingLevel matches a level word at the start of a message, which the badge replaces.
	leadingLevel = regexp.MustCompile(`(?i)^\[?(fatal|panic|crit(?:ical)?|error|err|warn(?:ing)?|info|debug|trace)\]?(?::\s*|\s+)`)
	errorWord    = regexp.MustCompile(`(?i)\b(fatal|panic|error|exception|traceback)\b`)
	jsonKey      = regexp.MustCompile(`^(\s*)("(?:[^"\\]|\\.)*")(:)`)
)

// levelWindow is how far into a message a leading level token is looked for.
const levelWindow = 48

func levelFromWord(w string) Level {
	switch strings.ToLower(w) {
	case "fatal", "panic", "crit", "critical", "error", "err":
		return LevelError
	case "warn", "warning":
		return LevelWarn
	case "info":
		return LevelInfo
	case "debug", "trace":
		return LevelDebug
	}
	return LevelNone
}

// DetectLevel guesses the severity of a plain-text message: a level token near
// the start wins, otherwise any error-like word marks it as an error.
func DetectLevel(msg string) Level {
	head := msg
	if len(head) > levelWindow {
		head = head[:levelWindow]
	}
	if m := levelToken.FindString(head); m != "" {
		return levelFromWord(m)
	}
	if errorWord.MatchString(msg) {
		return LevelError
	}
	return LevelNone
}

func jsonLevel(fields map[string]any) Level {
	for _, k := range []string{"level", "severity", "lvl", "log.level", "loglevel"} {
		if v, ok := fields[k].(string); ok {
			if l := levelFromWord(v); l != LevelNone {
				return l
			}
			if strings.EqualFold(v, "warning") {
				return LevelWarn
			}
		}
	}
	return LevelNone
}

// Line is a parsed log line.
type Line struct {
	Source    string
	Timestamp string
	Message   string
	Level     Level
	JSON      map[string]any
}

// Parse splits raw into source prefix, timestamp and message.
func Parse(raw string) Line {
	var l Line
	switch {
	case eksLine.MatchString(raw):
		m := eksLine.FindStringSubmatch(raw)
		l.Source, l.Timestamp, l.Message = m[1], m[2], m[3]
	case ecsLine.MatchString(raw):
		m := ecsLine.FindStringSubmatch(raw)
		l.Timestamp, l.Source, l.Message = m[1], shortStream(m[2]), m[3]
	default:
		l.Message = raw
	}

	trimmed := strings.TrimSpace(l.Message)
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		var fields map[string]any
		if json.Unmarshal([]byte(trimmed), &fields) == nil {
			l.JSON = fields
			l.Message = trimmed
			l.Level = jsonLevel(fields)
			return l
		}
	}
	l.Level = DetectLevel(l.Message)
	if m := leadingLevel.FindStringSubmatch(l.Message); m != nil && levelFromWord(m[1]) == l.Level {
		l.Message = l.Message[len(m[0]):]
	}
	return l
}

// shortStream abbreviates ECS stream names like prefix/container/0123456789abcdef.
func shortStream(s string) string {
	parts := strings.Split(s, "/")
	if len(parts) >= 3 {
		id := parts[len(parts)-1]
		if len(id) > 8 {
			id = id[:8]
		}
		return parts[len(parts)-2] + "/" + id
	}
	return s
}

// shortTime turns 2026-11-19T10:21:00.123456789Z into 10:21:00.123.
func shortTime(ts string) string {
	i := strings.IndexByte(ts, 'T')
	if i < 0 {
		return ts
	}
	t := strings.TrimSuffix(ts[i+1:], "Z")
	if dot := strings.IndexByte(t, '.'); dot >= 0 && len(t) > dot+4 {
		t = t[:dot+4]
	}
	return t
}

func badge(l Level) string {
	switch l {
	case LevelError:
		return sBadgeErr.Render("ERR")
	case LevelWarn:
		return sBadgeWrn.Render("WRN")
	case LevelInfo:
		return sBadgeInf.Render("INF")
	case LevelDebug:
		return sBadgeDbg.Render("DBG")
	}
	return "   "
}

func messageStyle(l Level) lipgloss.Style {
	switch l {
	case LevelError:
		return sError
	case LevelWarn:
		return sWarn
	case LevelDebug:
		return sMuted
	}
	return lipgloss.NewStyle()
}

// FormatOptions controls rendering.
type FormatOptions struct {
	Width      int
	PrettyJSON bool
}

// Format renders a parsed line, wrapped to opts.Width. The result may span
// several terminal lines.
func Format(l Line, opts FormatOptions) string {
	var head strings.Builder
	if l.Timestamp != "" {
		head.WriteString(sMuted.Render(shortTime(l.Timestamp)))
		head.WriteByte(' ')
	}
	head.WriteString(badge(l.Level))
	head.WriteByte(' ')
	if l.Source != "" {
		head.WriteString(sMuted.Render(l.Source))
		head.WriteByte(' ')
	}

	msgStyle := messageStyle(l.Level)
	var body string
	if l.JSON != nil && opts.PrettyJSON {
		body = "\n" + prettyJSON(l.Message)
	} else {
		body = msgStyle.Render(l.Message)
	}

	out := head.String() + body
	if opts.Width > 0 {
		out = lipgloss.NewStyle().Width(opts.Width).Render(out)
	}
	return out
}

func prettyJSON(s string) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(s), "  ", "  "); err != nil {
		return s
	}
	lines := strings.Split(buf.String(), "\n")
	for i, ln := range lines {
		if m := jsonKey.FindStringSubmatchIndex(ln); m != nil {
			lines[i] = ln[:m[3]] + sJSONKy.Render(ln[m[4]:m[5]]) + ln[m[6]:]
		}
		if i == 0 {
			lines[i] = "  " + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}
