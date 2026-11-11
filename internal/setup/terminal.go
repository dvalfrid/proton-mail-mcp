package setup

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// Terminal is the Prompter for a real terminal: it reads lines from in and
// the password with echo off (golang.org/x/term).
type Terminal struct {
	In  *os.File
	Out io.Writer
	r   *bufio.Reader
}

func (t *Terminal) reader() *bufio.Reader {
	if t.r == nil {
		t.r = bufio.NewReader(t.In)
	}
	return t.r
}

// Line implements Prompter.
func (t *Terminal) Line(prompt, def string) (string, error) {
	if def != "" {
		prompt += " [" + def + "]"
	}
	_, _ = fmt.Fprint(t.Out, prompt+": ") // a broken terminal shows up on the read
	s, err := t.reader().ReadString('\n')
	if err != nil && (!errors.Is(err, io.EOF) || s == "") {
		return "", fmt.Errorf("read answer: %w", err)
	}
	if s = strings.TrimSpace(s); s == "" {
		return def, nil
	}
	return s, nil
}

// Secret implements Prompter. It refuses to read a password from something
// that isn't a terminal, where echo can't be turned off.
func (t *Terminal) Secret(prompt string) (string, error) {
	fd := int(t.In.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("the password must be typed in a terminal; run setup in one")
	}
	_, _ = fmt.Fprint(t.Out, prompt+": ") // a broken terminal shows up on the read
	b, err := term.ReadPassword(fd)
	_, _ = fmt.Fprintln(t.Out)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

// Confirm implements Prompter.
func (t *Terminal) Confirm(prompt string, def bool) (bool, error) {
	hint := "y/N"
	if def {
		hint = "Y/n"
	}
	s, err := t.Line(prompt+" ("+hint+")", "")
	if err != nil {
		return false, err
	}
	switch strings.ToLower(s) {
	case "":
		return def, nil
	case "y", "yes", "j", "ja":
		return true, nil
	default:
		return false, nil
	}
}
