// Package catalog lists the server's tools in words for the owner.
package catalog

// Group is the tools that may do one kind of thing.
type Group struct {
	Group string `json:"group"`
	Tools []Tool `json:"tools"`
}

// Tool is one tool, in one line for the owner.
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Groups is every tool in one line each, for the owner rather than for
// Claude: the about tool and the setup window's About page show it.
// server.TestAboutListsEveryTool keeps it in step with the registered tools.
var Groups = []Group{
	{"Read (changes nothing)", []Tool{
		{"about", "Show the installed version, whether Bridge answers, how to update, and this list."},
		{"proton_mail_mcp_update", "Ask GitHub whether a newer version exists, when you ask."},
		{"list_folders", "List the folders and labels, with the number of unread messages."},
		{"list_messages", "List the newest messages in a folder, with their labels."},
		{"search_messages", "Search a folder by sender, recipient, subject, text or date."},
		{"read_message", "Read one message, with its list of attachments. It stays unread."},
		{"view_image", "Show an image attached to or embedded in a message."},
		{"fetch_image", "Fetch and show an image a message links to on the internet, when you ask (off unless switched on)."},
	}},
	{"Organize (syncs to Proton, deletes nothing)", []Tool{
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
	{"Drafts (nothing is sent)", []Tool{
		{"create_draft", "Write a new email as a draft in Drafts."},
		{"create_reply_draft", "Write a reply to a message as a draft, with the original quoted."},
		{"update_draft", "Replace a draft with a new version; the old one goes to Trash."},
	}},
	{"Send (off unless you switch it on)", []Tool{
		{"send_draft", "Send a draft you have seen, when you ask; Claude asks you first. At most 10 per run."},
	}},
	{"Files", []Tool{
		{"save_attachment", "Save an attachment into the attachment folder, without opening it."},
	}},
}
