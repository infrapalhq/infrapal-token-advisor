// The MCP server's prompts, which Claude Code lists as slash commands named
// /<server>:<prompt>. internal/mcpserver/prompts.go defines them.
export function slashCommands(serverName: string) {
  return [
    { command: `/${serverName}:review`, what: "Find where tokens went, and what to change first" },
    { command: `/${serverName}:session <session id>`, what: "Explain why one session cost what it did" },
    { command: `/${serverName}:compare`, what: "Compare this week with last week" },
  ];
}
