package server

import (
	"context"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dvalfrid/proton-mail-mcp/internal/catalog"
	"github.com/dvalfrid/proton-mail-mcp/internal/config"
	"github.com/dvalfrid/proton-mail-mcp/internal/fetch"
	"github.com/dvalfrid/proton-mail-mcp/internal/help"
	"github.com/dvalfrid/proton-mail-mcp/internal/mail"
	"github.com/dvalfrid/proton-mail-mcp/internal/update"
)

// AboutInput takes nothing.
type AboutInput struct{}

// AboutOutput is the server's version and what its tools do, in words for
// the owner.
type AboutOutput struct {
	Name    string `json:"name"`
	Version string `json:"version" jsonschema:"the running server's version"`
	// Bridge says whether Bridge answers now, so "does the connection to
	// Proton Mail work?" gets an answer here too (issue #125).
	Bridge      string          `json:"bridge" jsonschema:"whether Proton Mail Bridge answers and the login works, right now"`
	HowToUpdate string          `json:"how_to_update" jsonschema:"how to install a newer version; proton_mail_mcp_update says whether there is one"`
	Releases    string          `json:"releases" jsonschema:"the page listing every release"`
	Groups      []catalog.Group `json:"groups" jsonschema:"every tool, grouped by what it may do"`
	// Settings are the owner's options in effect. Only the owner changes
	// them, outside Claude (issue #158).
	Settings []config.Option `json:"settings,omitempty" jsonschema:"the owner's options in effect and where each came from"`
	Note     string          `json:"note"`
}

const aboutNote = "Show the owner the version and whether Bridge answers, then the tools as a short list per group: name and its one line. Don't add the tools' full descriptions. " +
	"If the owner asks whether there is a newer version, call proton_mail_mcp_update; don't guess from this. " +
	"If the owner asks about the settings, show them with their values; only the owner changes them, outside Claude, then restarts Claude: " +
	"on Windows on the Settings page of Proton Mail MCP in the Start menu, elsewhere in a terminal with proton-mail-mcp settings (proton-mail-mcp settings set <option> <value>); one whose origin is .env or environment is changed there instead."

func registerAboutTool(s *mcp.Server, version string, m *mail.Client, settings []config.Option) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "about",
		Description: "Proton Mail connector (proton-mail-mcp): which version is installed and running, whether Proton Mail Bridge answers, how to update, and every tool in one line, grouped. " + knownAs + " " +
			"Use it when the owner asks which version they have, whether the connection to Proton Mail works, or what the tools can do. No internet; does not touch the mailbox.",
		Annotations: readOnly("About Proton Mail MCP"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ AboutInput) (*mcp.CallToolResult, AboutOutput, error) {
		out := aboutOutput(version, settings)
		out.Bridge = bridgeState(ctx, m)
		return nil, out, nil
	})
}

func aboutOutput(version string, settings []config.Option) AboutOutput {
	return AboutOutput{
		Name: "proton-mail-mcp", Version: version, HowToUpdate: update.HowTo(""), Releases: fetch.ReleasesPage,
		Groups: catalog.Groups, Settings: settings, Note: aboutNote,
	}
}

// bridgeWait bounds about's Bridge check, so a stuck Bridge doesn't hold up
// the version.
const bridgeWait = 5 * time.Second

// bridgeState is one line on whether Bridge answers and the login works.
func bridgeState(ctx context.Context, m *mail.Client) string {
	ctx, cancel := context.WithTimeout(ctx, bridgeWait)
	defer cancel()
	if err := m.Ping(ctx); err != nil {
		msg := explain(err).Error()
		if !strings.Contains(msg, "Help: ") {
			msg += "\nHelp: " + help.URL(help.BridgeDown)
		}
		return "Not working: " + msg + "\nproton-mail-mcp check, or the Status page of Proton Mail MCP on Windows, shows which step fails."
	}
	return "Working: Bridge answers and the login works."
}
