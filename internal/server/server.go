// Package server builds the MCP server. Mail tools are registered here.
package server

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const Name = "proton-mail"

// New returns an MCP server with all tools registered.
func New(version string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: Name, Version: version}, nil)
	// Tools are added in the next step.
	return s
}
