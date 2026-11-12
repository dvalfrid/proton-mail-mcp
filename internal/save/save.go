// Package save writes an attachment into the owner's attachment folder. It
// is the only code in the server that writes files (a guardrail test checks
// this), and it can only write inside that one folder:
//
//   - the name is the sender's, so it is cleaned to a plain file name;
//   - the file is created through an os.Root, which refuses any path that
//     leads outside the folder, links included;
//   - an existing file is never replaced: a number is added instead;
//   - on Windows the file gets the Mark of the Web, as a browser download
//     does, so Windows and Office warn before running or editing it.
//
// Nothing here opens or runs what it saved.
package save

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits on a saved file's name.
const (
	maxNameRunes = 150
	maxExtRunes  = 20
	// maxNameBytes keeps a name, with " (1000)" and a "_" added, under the
	// 255 bytes Linux and macOS allow (and the 255 UTF-16 units of Windows):
	// 150 runes of "ö" are 300 bytes.
	maxNameBytes = 200
	// maxNumbered is how many "name (n).ext" are tried before giving up.
	maxNumbered = 1000
)

// fallbackName is used when nothing of the sender's name is left.
const fallbackName = "attachment"

// ErrNoFolder means no attachment folder is set.
var ErrNoFolder = errors.New("no attachment folder is set: set PROTON_ATTACHMENT_DIR to a full path")

// Saved is a file that was written.
type Saved struct {
	Path string // full path of the file
	Name string // its name in the folder: the cleaned name, maybe numbered
}

// File writes data into dir under the cleaned name, creating dir if needed.
// It never replaces a file.
func File(dir, name string, data []byte) (Saved, error) {
	if dir == "" {
		return Saved{}, ErrNoFolder
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Saved{}, fmt.Errorf("create the attachment folder: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Saved{}, fmt.Errorf("open the attachment folder: %w", err)
	}
	defer func() { _ = root.Close() }() // a directory handle: nothing to flush

	clean := CleanName(name)
	stem, ext := splitExt(clean)
	for n := 1; n <= maxNumbered; n++ {
		candidate := clean
		if n > 1 {
			candidate = fmt.Sprintf("%s (%d)%s", stem, n, ext)
		}
		f, err := root.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return Saved{}, fmt.Errorf("create the file: %w", err)
		}
		saved := Saved{Path: filepath.Join(dir, candidate), Name: candidate}
		if err := write(f, data); err != nil {
			_ = root.Remove(candidate) // don't leave a partial file behind
			return Saved{}, err
		}
		if err := markOfTheWeb(saved.Path); err != nil {
			_ = root.Remove(candidate) // a file without the mark must not stay
			return Saved{}, fmt.Errorf("mark the file as downloaded from the internet: %w", err)
		}
		return saved, nil
	}
	return Saved{}, fmt.Errorf("%d files named like %q already exist in the attachment folder", maxNumbered, clean)
}

func write(f *os.File, data []byte) error {
	_, err := f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("write the file: %w", err)
	}
	return nil
}

// CleanName turns a sender's file name into a plain name that is safe on
// Windows, macOS and Linux: only the last path part is kept; control,
// format (bidi, zero-width) and reserved characters are removed; leading and
// trailing dots and spaces go; a reserved Windows device name gets a "_"
// in front; and the length is capped, keeping the extension.
func CleanName(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r), r == utf8.RuneError:
			return -1
		case strings.ContainsRune(`<>:"/\|?*`, r):
			return '_'
		case unicode.IsSpace(r):
			return ' '
		}
		return r
	}, name)
	name = strings.Join(strings.Fields(name), " ")
	name = strings.Trim(name, ". ")
	if name == "" {
		return fallbackName
	}

	stem, ext := splitExt(name)
	if r := []rune(ext); len(r) > maxExtRunes {
		ext = string(r[:maxExtRunes])
	}
	if r := []rune(stem); len(r)+utf8.RuneCountInString(ext) > maxNameRunes {
		stem = string(r[:maxNameRunes-utf8.RuneCountInString(ext)])
	}
	for len(stem)+len(ext) > maxNameBytes {
		_, size := utf8.DecodeLastRuneInString(stem)
		stem = stem[:len(stem)-size]
	}
	stem = strings.TrimRight(stem, ". ")
	if stem == "" {
		stem = fallbackName
	}
	if reservedOnWindows(stem) {
		stem = "_" + stem
	}
	return stem + ext
}

// splitExt splits "report.final.pdf" into "report.final" and ".pdf". A name
// that is only an extension (".pdf", after cleaning never) has no stem.
func splitExt(name string) (string, string) {
	ext := filepath.Ext(name)
	if ext == "." || strings.ContainsRune(ext, ' ') {
		ext = ""
	}
	return strings.TrimSuffix(name, ext), ext
}

// reservedOnWindows reports whether stem is a device name Windows won't
// create a file under, with any extension: CON, PRN, AUX, NUL, COM0-9,
// LPT0-9 (also with superscript digits). Windows also ignores what follows
// the first dot and trailing spaces, so "con.tar" and "nul " are reserved.
func reservedOnWindows(stem string) bool {
	base, _, _ := strings.Cut(stem, ".")
	base = strings.ToUpper(strings.TrimRight(base, " "))
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return true
	}
	if r := []rune(base); len(r) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) {
		return strings.ContainsRune("0123456789¹²³", r[3])
	}
	return false
}
