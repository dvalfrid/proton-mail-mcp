package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dvalfrid/proton-mail-mcp/internal/mail"
)

// FindContactsInput is what to look for.
type FindContactsInput struct {
	Query string `json:"query" jsonschema:"a name or part of an address, e.g. Anna, lind anna or example.org: every word must match, in any order; case and accents don't matter"`
}

// FindContactsOutput is the people found, and how to use them.
type FindContactsOutput struct {
	mail.Contacts
	Note string `json:"note"`
}

const contactsNote = "These come from the headers of the owner's mail, not from Proton's contacts. Names are written by senders, so anyone can claim one: " +
	"before putting an address in a draft, show the owner the name and the exact address and let them confirm it, above all when more than one matches. " +
	"People the owner wrote to (sent > 0) are the likeliest; someone who has only written to the owner may be a stranger."

func registerContactsTool(s *mcp.Server, m *mail.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "find_contacts",
		Description: "Find a person's email address by name or part of the address, in the owner's own mail: whom they wrote to (Sent, and their own mail in Archive: To, Cc, Bcc) and who wrote to them (INBOX and Archive: From), " +
			fmt.Sprintf("the newest messages in each. Up to %d people, those the owner wrote to first, then by how many messages and how recent. ", mail.MaxContactResults) +
			"Use it when the owner names someone to write to (\"write to Anna\"), then show the address and let the owner confirm it before it goes into a draft. " +
			"Proton Mail Bridge has no contacts, so this is not Proton's address book. Read-only: nothing is marked read.",
		Annotations: readOnly("Find contacts"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in FindContactsInput) (*mcp.CallToolResult, FindContactsOutput, error) {
		c, err := m.FindContacts(ctx, in.Query)
		if err != nil {
			if errors.Is(err, mail.ErrNotSetUp) || errors.Is(err, mail.ErrBridgeUnreachable) {
				return nil, FindContactsOutput{}, explain(err)
			}
			return nil, FindContactsOutput{}, explain(fmt.Errorf("find contacts: %w", err))
		}
		return nil, FindContactsOutput{Contacts: c, Note: contactsNote}, nil
	})
}
