package ui

import (
	"bytes"
	"encoding/json"
	"hash/fnv"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

	// NestJS console logger: [App] 1  - 11/20/2026, 5:43:50 AM     LOG [Context] message
	nestLine = regexp.MustCompile(`^\[[^\]]+\]\s+\d+\s+(?:-\s+)?[0-9/.\-]+,?\s+[0-9:]+(?:\s*[AP]M)?\s+(LOG|ERROR|WARN|DEBUG|VERBOSE|FATAL)\s+(.*)$`)

	// Kubernetes generated names: <deployment>-<replicaset hash>-<id> and <name>-<id>.
	deployPod = regexp.MustCompile(`^(.+)-[bcdfghjklmnpqrstvwxz2456789]{6,10}-([bcdfghjklmnpqrstvwxz2456789]{5})$`)
	shortPod  = regexp.MustCompile(`^(.+)-([bcdfghjklmnpqrstvwxz2456789]{5})$`)
)

var nestLevels = map[string]Level{
	"LOG":     LevelInfo,
	"VERBOSE": LevelDebug,
	"DEBUG":   LevelDebug,
	"WARN":    LevelWarn,
	"ERROR":   LevelError,
	"FATAL":   LevelError,
}

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
	case "debug", "trace", "verbose":
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

// Line is a parsed log line. Source and Message are the compact forms;
// FullSource and FullMessage keep the original text.
type Line struct {
	Source      string
	FullSource  string
	SourceKey   string
	Timestamp   string
	Message     string
	FullMessage string
	Level       Level
	JSON        map[string]any
}

// Parse splits raw into source prefix, timestamp and message, dropping
// prefixes that repeat what the badge and timestamp already show.
func Parse(raw string) Line {
	if strings.IndexByte(raw, '\x1b') >= 0 {
		raw = ansi.Strip(raw)
	}
	var l Line
	switch {
	case eksLine.MatchString(raw):
		m := eksLine.FindStringSubmatch(raw)
		l.FullSource, l.Timestamp, l.Message = m[1], m[2], m[3]
		l.Source, l.SourceKey = compactPod(m[1])
	case ecsLine.MatchString(raw):
		m := ecsLine.FindStringSubmatch(raw)
		l.Timestamp, l.FullSource, l.Message = m[1], m[2], m[3]
		l.Source, l.SourceKey = shortStream(m[2]), m[2]
	default:
		l.Message = raw
	}
	l.FullMessage = l.Message

	if m := nestLine.FindStringSubmatch(l.Message); m != nil {
		l.Level, l.Message = nestLevels[m[1]], m[2]
	}

	trimmed := strings.TrimSpace(l.Message)
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		var fields map[string]any
		if json.Unmarshal([]byte(trimmed), &fields) == nil {
			l.JSON = fields
			l.Message = trimmed
			if l.Level == LevelNone {
				l.Level = jsonLevel(fields)
			}
			return l
		}
	}
	if l.Level != LevelNone {
		return l
	}
	l.Level = DetectLevel(l.Message)
	if m := leadingLevel.FindStringSubmatch(l.Message); m != nil && levelFromWord(m[1]) == l.Level {
		l.Message = l.Message[len(m[0]):]
	}
	return l
}

// compactPod turns [orders-api-66984c59dc-mjg8g/orders-api] into mjg8g: the
// deployment name is already in the pane title, and the container is only
// kept when it differs from it. key identifies the pod for colouring.
func compactPod(src string) (short, key string) {
	inner := strings.TrimSuffix(strings.TrimPrefix(src, "["), "]")
	pod, container, _ := strings.Cut(inner, "/")
	base, id := pod, pod
	if m := deployPod.FindStringSubmatch(pod); m != nil {
		base, id = m[1], m[2]
	} else if m := shortPod.FindStringSubmatch(pod); m != nil {
		base, id = m[1], m[2]
	}
	short = id
	if container != "" && container != base {
		short += "/" + container
	}
	return short, pod
}

func sourceStyle(key string) lipgloss.Style {
	h := fnv.New32a()
	h.Write([]byte(key))
	return lipgloss.NewStyle().Foreground(colSources[h.Sum32()%uint32(len(colSources))])
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
	// Raw shows the original source and message for every line, not just
	// warnings and errors.
	Raw bool
}

// Format renders a parsed line, wrapped to opts.Width. The result may span
// several terminal lines.
func Format(l Line, opts FormatOptions) string {
	src, msg := l.Source, l.Message
	if opts.Raw || l.Level >= LevelWarn {
		src, msg = l.FullSource, l.FullMessage
	}

	var head strings.Builder
	if l.Timestamp != "" {
		head.WriteString(sMuted.Render(shortTime(l.Timestamp)))
		head.WriteByte(' ')
	}
	head.WriteString(badge(l.Level))
	head.WriteByte(' ')
	if src != "" {
		head.WriteString(sourceStyle(l.SourceKey).Render(src))
		head.WriteByte(' ')
	}

	msgStyle := messageStyle(l.Level)
	var body string
	if l.JSON != nil && opts.PrettyJSON {
		body = "\n" + prettyJSON(l.Message)
	} else {
		body = msgStyle.Render(msg)
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
