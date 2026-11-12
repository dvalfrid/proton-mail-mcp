package save

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCleanName(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a", 300) + ".pdf"
	for in, want := range map[string]string{
		"report.pdf":                      "report.pdf",
		"../../x.txt":                     "x.txt",
		`..\..\Windows\System32\x.dll`:    "x.dll",
		`C:\x`:                            "x",
		"C:x":                             "C_x",
		"/etc/passwd":                     "passwd",
		"..":                              fallbackName,
		"":                                fallbackName,
		"   ":                             fallbackName,
		"...":                             fallbackName,
		"CON":                             "_CON",
		"con.txt":                         "_con.txt",
		"nul .tar.gz":                     "_nul .tar.gz",
		"COM1.log":                        "_COM1.log",
		"lpt9":                            "_lpt9",
		"COM¹":                            "_COM¹",
		"CONSOLE.txt":                     "CONSOLE.txt",
		"bad\x00na\x1fme\n.txt":           "badname.txt",
		"invoice\u202efdp.exe":            "invoicefdp.exe",
		"zero\u200bwidth.png":             "zerowidth.png",
		`a<b>c:d"e|f?g*h.txt`:             "a_b_c_d_e_f_g_h.txt",
		".hidden":                         "hidden",
		"trailing. . .":                   "trailing",
		"report.pdf:Zone.Identifier":      "report.pdf_Zone.Identifier",
		"tab\there.txt":                   "tabhere.txt",
		"name." + strings.Repeat("x", 40): "name." + strings.Repeat("x", maxExtRunes-1),
	} {
		if got := CleanName(in); got != want {
			t.Errorf("CleanName(%q) = %q, want %q", in, got, want)
		}
	}
	got := CleanName(long)
	if utf8.RuneCountInString(got) != maxNameRunes || !strings.HasSuffix(got, ".pdf") {
		t.Errorf("long name = %q (%d runes)", got, utf8.RuneCountInString(got))
	}
}

// Whatever the name, the file lands directly in the folder, under a name
// that cleans to itself.
func TestFileStaysInFolder(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	dir := filepath.Join(parent, "attachments")
	for _, name := range []string{
		"../../x", "..\\..\\x", `C:\x`, "/abs/x", "CON", "aux.txt", strings.Repeat("ö", 500),
		"a\x00b", "..", ".", "", "x/../../y", "\u202e..\\..\\evil",
	} {
		saved, err := File(dir, name, []byte("data"))
		if err != nil {
			t.Errorf("%q: %v", name, err)
			continue
		}
		if filepath.Dir(saved.Path) != dir || filepath.Base(saved.Path) != saved.Name || CleanName(saved.Name) != saved.Name {
			t.Errorf("%q saved as %q (%q)", name, saved.Path, saved.Name)
		}
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 1 || entries[0].Name() != "attachments" {
		t.Errorf("parent folder holds %v (%v): something was written outside the attachment folder", entries, err)
	}
}

// An existing file is kept; the new one gets a number.
func TestFileNeverReplaces(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for i, want := range []string{"report.pdf", "report (2).pdf", "report (3).pdf"} {
		saved, err := File(dir, "report.pdf", []byte{byte(i)})
		if err != nil || saved.Name != want {
			t.Fatalf("save %d: %+v, %v; want %s", i, saved, err, want)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "report.pdf"))
	if err != nil || !bytes.Equal(data, []byte{0}) {
		t.Errorf("the first file was changed: %v %v", data, err)
	}
	saved, err := File(dir, "README", []byte("x"))
	if err != nil || saved.Name != "README" {
		t.Errorf("no extension: %+v %v", saved, err)
	}
	saved, err = File(dir, "README", []byte("x"))
	if err != nil || saved.Name != "README (2)" {
		t.Errorf("no extension, again: %+v %v", saved, err)
	}
}

// A link in the folder with the attachment's name is not followed: the
// file it points to, outside the folder, stays as it was.
func TestFileDoesNotFollowLinks(t *testing.T) {
	t.Parallel()
	outside := filepath.Join(t.TempDir(), "target.txt")
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "evil.txt")); err != nil {
		t.Skipf("can't create a link here: %v", err) // Windows without the privilege
	}
	saved, err := File(dir, "evil.txt", []byte("overwritten"))
	if err != nil || saved.Name != "evil (2).txt" {
		t.Errorf("saved %+v, %v", saved, err)
	}
	if data, _ := os.ReadFile(outside); string(data) != "keep" {
		t.Errorf("the file outside the folder now holds %q", data)
	}
}

func TestFileMarkOfTheWeb(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "windows" {
		t.Skip("the Mark of the Web is a Windows feature")
	}
	saved, err := File(t.TempDir(), "invoice.pdf", []byte("%PDF"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(saved.Path + ":Zone.Identifier")
	if err != nil || !strings.Contains(string(data), "ZoneId=3") {
		t.Errorf("Zone.Identifier = %q, %v", data, err)
	}
}

func TestFileNeedsFolder(t *testing.T) {
	t.Parallel()
	if _, err := File("", "x.txt", nil); !errors.Is(err, ErrNoFolder) {
		t.Errorf("err = %v", err)
	}
}

// Only this package writes files from the server. Setup writes its
// settings through internal/config/file.go and nothing else; tests don't
// count. A new file write anywhere else must be added here deliberately.
func TestNothingElseWritesFiles(t *testing.T) {
	t.Parallel()
	writes := regexp.MustCompile(`os\.(WriteFile|Create|CreateTemp|OpenFile|Mkdir|MkdirAll|MkdirTemp|Rename|Symlink|Link|Chmod|Truncate|OpenRoot)\(|\.OpenFile\(|\.WriteFile\(`)
	allowed := map[string]bool{
		"internal/save/save.go":         true,
		"internal/save/motw_windows.go": true,
		// Setup, which the owner runs, writes its settings and Claude's
		// configuration through config.WriteFile. The server never calls it.
		"internal/config/file.go":   true,
		"internal/setup/clients.go": true,
		"internal/setup/steps.go":   true,
		"internal/testutil/imap.go": true, // tests only
	}
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			// The program's code: internal/ and cmd/.
			if rel != "." && rel != "internal" && rel != "cmd" && !strings.HasPrefix(rel, "internal/") && !strings.HasPrefix(rel, "cmd/") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") || allowed[rel] {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if m := writes.Find(src); m != nil {
			t.Errorf("%s writes files (%s); only internal/save may", rel, m)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
