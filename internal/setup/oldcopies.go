package setup

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
)

// oldCopy is a program the Windows installer renamed during an update
// because it was running (NAME.old0, .old1, ...): Claude keeps the server
// open until it restarts (issue #139).
var oldCopy = regexp.MustCompile(`^proton-mail-mcp(-setup)?\.exe\.old[0-9]+$`)

// RemoveOldCopies deletes, in dir, the copies an update left behind because
// they were still running, and nothing else. A copy still running can't be
// deleted: it stays, and the next start (or the next sign-in, which the
// installer arranges) removes it. It reports how many are left.
func RemoveOldCopies(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	left := 0
	var errs []error
	for _, e := range entries {
		if !e.Type().IsRegular() || !oldCopy.MatchString(e.Name()) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			left++
			errs = append(errs, err)
		}
	}
	return left, errors.Join(errs...)
}
