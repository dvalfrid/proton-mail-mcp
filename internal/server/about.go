package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dvalfrid/proton-mail-mcp/internal/catalog"
	"github.com/dvalfrid/proton-mail-mcp/internal/config"
)

// AboutInput takes nothing.
type AboutInput struct{}

// AboutOutput is the server's version and what its tools do, in words for
// the owner.
type AboutOutput struct {
	Name    string          `json:"name"`
	Version string          `json:"version" jsonschema:"the running server's version"`
	Groups  []catalog.Group `json:"groups" jsonschema:"every tool, grouped by what it may do"`
	// Settings are the owner's options in effect. Only the owner changes
	// them, outside Claude (issue #158).
	Settings []config.Option `json:"settings,omitempty" jsonschema:"the owner's options in effect and where each came from"`
	Note     string          `json:"note"`
}

const aboutNote = "Show the owner the version, then the tools as a short list per group: name and its one line. Don't add the tools' full descriptions. " +
	"If the owner asks about the settings, show them with their values; only the owner changes them, outside Claude, then restarts Claude: " +
	"on Windows on the Settings page of Proton Mail MCP in the Start menu, elsewhere in config.json in the settings folder; one whose origin is .env or environment is changed there instead."

func registerAboutTool(s *mcp.Server, version string, settings []config.Option) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "about",
		Description: "Which version of proton-mail-mcp is running, and every tool in one line, grouped. Use it when the owner asks which version runs or what the tools can do. Does not touch the mailbox.",
		Annotations: readOnly("About Proton Mail MCP"),
	}, func(context.Context, *mcp.CallToolRequest, AboutInput) (*mcp.CallToolResult, AboutOutput, error) {
		return nil, aboutOutput(version, settings), nil
	})
}

func aboutOutput(version string, settings []config.Option) AboutOutput {
	return AboutOutput{Name: "proton-mail-mcp", Version: version, Groups: catalog.Groups, Settings: settings, Note: aboutNote}
}
