// Package outbox reads the files the owner wants attached to a draft from
// their outbox folder (issue #173). It is the only code in the server that
// reads the owner's files (a test checks this), and it can only read inside
// that one folder:
//
//   - a file is named by its plain name: no path, no hidden (dot) file, and
//     nothing a cleaned name wouldn't keep;
//   - it is opened with os.OpenInRoot, which refuses any path that leads
//     outside the folder, links included;
//   - only regular files, at most MaxFiles of them and MaxBytes together,
//     read in full or refused, never cut.
//
// The outbox holds only what the owner means to send, so instructions
// hidden in an email can't make the assistant attach any other file.
// Nothing here writes, creates or runs anything.
package outbox

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/dvalfrid/proton-mail-mcp/internal/save"
)

// Limits on what one draft attaches. Proton takes at most 25 MB of
// attachments per message; this stays under it.
const (
	MaxFiles = 10
	MaxBytes = 20 << 20
)

// ErrNoFolder means no outbox folder is set.
var ErrNoFolder = errors.New("no outbox folder is set: set PROTON_OUTBOX_DIR to a full path, or choose it in Proton Mail MCP Settings")

// ErrRefused means a file wasn't attached; nothing was read.
var ErrRefused = errors.New("the files were not attached")

// File is a file read from the outbox.
type File struct {
	Name        string // its name in the folder
	ContentType string
	Data        []byte
}

// Read reads the named files from dir, all or none.
func Read(dir string, names []string) ([]File, error) {
	if len(names) == 0 {
		return nil, nil
	}
	if dir == "" {
		return nil, ErrNoFolder
	}
	if len(names) > MaxFiles {
		return nil, fmt.Errorf("%w: %d files, at most %d", ErrRefused, len(names), MaxFiles)
	}
	seen := map[string]bool{}
	for i, name := range names {
		if err := checkName(name); err != nil {
			return nil, fmt.Errorf("%w: file %d %w", ErrRefused, i+1, err)
		}
		key := strings.ToLower(name)
		if seen[key] {
			return nil, fmt.Errorf("%w: %q is given twice", ErrRefused, name)
		}
		seen[key] = true
	}

	out := make([]File, 0, len(names))
	total := int64(0)
	for _, name := range names {
		data, err := readOne(dir, name, MaxBytes-total)
		if err != nil {
			return nil, err
		}
		total += int64(len(data))
		out = append(out, File{Name: name, ContentType: contentType(name), Data: data})
	}
	return out, nil
}

// checkName refuses anything but a plain, visible file name.
func checkName(name string) error {
	switch {
	case name == "":
		return errors.New("has no name")
	case strings.ContainsAny(name, `/\`):
		return errors.New("is a path: give only the file's name in the outbox folder")
	case strings.HasPrefix(name, "."):
		return errors.New("is a hidden file")
	case save.CleanName(name) != name:
		return errors.New("has characters a file name in the outbox can't have")
	}
	return nil
}

// readOne reads one regular file of at most limit bytes.
func readOne(dir, name string, limit int64) ([]byte, error) {
	f, err := os.OpenInRoot(dir, name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("%w: %q is not in the outbox folder (%s): save it there first", ErrRefused, name, dir)
	case err != nil:
		return nil, fmt.Errorf("%w: %q can't be opened in the outbox folder: %w", ErrRefused, name, err)
	}
	defer func() { _ = f.Close() }() // read-only

	info, err := f.Stat()
	switch {
	case err != nil:
		return nil, fmt.Errorf("%w: %q: %w", ErrRefused, name, err)
	case !info.Mode().IsRegular():
		return nil, fmt.Errorf("%w: %q is not a regular file", ErrRefused, name)
	case info.Size() > limit:
		return nil, fmt.Errorf("%w: the files are larger than %d MiB together", ErrRefused, MaxBytes>>20)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	switch {
	case err != nil:
		return nil, fmt.Errorf("%w: read %q: %w", ErrRefused, name, err)
	case int64(len(data)) > limit:
		return nil, fmt.Errorf("%w: the files are larger than %d MiB together", ErrRefused, MaxBytes>>20)
	case int64(len(data)) != info.Size():
		return nil, fmt.Errorf("%w: %q changed while it was read; try again", ErrRefused, name)
	}
	return data, nil
}

// contentType is the type for the file's extension, or a plain byte stream.
// Parameters (a charset the system adds) are left out.
func contentType(name string) string {
	t, _, _ := strings.Cut(mime.TypeByExtension(strings.ToLower(filepath.Ext(name))), ";")
	if t = strings.TrimSpace(t); t == "" || !strings.Contains(t, "/") {
		return "application/octet-stream"
	}
	return t
}
