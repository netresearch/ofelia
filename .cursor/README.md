# Cursor MCP Configuration

This directory contains the configuration for Cursor's MCP (Model Context Protocol) integration.

## Setup Instructions

1. **Copy the template**: Copy `mcp.json.example` to `mcp.json`
   ```bash
   cp .cursor/mcp.json.example .cursor/mcp.json
   ```

2. **Add your API keys**: Edit `.cursor/mcp.json` and replace the placeholder values with your actual API keys:
   - `OPENAI_API_KEY`: Your OpenAI API key (format: `sk-...`)
   - `OPENROUTER_API_KEY`: Your OpenRouter API key (format: `sk-or-...`)
   - Other service credentials as needed

3. **Security Note**: The actual `mcp.json` file is git-ignored to prevent accidentally committing sensitive API keys.

## Available MCP Servers

- **taskmaster-ai**: Task management and development workflow automation
- **directmcp-atlassian**: Jira and Confluence integration (optional)

## File Structure

- `mcp.json.example` - Safe template (committed to repo)
- `mcp.json` - Your actual config with API keys (git-ignored)
- `rules/` - Development workflow and coding guidelines 