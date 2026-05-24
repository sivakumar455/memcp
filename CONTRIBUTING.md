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
├── main.go                 # Entry point (Cobra CLI)
├── configs/                # YAML configuration files
├── soul/                   # Persona files (SOUL.md, IDENTITY.md, MEMORY.md)
├── internal/
│   ├── config/             # Configuration loading
│   ├── engine/             # Core orchestrator
│   ├── memory/             # SQLite store (CRUD, FTS5)
│   ├── mcp/                # MCP server + tool handlers
│   ├── session/            # Session lifecycle
│   ├── persona/            # Soul/persona file loader
│   ├── observation/        # Tool call observer + fact extraction
│   ├── evolution/          # Soul evolution system
│   ├── skills/             # Domain skill routing
│   ├── daemon/             # Background task queue
│   ├── gateway/            # HTTP gateway server
│   ├── shim/               # Transparent observation proxy
│   └── logger/             # Structured logging
└── docs/                   # Architecture documentation
```

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
