package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// AboutInput takes nothing.
type AboutInput struct{}

// AboutOutput is the server's version and what its tools do, in words for
// the owner.
type AboutOutput struct {
	Name    string      `json:"name"`
	Version string      `json:"version" jsonschema:"the running server's version"`
	Groups  []toolGroup `json:"groups" jsonschema:"every tool, grouped by what it may do"`
	Note    string      `json:"note"`
}

type toolGroup struct {
	Group string     `json:"group"`
	Tools []toolLine `json:"tools"`
}

type toolLine struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// catalog is every tool in one line each, for the owner rather than for
// Claude. TestAboutListsEveryTool keeps it in step with the registered tools.
var catalog = []toolGroup{
	{"Read (changes nothing)", []toolLine{
		{"about", "Show this server's version and this list."},
		{"list_folders", "List the folders and labels, with the number of unread messages."},
		{"list_messages", "List the newest messages in a folder."},
		{"search_messages", "Search a folder by sender, recipient, subject, text or date."},
		{"read_message", "Read one message, with its list of attachments. It stays unread."},
		{"view_image", "Show an image attached to or embedded in a message."},
		{"fetch_image", "Fetch and show an image a message links to on the internet, when you ask (off unless switched on)."},
	}},
	{"Organize (syncs to Proton, deletes nothing)", []toolLine{
		{"mark_read", "Mark messages as read."},
		{"mark_unread", "Mark messages as unread."},
		{"flag", "Star messages."},
		{"unflag", "Remove the star from messages."},
		{"move_message", "Move messages to another folder."},
		{"add_label", "Add a label to messages."},
		{"remove_label", "Remove a label from messages; they stay in their folder."},
		{"trash_message", "Move messages, or drafts, to Trash, where they can be restored."},
		{"mark_spam", "Mark messages as spam when you ask; Proton can delete Spam after 30 days."},
	}},
	{"Drafts (nothing is sent)", []toolLine{
		{"create_draft", "Write a new email as a draft in Drafts."},
		{"create_reply_draft", "Write a reply to a message as a draft, with the original quoted."},
		{"update_draft", "Replace a draft with a new version; the old one goes to Trash."},
	}},
	{"Files", []toolLine{
		{"save_attachment", "Save an attachment into the attachment folder, without opening it."},
	}},
}

const aboutNote = "Show the owner the version, then the tools as a short list per group: name and its one line. Don't add the tools' full descriptions."

func registerAboutTool(s *mcp.Server, version string) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "about",
		Description: "Which version of proton-mail-mcp is running, and every tool in one line, grouped. Use it when the owner asks which version runs or what the tools can do. Does not touch the mailbox.",
		Annotations: readOnly("About Proton Mail MCP"),
	}, func(context.Context, *mcp.CallToolRequest, AboutInput) (*mcp.CallToolResult, AboutOutput, error) {
		return nil, AboutOutput{Name: "proton-mail-mcp", Version: version, Groups: catalog, Note: aboutNote}, nil
	})
}
