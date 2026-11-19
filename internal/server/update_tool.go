package server

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dvalfrid/proton-mail-mcp/internal/fetch"
	"github.com/dvalfrid/proton-mail-mcp/internal/update"
)

// UpdateInput takes nothing: the address asked is fixed.
type UpdateInput struct{}

// The names people use for this program, so Claude links a short question
// to these tools rather than to Claude or Claude Desktop (issue #125).
const knownAs = "Also called the Proton Mail connector, the connection to Proton Mail, Mail MCP or the mail connection " +
	"(in Swedish: kopplingen till Proton Mail, mejlkopplingen)."

const updateNote = "Tell the owner the message. If an update is available, give the release page link and how_to_update; " +
	"never download or run anything yourself."

func registerUpdateTool(s *mcp.Server, version string, updates update.Source) {
	open := true
	mcp.AddTool(s, &mcp.Tool{
		Name: "proton_mail_mcp_update",
		Description: "Proton Mail connector (proton-mail-mcp): is there a newer version of this program? " + knownAs + " " +
			"Use it only when the owner asks whether there is an update. It asks GitHub once for the latest release " +
			"(" + fetch.ReleasesPage + "; release candidates only if the owner chose the test channel), sending nothing but the program's name, and says whether it is newer than the running version, with how to update. " +
			"Never downloads or installs anything. Does not touch the mailbox. For the installed version alone, use about, which needs no internet.",
		Annotations: &mcp.ToolAnnotations{Title: "Check for a Proton Mail MCP update", ReadOnlyHint: true, OpenWorldHint: &open},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ UpdateInput) (*mcp.CallToolResult, UpdateOutput, error) {
		if updates == nil {
			return nil, UpdateOutput{}, errors.New("checking for updates is not available in this build")
		}
		r, err := update.Check(ctx, updates, version)
		if err != nil {
			return nil, UpdateOutput{}, errors.New(err.Error() + ". Try again later, or see the release page: " + fetch.ReleasesPage)
		}
		return nil, UpdateOutput{Result: r, Note: updateNote}, nil
	})
}

// UpdateOutput is the answer and how to show it.
type UpdateOutput struct {
	update.Result
	Note string `json:"note"`
}
