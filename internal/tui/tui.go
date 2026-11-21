// Package tui wraps huh prompts used by loggy.
package tui

import (
	"errors"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/shreyas-shriyan/loggy/internal/config"
)

// ErrBack is returned when the user cancels a prompt with Ctrl+C.
var ErrBack = errors.New("back")

// Separator is the key of a non-selectable divider row.
const Separator = "__separator__"

var (
	bold  = lipgloss.NewStyle().Bold(true)
	faint = lipgloss.NewStyle().Faint(true)
)

// Opt is a selectable menu row.
type Opt struct {
	Key   string
	Label string
}

// SeparatorOpt returns a divider row.
func SeparatorOpt() Opt {
	return Opt{Key: Separator, Label: faint.Render(strings.Repeat("─", 48))}
}

// Faint renders s dimmed.
func Faint(s string) string { return faint.Render(s) }

// TargetLabel renders a target with its leaf name in bold and the hierarchy dimmed.
func TargetLabel(t config.Target, region string) string {
	return bold.Render(t.Primary()) + "  " + faint.Render("— "+t.Secondary(region))
}

func run(fields ...huh.Field) error {
	err := huh.NewForm(huh.NewGroup(fields...)).WithShowHelp(true).Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return ErrBack
	}
	return err
}

func height(n int) int {
	if n > 14 {
		return 16
	}
	return 0
}

// Select shows a single-choice menu and returns the chosen key. Separator rows
// cannot be chosen.
func Select(title string, opts []Opt) (string, error) {
	hopts := make([]huh.Option[string], len(opts))
	for i, o := range opts {
		hopts[i] = huh.NewOption(o.Label, o.Key)
	}
	for {
		var v string
		s := huh.NewSelect[string]().Title(title).Options(hopts...).Value(&v)
		if h := height(len(opts)); h > 0 {
			s = s.Height(h)
		}
		if err := run(s); err != nil {
			return "", err
		}
		if v != Separator {
			return v, nil
		}
	}
}

// SelectStrings shows a menu whose keys and labels are the same values.
func SelectStrings(title string, values []string, label func(string) string) (string, error) {
	opts := make([]Opt, len(values))
	for i, v := range values {
		l := v
		if label != nil {
			l = label(v)
		}
		opts[i] = Opt{Key: v, Label: l}
	}
	return Select(title, opts)
}

// MultiSelect shows a multi-choice menu and returns the chosen keys.
func MultiSelect(title string, opts []Opt) ([]string, error) {
	hopts := make([]huh.Option[string], len(opts))
	for i, o := range opts {
		hopts[i] = huh.NewOption(o.Label, o.Key)
	}
	var v []string
	m := huh.NewMultiSelect[string]().Title(title).Options(hopts...).Value(&v)
	if h := height(len(opts)); h > 0 {
		m = m.Height(h)
	}
	if err := run(m); err != nil {
		return nil, err
	}
	return v, nil
}

// Input prompts for text, pre-filled with value.
func Input(title, value, placeholder string, validate func(string) error) (string, error) {
	v := value
	in := huh.NewInput().Title(title).Placeholder(placeholder).Value(&v)
	if validate != nil {
		in = in.Validate(validate)
	}
	if err := run(in); err != nil {
		return "", err
	}
	return strings.TrimSpace(v), nil
}

// Password prompts for text without echoing it.
func Password(title string, validate func(string) error) (string, error) {
	var v string
	in := huh.NewInput().Title(title).EchoMode(huh.EchoModePassword).Value(&v)
	if validate != nil {
		in = in.Validate(validate)
	}
	if err := run(in); err != nil {
		return "", err
	}
	return strings.TrimSpace(v), nil
}

// Confirm asks a yes/no question.
func Confirm(title string, def bool) (bool, error) {
	v := def
	if err := run(huh.NewConfirm().Title(title).Affirmative("Yes").Negative("No").Value(&v)); err != nil {
		return false, err
	}
	return v, nil
}
