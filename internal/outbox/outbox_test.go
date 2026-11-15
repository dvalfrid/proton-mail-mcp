package outbox

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, dir, "Guide.pdf", []byte("%PDF-1.7 test"))
	write(t, dir, "notes.txt", []byte("hello"))

	files, err := Read(dir, []string{"Guide.pdf", "notes.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Name != "Guide.pdf" || files[0].ContentType != "application/pdf" ||
		!bytes.Equal(files[0].Data, []byte("%PDF-1.7 test")) || files[1].ContentType != "text/plain" {
		t.Errorf("%+v", files)
	}

	if files, err := Read(dir, nil); err != nil || files != nil {
		t.Errorf("no names: %v, %v", files, err)
	}
	if _, err := Read("", []string{"notes.txt"}); !errors.Is(err, ErrNoFolder) {
		t.Errorf("no folder: %v", err)
	}
}

// Guardrail: only a plain, visible name in the folder is read; nothing that
// leads outside it, and nothing but regular files.
func TestReadStaysInFolder(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	write(t, parent, "secret.txt", []byte("private"))
	dir := filepath.Join(parent, "outbox")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, dir, ".hidden", []byte("x"))
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "sub"), "inner.txt", []byte("x"))

	for _, name := range []string{
		"../secret.txt", `..\secret.txt`, filepath.Join(parent, "secret.txt"), "sub/inner.txt", `sub\inner.txt`,
		".hidden", "sub", "", "missing.pdf", "a:b.txt", "report.pdf ", "con.txt",
	} {
		files, err := Read(dir, []string{name})
		if !errors.Is(err, ErrRefused) || files != nil {
			t.Errorf("%q: %v, %v; want a refusal", name, files, err)
		}
	}
}

// A link in the outbox to a file outside it is not followed.
func TestReadDoesNotFollowLinksOut(t *testing.T) {
	t.Parallel()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "report.txt")); err != nil {
		t.Skipf("can't create a link here: %v", err) // Windows without the privilege
	}
	if files, err := Read(dir, []string{"report.txt"}); !errors.Is(err, ErrRefused) || files != nil {
		t.Errorf("%v, %v; want a refusal", files, err)
	}
}

func TestReadLimits(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var many []string
	for i := range MaxFiles + 1 {
		name := strings.Repeat("a", i+1) + ".txt"
		write(t, dir, name, []byte("x"))
		many = append(many, name)
	}
	if _, err := Read(dir, many[:MaxFiles]); err != nil {
		t.Errorf("%d files: %v", MaxFiles, err)
	}
	if _, err := Read(dir, many); !errors.Is(err, ErrRefused) {
		t.Errorf("%d files: %v; want a refusal", len(many), err)
	}
	if _, err := Read(dir, []string{"a.txt", "A.txt"}); !errors.Is(err, ErrRefused) {
		t.Errorf("the same file twice: %v; want a refusal", err)
	}

	write(t, dir, "big1.bin", make([]byte, MaxBytes/2))
	write(t, dir, "big2.bin", make([]byte, MaxBytes/2+1))
	if _, err := Read(dir, []string{"big1.bin"}); err != nil {
		t.Errorf("half the limit: %v", err)
	}
	if _, err := Read(dir, []string{"big1.bin", "big2.bin"}); !errors.Is(err, ErrRefused) {
		t.Errorf("over the limit together: %v; want a refusal", err)
	}
}

func TestContentType(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]string{
		"a.pdf": "application/pdf", "a.PDF": "application/pdf", "a.png": "image/png",
		"a": "application/octet-stream", "a.unknownext": "application/octet-stream",
	} {
		if got := contentType(name); got != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}
}

// Only this package reads the owner's files for the tools: the server's
// other packages open no files but the settings, the certificate and their
// own. A new file read anywhere a tool can reach must be added here
// deliberately.
func TestNothingElseReadsFiles(t *testing.T) {
	t.Parallel()
	reads := regexp.MustCompile(`os\.(Open|ReadFile|OpenFile|OpenInRoot|OpenRoot|ReadDir|DirFS)\(|\.Open\(|\.ReadFile\(`)
	allowed := map[string]bool{
		"client.go": true, // internal/mail: the Bridge certificate, a path from the owner's settings
		"save.go":   true, // internal/save: os.OpenRoot on the attachment folder, to write
	}
	for _, dir := range []string{"../server", "../mail", "../fetch", "../save"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			if m := reads.Find(src); m != nil && !allowed[filepath.Base(f)] {
				t.Errorf("%s reads files (%s); only internal/outbox may", f, m)
			}
		}
	}
}
