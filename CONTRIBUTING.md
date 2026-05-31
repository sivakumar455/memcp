# Contributing to memcp

Thanks for your interest in contributing to **memcp**! Whether it's a bug report, feature idea, or code contribution — all help is welcome.

## 🚀 Getting Started

1. **Fork** the repo and clone your fork:
   ```bash
   git clone https://github.com/<your-username>/memcp.git
   cd memcp
   ```

2. **Build** the project:
   ```bash
   make build
   ```

3. **Run** locally:
   ```bash
   ./memcp --config standalone
   ```

## 📂 Project Structure

```
memcp/
├── cmd/memcp/main.go       # Entry point (Cobra CLI, registers extensions)
├── configs/                # Default YAML config templates
├── soul/                   # Default persona files (SOUL.md, IDENTITY.md, MEMORY.md)
├── skills/                 # User-created domain skill files
├── extensions/             # Public extension watchers
│   └── webhook/            # Example: generic webhook watcher
├── site/                   # Organization-specific content (see below)
├── internal/
│   ├── config/             # Configuration loading
│   ├── daemon/             # Background task queue (core interfaces)
│   ├── engine/             # Core orchestrator
│   ├── memory/             # SQLite store (CRUD, FTS5)
│   ├── mcp/                # MCP server + tool handlers
│   ├── tools/              # MCP client utilities
│   ├── agent/              # Agent loop
│   ├── llm/                # LLM provider abstraction
│   ├── session/            # Session lifecycle
│   ├── persona/            # Soul/persona file loader
│   ├── observation/        # Tool call observer + fact extraction
│   ├── evolution/          # Soul evolution system
│   ├── skills/             # Domain skill routing
│   ├── gateway/            # HTTP gateway server
│   ├── shim/               # Transparent observation proxy
│   └── logger/             # Structured logging
├── scripts/setup.go        # Bootstrap + MCP client setup
└── .gitattributes          # Excludes site/ from public releases
```

## 🔌 Writing Extensions

Extensions are daemon watchers that poll external systems (Jira, email, etc.) and
produce `daemon.Event`s. They live in separate Go packages and are registered at
compile time via explicit imports in `cmd/memcp/main.go`.

To create a new extension:

1. Create a directory under `extensions/` (or `site/extensions/` for private use):
   ```
   extensions/slack/watcher.go
   ```

2. Implement the `daemon.Watcher` interface:
   ```go
   package slack

   import "github.com/sivakumar455/memcp/internal/daemon"

   type Watcher struct { /* ... */ }

   func NewWatcher(/* config */) *Watcher { /* ... */ }
   func (w *Watcher) Name() string              { return "slack" }
   func (w *Watcher) Poll() ([]daemon.Event, error) { /* ... */ }
   ```

3. Import and register in `cmd/memcp/main.go`:
   ```go
   import extSlack "github.com/sivakumar455/memcp/extensions/slack"

   // In registerExtensions():
   if cfg.Daemon.Watchers.Slack.Enabled {
       w := extSlack.NewWatcher(...)
       d.Registry().Register(w)
   }
   ```

4. Rebuild: `make build`

See `extensions/webhook/watcher.go` for a complete example.

## 📁 The `site/` Directory

The `site/` directory holds organization-specific content that should not be
shared upstream. It is excluded from public releases via `.gitattributes`.

```
site/
├── extensions/      # Private extension watchers (Jira, email, etc.)
├── configs/         # Config overrides with real paths/credentials
├── soul/            # Persona files with learned domain knowledge
└── skills/          # Domain skill files (your-domain, etc.)
```

The config loader searches `site/configs/` before `configs/`, and the bootstrap
script (`scripts/setup.go`) copies from `site/` when populating `~/.memcp/`.

To add your own organization content, create a `site/` directory and place your
files there. They will never conflict with upstream updates.

## 🛠️ How to Contribute

### Reporting Bugs

Open an [issue](https://github.com/sivakumar455/memcp/issues) with:
- Steps to reproduce
- Expected vs. actual behavior
- Your OS and Go version

### Suggesting Features

Open an issue tagged `enhancement` describing:
- The problem you're solving
- Your proposed solution
- Any alternatives you've considered

### Submitting Code

1. Create a feature branch from `main`:
   ```bash
   git checkout -b feature/your-feature-name
   ```

2. Make your changes. Follow the existing code style:
   - Use `slog` for structured logging
   - Keep packages focused (one concern per package)
   - Add comments for exported functions

3. Build and test:
   ```bash
   make build
   ```

4. Commit with a clear message:
   ```bash
   git commit -m "feat: add support for custom recall strategies"
   ```

5. Push and open a Pull Request against `main`.

## 💡 Areas Where Help Is Appreciated

This project is a work in progress. Here are some areas where contributions would be especially valuable:

- **Testing** — Unit and integration tests across all packages
- **Documentation** — Improving the README, adding usage examples
- **New Skill Domains** — Creating skill files for common development domains
- **Daemon Watchers** — Implementing watchers for GitHub Issues, Slack, etc.
- **Performance** — Profiling and optimizing SQLite queries, memory usage
- **Cross-platform** — Testing on Windows and Linux

## 📝 Code Style

- **Go version**: 1.25+
- **Dependencies**: Keep external dependencies minimal. Prefer stdlib where possible.
- **Error handling**: Always wrap errors with context (`fmt.Errorf("doing X: %w", err)`)
- **Logging**: Use `log/slog` with structured fields

## 📜 License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).

---

Questions? Open an issue or start a discussion. We're happy to help you get started! 🎉
