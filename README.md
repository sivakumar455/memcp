# memcp — Agent Memory System for MCP

**memcp** is a persistent memory layer for AI coding assistants, built as an [MCP (Model Context Protocol)](https://modelcontextprotocol.io/) server. It gives LLM agents the ability to remember findings, evolve their persona, and build domain expertise across chat sessions.

## Features

- **Persistent Memory** — Save and recall findings across sessions via SQLite (FTS5 full-text search)
- **Soul/Persona System** — Three-file persona (SOUL.md, IDENTITY.md, MEMORY.md) that evolves as the agent works
- **ADD/UPDATE/NOOP Pipeline** — Smart deduplication: new facts are added, existing facts are merged, redundant facts are skipped
- **Credential Sanitization** — Passwords, tokens, and secrets are auto-redacted before storage
- **Tiered Context Recall** — Budget-aware context assembly from persona, working memory, findings, and history
- **Session Management** — Organize work into named sessions

## Quick Start

### Build

```bash
make build
```

### Register as MCP Server

Add to your IDE's MCP configuration (e.g., Cursor, VS Code):

```json
{
  "mcpServers": {
    "memcp": {
      "command": "/path/to/memcp",
      "env": { "MEMCP_CONFIG": "standalone" }
    }
  }
}
```

### MCP Tools

| Tool | Description |
|------|-------------|
| `agent_recall` | Recall context from persistent memory (call FIRST every conversation) |
| `agent_save` | Save a finding to persistent memory |
| `agent_session` | Manage chat sessions (list, create, switch) |

### Example Usage

```
# Agent recalls context at start of conversation
agent_recall(query="timeout certificate staging")

# Agent saves a discovery
agent_save(key="service-x-rootcause", content="Root cause: expired certificate", tags="rootcause,timeout", importance=2)

# Agent manages sessions
agent_session(operation="create", name="PROJ-1234-investigation")
```

## Configuration

Configuration is loaded from `configs/standalone.yaml`. Override with environment variables:

| Env Var | Description |
|---------|-------------|
| `MEMCP_CONFIG` | Config file name (default: `standalone`) |
| `MEMCP_DATA_DIR` | Data directory for DB, soul files, logs |
| `MEMCP_LOG_LEVEL` | Log level: debug, info, warn, error |

## Project Structure

```
memcp/
├── main.go                    # Entry point
├── Makefile                   # Build targets
├── configs/
│   └── standalone.yaml        # Default config
├── soul/                      # Persona files
│   ├── SOUL.md                # Immutable core personality
│   ├── IDENTITY.md            # Evolving domain knowledge
│   └── MEMORY.md              # Auto-populated findings
├── internal/
│   ├── config/config.go       # Configuration
│   ├── memory/store.go        # SQLite store (CRUD, FTS5)
│   ├── engine/
│   │   ├── engine.go          # Core orchestrator
│   │   ├── memory_manager.go  # ADD/UPDATE/NOOP pipeline
│   │   └── context_builder.go # Tiered recall assembly
│   ├── session/manager.go     # Session lifecycle
│   └── mcp/server.go          # MCP server + tool handlers
└── data/                      # Auto-created
    └── memory.db              # SQLite database
```

