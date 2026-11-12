//go:build live

// Live tests: the real tools, end to end over MCP, against the owner's own
// Proton Mail Bridge. Run by hand only, never in CI:
//
//	go test -tags live ./internal/server -run Live -v
//
// They use the settings `proton-mail-mcp setup` saved and the password in the
// OS credential store, or a .env file named by PROTON_MCP_ENV_FILE.
//
// They are read-only: nothing in the mailbox changes. They print counts,
// uids, dates and timings only, never subjects, senders or bodies, so the
// output can be shared in an issue after a look.

package server

import (
	"context"
	"log/slog"
	"maps"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dvalfrid/proton-mail-mcp/internal/check"
	"github.com/dvalfrid/proton-mail-mcp/internal/config"
	"github.com/dvalfrid/proton-mail-mcp/internal/keystore"
	"github.com/dvalfrid/proton-mail-mcp/internal/mail"
)

// liveConfig loads the settings the server would use, or skips the test.
func liveConfig(t *testing.T) config.Config {
	t.Helper()
	if path := os.Getenv("PROTON_MCP_ENV_FILE"); path != "" {
		dotenv, err := config.ReadDotEnv(path)
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load(config.Lookup(dotenv))
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	dir, err := config.Dir()
	if err != nil {
		t.Fatal(err)
	}
	saved, err := config.ReadSettings(config.SettingsFile(dir))
	if err != nil {
		t.Fatal(err)
	}
	if saved.User == "" {
		t.Skip("run `proton-mail-mcp setup`, or set PROTON_MCP_ENV_FILE, to run the live tests")
	}
	files := config.FromMap(saved.Values())
	cfg, err := config.Load(config.Chain(files, config.PasswordFrom(files, keystore.OS{}.Get)))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// liveSession connects an in-memory MCP client to the real server, which
// talks to the real Bridge.
func liveSession(t *testing.T, cfg config.Config) *env {
	t.Helper()
	return liveSessionTimeout(t, cfg, 0)
}

// liveSessionTimeout is liveSession with another operation timeout; 0 is the
// default.
func liveSessionTimeout(t *testing.T, cfg config.Config, timeout time.Duration) *env {
	t.Helper()
	tlsCfg, err := mail.TLSConfig(cfg.Host, cfg.CertFile)
	if err != nil {
		t.Fatal(err)
	}
	m := mail.New(mail.Options{Addr: cfg.IMAPAddr(), User: cfg.User, Password: cfg.Password, TLSConfig: tlsCfg, Timeout: timeout, Logger: slog.New(slog.DiscardHandler)})
	t.Cleanup(func() { _ = m.Close() })
	ctx := context.Background()
	serverT, clientT := mcp.NewInMemoryTransports()
	ss, err := New("live", m).Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "live-test", Version: "test"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return &env{session: cs}
}

func folderNamed(t *testing.T, e *env, name string) mail.Folder {
	t.Helper()
	var out ListFoldersOutput
	e.callOK(t, "list_folders", map[string]any{}, &out)
	for _, f := range out.Folders {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("no folder %q", name)
	return mail.Folder{}
}

func TestLiveCheck(t *testing.T) {
	cfg := liveConfig(t)
	steps := check.Run(context.Background(), check.Input{Config: cfg, SettingsFile: os.Getenv("PROTON_MCP_ENV_FILE")})
	for _, s := range steps {
		if s.Name == "login" {
			continue // the detail names the account
		}
		t.Logf("%-22s %v", s.Name, s.Status == check.OK)
	}
	if check.Failed(steps) {
		t.Fatal("check failed: run `proton-mail-mcp check` for details")
	}
}

func TestLiveFolders(t *testing.T) {
	e := liveSession(t, liveConfig(t))
	var out ListFoldersOutput
	e.callOK(t, "list_folders", map[string]any{}, &out)
	roles := map[string]bool{}
	for _, f := range out.Folders {
		roles[f.Role] = true
	}
	for _, r := range []string{"inbox", "drafts", "sent", "trash", "spam"} {
		if !roles[r] {
			t.Errorf("no folder with role %q", r)
		}
	}
	t.Logf("%d folders", len(out.Folders))
}

// Newest first by date, uidvalidity present, and the list doesn't change
// what is unread.
func TestLiveListNewestFirst(t *testing.T) {
	e := liveSession(t, liveConfig(t))
	before := folderNamed(t, e, "INBOX")

	began := time.Now()
	var out ListMessagesOutput
	e.callOK(t, "list_messages", map[string]any{"folder": "INBOX", "limit": 20}, &out)
	t.Logf("INBOX: %d messages, listed 20 in %v", before.Messages, time.Since(began))

	if out.UIDValidity == 0 {
		t.Error("no uidvalidity")
	}
	// The order uses the earlier of received date and Date header, while
	// date shows the header, so allow a day of slack between neighbours.
	for i := 1; i < len(out.Messages); i++ {
		if out.Messages[i].Date.After(out.Messages[i-1].Date.Add(24 * time.Hour)) {
			t.Errorf("position %d (uid %d, %s) is newer than position %d (uid %d, %s)",
				i, out.Messages[i].UID, out.Messages[i].Date.UTC().Format(time.RFC3339),
				i-1, out.Messages[i-1].UID, out.Messages[i-1].Date.UTC().Format(time.RFC3339))
		}
	}
	if after := folderNamed(t, e, "INBOX"); after.Unread != before.Unread {
		t.Errorf("unread %d → %d after listing", before.Unread, after.Unread)
	}
}

// Reading an unread message leaves it unread; a stale uidvalidity is refused.
func TestLiveReadIsReadOnly(t *testing.T) {
	e := liveSession(t, liveConfig(t))
	var unread SearchMessagesOutput
	e.callOK(t, "search_messages", map[string]any{"folder": "INBOX", "unread_only": true, "limit": 1}, &unread)
	if len(unread.Messages) == 0 {
		t.Skip("no unread message in INBOX to test with")
	}
	uid := unread.Messages[0].UID
	before := folderNamed(t, e, "INBOX").Unread

	var msg ReadMessageOutput
	e.callOK(t, "read_message", map[string]any{"folder": "INBOX", "uid": uid, "uidvalidity": unread.UIDValidity}, &msg)
	if msg.UntrustedContent == nil || msg.Note == "" {
		t.Fatal("no untrusted_content or note")
	}
	t.Logf("read uid %d: %s body, %d characters, %d attachments, truncated %v",
		uid, msg.UntrustedContent.BodyFormat, len(msg.UntrustedContent.Body), len(msg.UntrustedContent.Attachments), msg.UntrustedContent.Truncated)

	var again SearchMessagesOutput
	e.callOK(t, "search_messages", map[string]any{"folder": "INBOX", "unread_only": true, "limit": 100}, &again)
	uids := make([]uint32, 0, len(again.Messages))
	for _, m := range again.Messages {
		uids = append(uids, m.UID)
	}
	if !slices.Contains(uids, uid) {
		t.Errorf("uid %d is no longer unread after read_message", uid)
	}
	if after := folderNamed(t, e, "INBOX").Unread; after != before {
		t.Errorf("unread %d → %d after reading", before, after)
	}

	res := e.call(t, "read_message", map[string]any{"folder": "INBOX", "uid": uid, "uidvalidity": unread.UIDValidity + 1})
	if !res.IsError {
		t.Error("a wrong uidvalidity was not refused")
	}
}

// Every search criterion is accepted by Bridge.
func TestLiveSearchCriteria(t *testing.T) {
	e := liveSession(t, liveConfig(t))
	month := time.Now().AddDate(0, -1, 0).Format(time.DateOnly)
	for _, args := range []map[string]any{
		{"from": "a"}, {"to": "a"}, {"subject": "a"}, {"body": "the"},
		{"since": month}, {"before": "2000-01-01"}, {"unread_only": true},
	} {
		args["folder"] = "INBOX"
		args["limit"] = 5
		began := time.Now()
		res := e.call(t, "search_messages", args)
		if res.IsError {
			t.Errorf("search %v: %s", args, text(res))
			continue
		}
		var out SearchMessagesOutput
		if err := decode(res, &out); err != nil {
			t.Fatal(err)
		}
		delete(args, "folder")
		delete(args, "limit")
		t.Logf("search %v: %d hits in %v", args, len(out.Messages), time.Since(began))
	}
}

// The largest folder still lists well within the operation timeout.
func TestLiveLargeFolder(t *testing.T) {
	e := liveSession(t, liveConfig(t))
	var folders ListFoldersOutput
	e.callOK(t, "list_folders", map[string]any{}, &folders)
	largest := slices.MaxFunc(folders.Folders, func(a, b mail.Folder) int { return int(a.Messages) - int(b.Messages) })

	began := time.Now()
	var out ListMessagesOutput
	e.callOK(t, "list_messages", map[string]any{"folder": largest.Name, "limit": 100}, &out)
	took := time.Since(began)
	t.Logf("largest folder: %d messages, listed 100 in %v", largest.Messages, took)
	if took > mail.DefaultTimeout/2 {
		t.Errorf("listing took %v, more than half the %v operation timeout", took, mail.DefaultTimeout)
	}
}

// How long text searches in the largest folder really take (issue #39). The
// timeout is raised so the time is measured instead of cut off at 30 s.
func TestLiveLargeFolderSearch(t *testing.T) {
	e := liveSessionTimeout(t, liveConfig(t), 5*time.Minute)
	var folders ListFoldersOutput
	e.callOK(t, "list_folders", map[string]any{}, &folders)
	largest := slices.MaxFunc(folders.Folders, func(a, b mail.Folder) int { return int(a.Messages) - int(b.Messages) })
	for _, args := range []map[string]any{{"from": "a"}, {"subject": "a"}, {"body": "the"}} {
		args["folder"] = largest.Name
		args["limit"] = 5
		began := time.Now()
		res := e.call(t, "search_messages", args)
		took := time.Since(began)
		delete(args, "folder")
		delete(args, "limit")
		if res.IsError {
			t.Errorf("search %v failed after %v: %s", args, took, text(res))
			continue
		}
		t.Logf("%d messages, search %v: %v", largest.Messages, args, took)
	}
}

// The organize tools on one test message the owner sent themself (issue
// #54). Set:
//
//	PROTON_LIVE_SUBJECT  an ASCII marker only that message's subject contains
//	PROTON_LIVE_LABEL    an existing label, e.g. Labels/Test
//	PROTON_LIVE_FOLDER   an existing folder, e.g. Folders/Test
//
// It refuses unless exactly one INBOX message matches. It marks the message
// read and unread, stars and unstars it, labels it, moves it to the folder
// and then to Trash, checking each step. Only uids, flags and folder names
// are logged, never the subject. The message ends in Trash, restorable.
func TestLiveOrganize(t *testing.T) {
	marker, label, folder := os.Getenv("PROTON_LIVE_SUBJECT"), os.Getenv("PROTON_LIVE_LABEL"), os.Getenv("PROTON_LIVE_FOLDER")
	if marker == "" || label == "" || folder == "" {
		t.Skip("set PROTON_LIVE_SUBJECT, PROTON_LIVE_LABEL and PROTON_LIVE_FOLDER to run the organize live test")
	}
	e := liveSession(t, liveConfig(t))
	find := func(f string) (SearchMessagesOutput, mail.Summary, bool) {
		t.Helper()
		var out SearchMessagesOutput
		e.callOK(t, "search_messages", map[string]any{"folder": f, "subject": marker, "limit": 5}, &out)
		if len(out.Messages) > 1 {
			t.Fatalf("%d messages in %s match the marker; it must be unique", len(out.Messages), f)
		}
		if len(out.Messages) == 0 {
			return out, mail.Summary{}, false
		}
		return out, out.Messages[0], true
	}
	change := func(tool, f string, extra map[string]any) {
		t.Helper()
		l, m, ok := find(f)
		if !ok {
			t.Fatalf("the test message is not in %s before %s", f, tool)
		}
		args := map[string]any{"folder": f, "uids": []uint32{m.UID}, "uidvalidity": l.UIDValidity}
		maps.Copy(args, extra)
		var out ChangeOutput
		e.callOK(t, tool, args, &out)
		t.Logf("%s: uid %d in %s, changed %d", tool, m.UID, f, out.Changed)
	}

	if _, m, ok := find("INBOX"); !ok {
		t.Fatal("no INBOX message matches PROTON_LIVE_SUBJECT")
	} else {
		t.Logf("start: uid %d, unread %v, flagged %v", m.UID, m.Unread, m.Flagged)
	}
	steps := []struct {
		tool  string
		extra map[string]any
		check func(m mail.Summary) bool
	}{
		{"mark_read", nil, func(m mail.Summary) bool { return !m.Unread }},
		{"mark_unread", nil, func(m mail.Summary) bool { return m.Unread }},
		{"flag", nil, func(m mail.Summary) bool { return m.Flagged }},
		{"unflag", nil, func(m mail.Summary) bool { return !m.Flagged }},
	}
	for _, s := range steps {
		change(s.tool, "INBOX", s.extra)
		if _, m, ok := find("INBOX"); !ok || !s.check(m) {
			t.Fatalf("after %s: found %v, unread %v, flagged %v", s.tool, ok, m.Unread, m.Flagged)
		}
	}

	change("add_label", "INBOX", map[string]any{"label": label})
	if _, _, ok := find(label); !ok {
		t.Fatalf("not under %s after add_label", label)
	}
	if _, _, ok := find("INBOX"); !ok {
		t.Fatal("add_label took the message out of INBOX")
	}

	change("move_message", "INBOX", map[string]any{"to": folder})
	if _, _, ok := find(folder); !ok {
		t.Fatalf("not in %s after move_message", folder)
	}
	if _, _, ok := find("INBOX"); ok {
		t.Fatal("still in INBOX after move_message")
	}

	change("trash_message", folder, nil)
	if _, _, ok := find("Trash"); !ok {
		t.Fatal("not in Trash after trash_message")
	}
	if _, _, ok := find(folder); ok {
		t.Fatalf("still in %s after trash_message", folder)
	}
	_, _, labelled := find(label)
	t.Logf("in Trash; still under %s: %v", label, labelled)
}
