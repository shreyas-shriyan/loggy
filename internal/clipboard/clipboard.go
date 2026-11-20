// Package clipboard copies text to the system clipboard.
package clipboard

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"unicode/utf16"
)

type tool struct {
	name    string
	args    []string
	utf16le bool
}

// Copy puts text on the clipboard and returns the mechanism used. When no
// clipboard tool is available it falls back to an OSC 52 escape written to tty.
func Copy(text string, tty io.Writer) (string, error) {
	for _, t := range tools() {
		if _, err := exec.LookPath(t.name); err != nil {
			continue
		}
		in := []byte(text)
		if t.utf16le {
			in = encodeUTF16LE(text)
		}
		cmd := exec.Command(t.name, t.args...)
		cmd.Stdin = bytes.NewReader(in)
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("%s: %v %s", t.name, err, strings.TrimSpace(string(out)))
		}
		return t.name, nil
	}
	if tty == nil {
		return "", errors.New("no clipboard tool found")
	}
	if _, err := fmt.Fprintf(tty, "\x1b]52;c;%s\a", base64.StdEncoding.EncodeToString([]byte(text))); err != nil {
		return "", err
	}
	return "terminal (OSC 52)", nil
}

func tools() []tool {
	switch {
	case runtime.GOOS == "darwin":
		return []tool{{name: "pbcopy"}}
	case runtime.GOOS == "windows":
		return []tool{{name: "clip.exe", utf16le: true}}
	case isWSL():
		return []tool{{name: "clip.exe", utf16le: true}}
	}
	var ts []tool
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		ts = append(ts, tool{name: "wl-copy"})
	}
	if os.Getenv("DISPLAY") != "" {
		ts = append(ts, tool{name: "xclip", args: []string{"-selection", "clipboard"}}, tool{name: "xsel", args: []string{"--clipboard", "--input"}})
	}
	return ts
}

func isWSL() bool {
	if os.Getenv("WSL_DISTRO_NAME") != "" {
		return true
	}
	b, err := os.ReadFile("/proc/version")
	return err == nil && bytes.Contains(bytes.ToLower(b), []byte("microsoft"))
}

// encodeUTF16LE encodes s as UTF-16LE with a BOM, which clip.exe reads without
// mangling non-ASCII text.
func encodeUTF16LE(s string) []byte {
	u := utf16.Encode([]rune(strings.ReplaceAll(s, "\n", "\r\n")))
	out := make([]byte, 2, 2+2*len(u))
	out[0], out[1] = 0xFF, 0xFE
	for _, c := range u {
		out = append(out, byte(c), byte(c>>8))
	}
	return out
}
