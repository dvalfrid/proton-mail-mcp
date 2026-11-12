// Package help links errors to their section in docs/troubleshooting.md.
package help

// Topic is a section of docs/troubleshooting.md, named by its heading's
// anchor. help_test checks that every topic has a heading.
type Topic string

// The sections an error can point to.
const (
	NotSetUp     Topic = "not-set-up"
	BridgeDown   Topic = "bridge-not-running"
	LoginFailed  Topic = "login-failed"
	Certificate  Topic = "certificate-does-not-match"
	Timeout      Topic = "timeout"
	Renumbered   Topic = "folder-renumbered"
	ChangeFailed Topic = "change-failed"
	ChangeLimit  Topic = "change-limit-reached"
	Internal     Topic = "internal-error"
	NoTools      Topic = "claude-does-not-show-the-tools"
	Logs         Topic = "where-the-logs-are"
	Troubleshoot Topic = ""
)

// Topics are all the topics, for the test.
var Topics = []Topic{NotSetUp, BridgeDown, LoginFailed, Certificate, Timeout, Renumbered, ChangeFailed, ChangeLimit, Internal, NoTools, Logs}

const page = "https://github.com/dvalfrid/proton-mail-mcp/blob/main/docs/troubleshooting.md"

// URL is the link to a topic's section; Troubleshoot is the whole page.
func URL(t Topic) string {
	if t == Troubleshoot {
		return page
	}
	return page + "#" + string(t)
}
