package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dvalfrid/proton-mail-mcp/internal/mail"
)

// ChangeInput are the arguments of the organize tools: messages in one
// folder, with the uidvalidity their uids came with.
type ChangeInput struct {
	Folder      string   `json:"folder" jsonschema:"the folder the messages are in, as list_folders names it"`
	UIDs        []uint32 `json:"uids" jsonschema:"the messages' uids, from list_messages or search_messages in this folder (1 to 50)"`
	UIDValidity uint32   `json:"uidvalidity" jsonschema:"the uidvalidity that came with the uids; required, so a folder renumbered by Bridge is detected instead of changing other messages"`
}

// ChangeOutput is the result of an organize tool.
type ChangeOutput struct {
	Folder  string `json:"folder"`
	Changed int    `json:"changed" jsonschema:"how many messages the call applied to"`
}

// organize annotates a tool that changes the mailbox in a way the owner can
// undo. destructive asks clients that honour it to confirm first.
func organize(title string, destructive bool) *mcp.ToolAnnotations {
	closed := false
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: false, DestructiveHint: &destructive, IdempotentHint: true, OpenWorldHint: &closed}
}

func selection(in ChangeInput) (mail.Selection, error) {
	if in.Folder == "" {
		return mail.Selection{}, errors.New("folder is required; call list_folders to see the names")
	}
	return mail.Selection{Folder: in.Folder, UIDs: in.UIDs, UIDValidity: in.UIDValidity}, nil
}

// changeError explains a refused change so Claude can recover.
func changeError(action string, in ChangeInput, err error) error {
	switch {
	case errors.Is(err, mail.ErrUIDValidityChanged):
		return fmt.Errorf("nothing was changed: %q was renumbered by Bridge since the uids were listed; list it again: %w", in.Folder, err)
	case errors.Is(err, mail.ErrMessagesMissing):
		return fmt.Errorf("nothing was changed: %w; list %q again", err, in.Folder)
	case errors.Is(err, mail.ErrNoUIDValidity):
		return fmt.Errorf("uidvalidity is required: pass the one list_messages or search_messages returned with the uids")
	case errors.Is(err, mail.ErrNoUIDs), errors.Is(err, mail.ErrTooManyUIDs):
		return fmt.Errorf("give 1 to %d uids: %w", mail.MaxChangeUIDs, err)
	}
	return fmt.Errorf("%s in %q: %w", action, in.Folder, err)
}

func registerOrganizeTools(s *mcp.Server, m *mail.Client) {
	flagTool := func(name, title, desc, action string, apply func(context.Context, mail.Selection) (int, error)) {
		mcp.AddTool(s, &mcp.Tool{
			Name:        name,
			Description: desc + " Applies to all the given messages or, if any uid is stale or missing, to none.",
			Annotations: organize(title, false),
		}, func(ctx context.Context, _ *mcp.CallToolRequest, in ChangeInput) (*mcp.CallToolResult, ChangeOutput, error) {
			sel, err := selection(in)
			if err != nil {
				return nil, ChangeOutput{}, err
			}
			n, err := apply(ctx, sel)
			if err != nil {
				return nil, ChangeOutput{}, changeError(action, in, err)
			}
			return nil, ChangeOutput{Folder: in.Folder, Changed: n}, nil
		})
	}
	flagTool("mark_read", "Mark as read", "Mark messages as read in Proton Mail.", "mark read",
		func(ctx context.Context, s mail.Selection) (int, error) { return m.MarkRead(ctx, s, true) })
	flagTool("mark_unread", "Mark as unread", "Mark messages as unread in Proton Mail.", "mark unread",
		func(ctx context.Context, s mail.Selection) (int, error) { return m.MarkRead(ctx, s, false) })
	flagTool("flag", "Star", "Star messages (Proton's Starred; flagged in IMAP).", "star",
		func(ctx context.Context, s mail.Selection) (int, error) { return m.Flag(ctx, s, true) })
	flagTool("unflag", "Unstar", "Remove the star from messages.", "unstar",
		func(ctx context.Context, s mail.Selection) (int, error) { return m.Flag(ctx, s, false) })
}
