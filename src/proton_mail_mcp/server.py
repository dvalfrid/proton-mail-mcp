"""MCP server entry point. Tools are added in the next step."""

from mcp.server.mcpserver import MCPServer

mcp = MCPServer("proton-mail")


def main() -> None:
    mcp.run()


if __name__ == "__main__":
    main()
