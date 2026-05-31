// Package memory provides SQLite-backed persistent storage for memcp.
package memory

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/sivakumar455/memcp/internal/embedding"
)

// Store is the SQLite-backed persistence layer.
type Store struct {
	db       *sql.DB
	dbPath   string
	embedder embedding.Provider
}

// NewStore opens (or creates) a SQLite database at the given path.
func NewStore(dbPath string) (*Store, error) {
	// Ensure parent directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating db directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Configure SQLite for concurrent access
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("setting WAL mode: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("setting busy timeout: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enabling foreign keys: %w", err)
	}

	s := &Store{db: db, dbPath: dbPath}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}
	return s, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	if s.embedder != nil {
		s.embedder.Close()
	}
	return s.db.Close()
}

// SetEmbedder sets the embedding provider.
func (s *Store) SetEmbedder(p embedding.Provider) {
	s.embedder = p
}

// DB returns the underlying *sql.DB for advanced queries.
func (s *Store) DB() *sql.DB {
	return s.db
}

// --- Schema Migration ---

func (s *Store) migrate() error {
	stmts := []string{
		// Sessions
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			name TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		// Messages
		`CREATE TABLE IF NOT EXISTS messages (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (session_id) REFERENCES sessions(id)
		)`,

		// Findings
		`CREATE TABLE IF NOT EXISTS findings (
			id TEXT PRIMARY KEY,
			key TEXT NOT NULL UNIQUE,
			content TEXT NOT NULL,
			tags TEXT DEFAULT '',
			importance INTEGER DEFAULT 1,
			source TEXT DEFAULT 'manual',
			session_id TEXT DEFAULT '',
			domain TEXT DEFAULT '',
			version INTEGER DEFAULT 1,
			embedding BLOB,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		// Tool Calls
		`CREATE TABLE IF NOT EXISTS tool_calls (
			id TEXT PRIMARY KEY,
			session_id TEXT DEFAULT '',
			tool_name TEXT NOT NULL,
			backend TEXT DEFAULT '',
			args_summary TEXT DEFAULT '',
			result_summary TEXT DEFAULT '',
			extracted_facts INTEGER DEFAULT 0,
			elapsed_ms INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		// Profile
		`CREATE TABLE IF NOT EXISTS profile (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			category TEXT DEFAULT '',
			hit_count INTEGER DEFAULT 1,
			last_seen DATETIME DEFAULT CURRENT_TIMESTAMP,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		// Soul Evolutions
		`CREATE TABLE IF NOT EXISTS soul_evolutions (
			id TEXT PRIMARY KEY,
			evolution_type TEXT NOT NULL,
			target_file TEXT NOT NULL,
			content_added TEXT NOT NULL,
			source_summary TEXT DEFAULT '',
			findings_count INTEGER DEFAULT 0,
			messages_count INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		// Daemon Tasks
		`CREATE TABLE IF NOT EXISTS daemon_tasks (
			id TEXT PRIMARY KEY,
			source TEXT NOT NULL,
			source_id TEXT DEFAULT '',
			title TEXT NOT NULL,
			summary TEXT NOT NULL,
			priority TEXT DEFAULT 'normal',
			category TEXT DEFAULT '',
			status TEXT DEFAULT 'pending',
			raw_data TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			acted_at DATETIME,
			action_result TEXT DEFAULT '',
			snoozed_until DATETIME
		)`,

		// Indexes
		`CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_created ON messages(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_findings_tags ON findings(tags)`,
		`CREATE INDEX IF NOT EXISTS idx_findings_importance ON findings(importance)`,
		`CREATE INDEX IF NOT EXISTS idx_findings_updated ON findings(updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_findings_domain ON findings(domain)`,
		`CREATE INDEX IF NOT EXISTS idx_tool_calls_session ON tool_calls(session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_tool_calls_created ON tool_calls(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_tool_calls_tool ON tool_calls(tool_name)`,
		`CREATE INDEX IF NOT EXISTS idx_profile_category ON profile(category)`,
		`CREATE INDEX IF NOT EXISTS idx_profile_hits ON profile(hit_count)`,
		`CREATE INDEX IF NOT EXISTS idx_evolutions_created ON soul_evolutions(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_daemon_tasks_status ON daemon_tasks(status)`,
		`CREATE INDEX IF NOT EXISTS idx_daemon_tasks_source ON daemon_tasks(source)`,
		`CREATE INDEX IF NOT EXISTS idx_daemon_tasks_source_id ON daemon_tasks(source_id)`,
		`CREATE INDEX IF NOT EXISTS idx_daemon_tasks_priority ON daemon_tasks(priority)`,
		`CREATE INDEX IF NOT EXISTS idx_daemon_tasks_created ON daemon_tasks(created_at)`,

		// FTS5 for full-text search on findings
		`CREATE VIRTUAL TABLE IF NOT EXISTS findings_fts USING fts5(
			key, content, tags,
			content='findings',
			content_rowid='rowid',
			tokenize='porter unicode61'
		)`,

		// Triggers to keep FTS in sync
		`CREATE TRIGGER IF NOT EXISTS findings_ai AFTER INSERT ON findings BEGIN
			INSERT INTO findings_fts(rowid, key, content, tags)
			VALUES (new.rowid, new.key, new.content, new.tags);
		END`,

		`CREATE TRIGGER IF NOT EXISTS findings_ad AFTER DELETE ON findings BEGIN
			INSERT INTO findings_fts(findings_fts, rowid, key, content, tags)
			VALUES('delete', old.rowid, old.key, old.content, old.tags);
		END`,

		`CREATE TRIGGER IF NOT EXISTS findings_au AFTER UPDATE ON findings BEGIN
			INSERT INTO findings_fts(findings_fts, rowid, key, content, tags)
			VALUES('delete', old.rowid, old.key, old.content, old.tags);
			INSERT INTO findings_fts(rowid, key, content, tags)
			VALUES (new.rowid, new.key, new.content, new.tags);
		END`,
	}

	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("executing %q: %w", stmt[:min(60, len(stmt))], err)
		}
	}

	// Additive column migrations for databases created by older versions.
	// ALTER TABLE … ADD COLUMN is safe: it's a no-op if the column already exists in SQLite.
	alters := []string{
		`ALTER TABLE findings ADD COLUMN embedding BLOB`,
	}
	for _, alt := range alters {
		s.db.Exec(alt) // ignore "duplicate column" errors
	}

	return nil
}

// --- Data Types ---

// Session represents a chat session.
type Session struct {
	ID        string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Message represents a conversation message.
type Message struct {
	ID        string
	SessionID string
	Role      string
	Content   string
	CreatedAt time.Time
}

// Finding represents a persisted knowledge item.
type Finding struct {
	ID         string
	Key        string
	Content    string
	Tags       string
	Importance int
	Source     string
	SessionID  string
	Domain     string
	Version    int
	Embedding  []float32
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// ToolCall represents an observed tool call record.
type ToolCall struct {
	ID             string
	SessionID      string
	ToolName       string
	Backend        string
	ArgsSummary    string
	ResultSummary  string
	ExtractedFacts int
	ElapsedMs      int
	CreatedAt      time.Time
}

// ProfileEntry represents a user behavior pattern.
type ProfileEntry struct {
	Key       string
	Value     string
	Category  string
	HitCount  int
	LastSeen  time.Time
	CreatedAt time.Time
}

// DaemonTask represents a background queued task.
type DaemonTask struct {
	ID           string
	Source       string
	SourceID     string
	Title        string
	Summary      string
	Priority     string
	Category     string
	Status       string
	RawData      string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	ActedAt      *time.Time
	ActionResult string
	SnoozedUntil *time.Time
}

// --- Session Operations ---

// CreateSession creates a new session.
func (s *Store) CreateSession(name string) (*Session, error) {
	id := uuid.New().String()
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`INSERT INTO sessions (id, name, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		id, name, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("creating session: %w", err)
	}
	return &Session{ID: id, Name: name, CreatedAt: now, UpdatedAt: now}, nil
}

// GetSession retrieves a session by ID.
func (s *Store) GetSession(id string) (*Session, error) {
	row := s.db.QueryRow(`SELECT id, name, created_at, updated_at FROM sessions WHERE id = ?`, id)
	sess := &Session{}
	if err := row.Scan(&sess.ID, &sess.Name, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
		return nil, err
	}
	return sess, nil
}

// GetSessionByName retrieves a session by name.
func (s *Store) GetSessionByName(name string) (*Session, error) {
	row := s.db.QueryRow(`SELECT id, name, created_at, updated_at FROM sessions WHERE name = ?`, name)
	sess := &Session{}
	if err := row.Scan(&sess.ID, &sess.Name, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
		return nil, err
	}
	return sess, nil
}

// ListSessions returns all sessions ordered by most recent.
func (s *Store) ListSessions() ([]*Session, error) {
	rows, err := s.db.Query(`SELECT id, name, created_at, updated_at FROM sessions ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		sess := &Session{}
		if err := rows.Scan(&sess.ID, &sess.Name, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

// TouchSession updates the session's updated_at timestamp.
func (s *Store) TouchSession(id string) error {
	_, err := s.db.Exec(`UPDATE sessions SET updated_at = ? WHERE id = ?`, time.Now().UTC(), id)
	return err
}

// --- Message Operations ---

// SaveMessage persists a conversation message.
func (s *Store) SaveMessage(sessionID, role, content string) error {
	id := uuid.New().String()
	_, err := s.db.Exec(
		`INSERT INTO messages (id, session_id, role, content) VALUES (?, ?, ?, ?)`,
		id, sessionID, role, content,
	)
	return err
}

// GetMessages returns recent messages for a session.
func (s *Store) GetMessages(sessionID string, limit int) ([]*Message, error) {
	rows, err := s.db.Query(
		`SELECT id, session_id, role, content, created_at
		 FROM messages WHERE session_id = ?
		 ORDER BY created_at DESC LIMIT ?`, sessionID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []*Message
	for rows.Next() {
		m := &Message{}
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.CreatedAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	// Reverse to chronological order
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, rows.Err()
}

// --- Finding Operations ---

// UpsertFinding inserts or updates a finding.
func (s *Store) UpsertFinding(f *Finding) error {
	if f.ID == "" {
		f.ID = uuid.New().String()
	}
	now := time.Now().UTC()

	var embeddingBytes []byte
	var err error

	// Generate embedding if provider is available
	if s.embedder != nil {
		f.Embedding, err = s.embedder.Generate(f.Content)
		if err != nil {
			return fmt.Errorf("generating embedding: %w", err)
		}
		embeddingBytes, err = Float32ArrayToBytes(f.Embedding)
		if err != nil {
			return fmt.Errorf("serializing embedding: %w", err)
		}
	}

	_, err = s.db.Exec(`
		INSERT INTO findings (id, key, content, tags, importance, source, session_id, domain, version, embedding, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			content = excluded.content,
			tags = excluded.tags,
			importance = CASE WHEN excluded.importance > findings.importance THEN excluded.importance ELSE findings.importance END,
			domain = CASE WHEN excluded.domain != '' THEN excluded.domain ELSE findings.domain END,
			version = findings.version + 1,
			embedding = excluded.embedding,
			updated_at = excluded.updated_at
	`, f.ID, f.Key, f.Content, f.Tags, f.Importance, f.Source, f.SessionID, f.Domain, 1, embeddingBytes, now, now)
	return err
}

// GetFinding retrieves a finding by key.
func (s *Store) GetFinding(key string) (*Finding, error) {
	row := s.db.QueryRow(`
		SELECT id, key, content, tags, importance, source, session_id, domain, version, embedding, created_at, updated_at
		FROM findings WHERE key = ?`, key)
	f := &Finding{}
	var embedBytes []byte
	err := row.Scan(&f.ID, &f.Key, &f.Content, &f.Tags, &f.Importance, &f.Source, &f.SessionID, &f.Domain, &f.Version, &embedBytes, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if len(embedBytes) > 0 {
		f.Embedding, _ = BytesToFloat32Array(embedBytes)
	}
	return f, nil
}

// SearchFindings performs a semantic or full-text search on findings.
func (s *Store) SearchFindings(query string, limit int) ([]*Finding, error) {
	if limit <= 0 {
		limit = 20
	}

	if s.embedder != nil {
		// Attempt semantic vector search first
		findings, err := s.searchHybrid(query, limit)
		if err == nil && len(findings) > 0 {
			return findings, nil
		}
	}

	// Try FTS5 fallback
	findings, err := s.searchFTS(query, limit)
	if err == nil && len(findings) > 0 {
		return findings, nil
	}

	// Fallback to LIKE search
	return s.searchLike(query, limit)
}

func (s *Store) searchFTS(query string, limit int) ([]*Finding, error) {
	// Sanitize the FTS query: escape special chars
	ftsQuery := sanitizeFTSQuery(query)
	if ftsQuery == "" {
		return nil, nil
	}

	rows, err := s.db.Query(`
		SELECT f.id, f.key, f.content, f.tags, f.importance, f.source,
		       f.session_id, f.domain, f.version, f.embedding, f.created_at, f.updated_at
		FROM findings f
		JOIN findings_fts fts ON f.rowid = fts.rowid
		WHERE findings_fts MATCH ?
		ORDER BY f.importance DESC, bm25(findings_fts)
		LIMIT ?
	`, ftsQuery, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFindings(rows)
}

func (s *Store) searchHybrid(query string, limit int) ([]*Finding, error) {
	queryVector, err := s.embedder.Generate(query)
	if err != nil {
		return nil, fmt.Errorf("generating query embedding: %w", err)
	}

	// In a pure-Go setup without sqlite-vec, we fetch all findings into memory and sort.
	// We cap it at 10,000 to prevent out-of-memory issues on massive DBs.
	rows, err := s.db.Query(`
		SELECT id, key, content, tags, importance, source, session_id, domain, version, embedding, created_at, updated_at
		FROM findings
		WHERE embedding IS NOT NULL
		LIMIT 10000
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	findings, err := scanFindings(rows)
	if err != nil {
		return nil, err
	}

	return SortFindingsBySimilarity(queryVector, findings, limit), nil
}

func (s *Store) searchLike(query string, limit int) ([]*Finding, error) {
	pattern := "%" + query + "%"
	rows, err := s.db.Query(`
		SELECT id, key, content, tags, importance, source, session_id, domain, version, embedding, created_at, updated_at
		FROM findings
		WHERE key LIKE ? OR content LIKE ? OR tags LIKE ?
		ORDER BY importance DESC, updated_at DESC
		LIMIT ?
	`, pattern, pattern, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFindings(rows)
}

// GetAllFindings returns all findings, optionally capped.
func (s *Store) GetAllFindings(limit int) ([]*Finding, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.Query(`
		SELECT id, key, content, tags, importance, source, session_id, domain, version, embedding, created_at, updated_at
		FROM findings ORDER BY importance DESC, updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFindings(rows)
}

// GetFindingsSince returns findings updated since the given time.
func (s *Store) GetFindingsSince(since time.Time) ([]*Finding, error) {
	rows, err := s.db.Query(`
		SELECT id, key, content, tags, importance, source, session_id, domain, version, embedding, created_at, updated_at
		FROM findings WHERE updated_at > ?
		ORDER BY updated_at ASC`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFindings(rows)
}

// DeleteFinding removes a finding by key. Returns true if a finding was actually deleted.
func (s *Store) DeleteFinding(key string) (bool, error) {
	result, err := s.db.Exec(`DELETE FROM findings WHERE key = ?`, key)
	if err != nil {
		return false, fmt.Errorf("deleting finding: %w", err)
	}
	rows, _ := result.RowsAffected()
	return rows > 0, nil
}

// CountFindings returns the total number of findings.
func (s *Store) CountFindings() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM findings").Scan(&count)
	return count, err
}

// ListFindings returns a list of findings, ordered by updated_at DESC.
func (s *Store) ListFindings(limit, offset int) ([]*Finding, error) {
	rows, err := s.db.Query(`
		SELECT id, key, content, tags, importance, source, session_id, domain, version, embedding, created_at, updated_at
		FROM findings
		ORDER BY updated_at DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var findings []*Finding
	for rows.Next() {
		f := &Finding{}
		var createdStr, updatedStr string
		var embedBytes []byte
		err := rows.Scan(
			&f.ID, &f.Key, &f.Content, &f.Tags, &f.Importance,
			&f.Source, &f.SessionID, &f.Domain, &f.Version, &embedBytes,
			&createdStr, &updatedStr,
		)
		if err != nil {
			return nil, err
		}
		if len(embedBytes) > 0 {
			f.Embedding, _ = BytesToFloat32Array(embedBytes)
		}
		f.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		f.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
		findings = append(findings, f)
	}
	return findings, rows.Err()
}

func scanFindings(rows *sql.Rows) ([]*Finding, error) {
	var findings []*Finding
	for rows.Next() {
		f := &Finding{}
		var embedBytes []byte
		if err := rows.Scan(&f.ID, &f.Key, &f.Content, &f.Tags, &f.Importance, &f.Source,
			&f.SessionID, &f.Domain, &f.Version, &embedBytes, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		if len(embedBytes) > 0 {
			f.Embedding, _ = BytesToFloat32Array(embedBytes)
		}
		findings = append(findings, f)
	}
	return findings, rows.Err()
}

// --- Tool Call Operations ---

// SaveToolCall records an observed tool call.
func (s *Store) SaveToolCall(tc *ToolCall) error {
	if tc.ID == "" {
		tc.ID = uuid.New().String()
	}
	_, err := s.db.Exec(`
		INSERT INTO tool_calls (id, session_id, tool_name, backend, args_summary, result_summary, extracted_facts, elapsed_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, tc.ID, tc.SessionID, tc.ToolName, tc.Backend, tc.ArgsSummary, tc.ResultSummary, tc.ExtractedFacts, tc.ElapsedMs)
	return err
}

// GetRecentToolCalls returns the most recent tool calls.
func (s *Store) GetRecentToolCalls(sessionID string, limit int) ([]*ToolCall, error) {
	if limit <= 0 {
		limit = 30
	}
	var rows *sql.Rows
	var err error
	if sessionID != "" {
		rows, err = s.db.Query(`
			SELECT id, session_id, tool_name, backend, args_summary, result_summary, extracted_facts, elapsed_ms, created_at
			FROM tool_calls WHERE session_id = ?
			ORDER BY created_at DESC LIMIT ?`, sessionID, limit)
	} else {
		rows, err = s.db.Query(`
			SELECT id, session_id, tool_name, backend, args_summary, result_summary, extracted_facts, elapsed_ms, created_at
			FROM tool_calls
			ORDER BY created_at DESC LIMIT ?`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var calls []*ToolCall
	for rows.Next() {
		tc := &ToolCall{}
		if err := rows.Scan(&tc.ID, &tc.SessionID, &tc.ToolName, &tc.Backend, &tc.ArgsSummary,
			&tc.ResultSummary, &tc.ExtractedFacts, &tc.ElapsedMs, &tc.CreatedAt); err != nil {
			return nil, err
		}
		calls = append(calls, tc)
	}
	// Reverse to chronological order
	for i, j := 0, len(calls)-1; i < j; i, j = i+1, j-1 {
		calls[i], calls[j] = calls[j], calls[i]
	}
	return calls, rows.Err()
}

// CountToolCalls returns the total number of tool calls.
func (s *Store) CountToolCalls() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM tool_calls`).Scan(&count)
	return count, err
}

// --- Profile Operations ---

// UpsertProfile increments or inserts a profile entry.
func (s *Store) UpsertProfile(key, value, category string) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(`
		INSERT INTO profile (key, value, category, hit_count, last_seen, created_at)
		VALUES (?, ?, ?, 1, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			hit_count = profile.hit_count + 1,
			last_seen = excluded.last_seen
	`, key, value, category, now, now)
	return err
}

// GetProfileByCategory returns profile entries for a category, sorted by hit count.
func (s *Store) GetProfileByCategory(category string, limit int) ([]*ProfileEntry, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.Query(`
		SELECT key, value, category, hit_count, last_seen, created_at
		FROM profile WHERE category = ?
		ORDER BY hit_count DESC LIMIT ?`, category, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*ProfileEntry
	for rows.Next() {
		e := &ProfileEntry{}
		if err := rows.Scan(&e.Key, &e.Value, &e.Category, &e.HitCount, &e.LastSeen, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// --- Soul Evolution Operations ---

// SaveEvolution records an evolution event.
func (s *Store) SaveEvolution(evoType, targetFile, contentAdded, sourceSummary string, findingsCount, messagesCount int) error {
	id := uuid.New().String()
	_, err := s.db.Exec(`
		INSERT INTO soul_evolutions (id, evolution_type, target_file, content_added, source_summary, findings_count, messages_count)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, id, evoType, targetFile, contentAdded, sourceSummary, findingsCount, messagesCount)
	return err
}

// CountEvolutions returns the total number of evolution events.
func (s *Store) CountEvolutions() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM soul_evolutions`).Scan(&count)
	return count, err
}

// GetLastEvolutionTime returns the timestamp of the most recent evolution.
func (s *Store) GetLastEvolutionTime() (time.Time, error) {
	var t time.Time
	err := s.db.QueryRow(`SELECT COALESCE(MAX(created_at), '2000-01-01') FROM soul_evolutions`).Scan(&t)
	return t, err
}

// --- Daemon Task Operations ---

// SaveDaemonTask inserts or updates a daemon task.
func (s *Store) SaveDaemonTask(t *DaemonTask) error {
	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	now := time.Now().UTC()
	
	_, err := s.db.Exec(`
		INSERT INTO daemon_tasks (id, source, source_id, title, summary, priority, category, status, raw_data, created_at, updated_at, acted_at, action_result, snoozed_until)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title = excluded.title,
			summary = excluded.summary,
			priority = excluded.priority,
			category = excluded.category,
			status = excluded.status,
			raw_data = excluded.raw_data,
			updated_at = ?,
			acted_at = excluded.acted_at,
			action_result = excluded.action_result,
			snoozed_until = excluded.snoozed_until
	`, t.ID, t.Source, t.SourceID, t.Title, t.Summary, t.Priority, t.Category, t.Status, t.RawData, now, now, t.ActedAt, t.ActionResult, t.SnoozedUntil, now)
	return err
}

// GetPendingDaemonTasks returns tasks awaiting user action.
func (s *Store) GetPendingDaemonTasks(limit int) ([]*DaemonTask, error) {
	if limit <= 0 {
		limit = 10
	}
	now := time.Now().UTC()
	
	rows, err := s.db.Query(`
		SELECT id, source, source_id, title, summary, priority, category, status, raw_data, created_at, updated_at, acted_at, action_result, snoozed_until
		FROM daemon_tasks
		WHERE status = 'pending' AND (snoozed_until IS NULL OR snoozed_until <= ?)
		ORDER BY
			CASE priority
				WHEN 'critical' THEN 1
				WHEN 'high' THEN 2
				WHEN 'normal' THEN 3
				WHEN 'low' THEN 4
				ELSE 5
			END ASC,
			created_at DESC
		LIMIT ?
	`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []*DaemonTask
	for rows.Next() {
		t := &DaemonTask{}
		if err := rows.Scan(&t.ID, &t.Source, &t.SourceID, &t.Title, &t.Summary, &t.Priority, &t.Category, &t.Status, &t.RawData, &t.CreatedAt, &t.UpdatedAt, &t.ActedAt, &t.ActionResult, &t.SnoozedUntil); err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// GetDaemonTaskByID retrieves a single daemon task by its ID.
func (s *Store) GetDaemonTaskByID(id string) (*DaemonTask, error) {
	row := s.db.QueryRow(`
		SELECT id, source, source_id, title, summary, priority, category, status, raw_data,
		       created_at, updated_at, acted_at, action_result, snoozed_until
		FROM daemon_tasks WHERE id = ?`, id)
	t := &DaemonTask{}
	err := row.Scan(&t.ID, &t.Source, &t.SourceID, &t.Title, &t.Summary, &t.Priority,
		&t.Category, &t.Status, &t.RawData, &t.CreatedAt, &t.UpdatedAt,
		&t.ActedAt, &t.ActionResult, &t.SnoozedUntil)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// GetDaemonTaskBySourceID retrieves a daemon task by its external source ID.
func (s *Store) GetDaemonTaskBySourceID(sourceID string) (*DaemonTask, error) {
	row := s.db.QueryRow(`
		SELECT id, source, source_id, title, summary, priority, category, status, raw_data,
		       created_at, updated_at, acted_at, action_result, snoozed_until
		FROM daemon_tasks WHERE source_id = ? ORDER BY created_at DESC LIMIT 1`, sourceID)
	t := &DaemonTask{}
	err := row.Scan(&t.ID, &t.Source, &t.SourceID, &t.Title, &t.Summary, &t.Priority,
		&t.Category, &t.Status, &t.RawData, &t.CreatedAt, &t.UpdatedAt,
		&t.ActedAt, &t.ActionResult, &t.SnoozedUntil)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// UpdateDaemonTaskStatus updates a task's status and optionally its action result.
func (s *Store) UpdateDaemonTaskStatus(id, status, actionResult string) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(`
		UPDATE daemon_tasks SET status = ?, action_result = ?, acted_at = ?, updated_at = ?
		WHERE id = ?`, status, actionResult, now, now, id)
	return err
}

// SnoozeDaemonTask snoozes a task until the given time.
func (s *Store) SnoozeDaemonTask(id string, until time.Time) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(`
		UPDATE daemon_tasks SET status = 'snoozed', snoozed_until = ?, updated_at = ?
		WHERE id = ?`, until, now, id)
	return err
}

// CountDaemonTasksByStatus returns task counts grouped by status.
func (s *Store) CountDaemonTasksByStatus() (map[string]int, error) {
	rows, err := s.db.Query(`SELECT status, COUNT(*) FROM daemon_tasks GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		counts[status] = count
	}
	return counts, rows.Err()
}

// SoulEvolution represents a soul evolution audit event.
type SoulEvolution struct {
	ID             string
	EvolutionType  string
	TargetFile     string
	ContentAdded   string
	SourceSummary  string
	FindingsCount  int
	MessagesCount  int
	CreatedAt      time.Time
}

// GetRecentEvolutions returns the most recent evolution events.
func (s *Store) GetRecentEvolutions(limit int) ([]*SoulEvolution, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.Query(`
		SELECT id, evolution_type, target_file, content_added, source_summary,
		       findings_count, messages_count, created_at
		FROM soul_evolutions ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var evos []*SoulEvolution
	for rows.Next() {
		e := &SoulEvolution{}
		if err := rows.Scan(&e.ID, &e.EvolutionType, &e.TargetFile, &e.ContentAdded,
			&e.SourceSummary, &e.FindingsCount, &e.MessagesCount, &e.CreatedAt); err != nil {
			return nil, err
		}
		evos = append(evos, e)
	}
	return evos, rows.Err()
}

// ToolCallStat represents an aggregated tool call statistic.
type ToolCallStat struct {
	ToolName string
	Count    int
}

// GetTopToolCalls returns the most frequently used tools.
func (s *Store) GetTopToolCalls(limit int) ([]ToolCallStat, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.Query(`
		SELECT tool_name, COUNT(*) as cnt FROM tool_calls
		GROUP BY tool_name ORDER BY cnt DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []ToolCallStat
	for rows.Next() {
		var s ToolCallStat
		if err := rows.Scan(&s.ToolName, &s.Count); err != nil {
			return nil, err
		}
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

// CountSessions returns the total number of sessions.
func (s *Store) CountSessions() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&count)
	return count, err
}

// GetFindingsByDomain returns findings filtered by domain.
func (s *Store) GetFindingsByDomain(domain string, limit int) ([]*Finding, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.Query(`
		SELECT id, key, content, tags, importance, source, session_id, domain, version, embedding, created_at, updated_at
		FROM findings WHERE domain = ?
		ORDER BY updated_at DESC LIMIT ?`, domain, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFindings(rows)
}

// --- Maintenance ---

// PruneTransientFindings deletes findings with importance=0 that haven't been updated in maxAgeDays.
func (s *Store) PruneTransientFindings(maxAgeDays int) (int, error) {
	cutoff := time.Now().AddDate(0, 0, -maxAgeDays)
	result, err := s.db.Exec(`DELETE FROM findings WHERE importance = 0 AND updated_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	rows, err := result.RowsAffected()
	return int(rows), err
}

// DecayFindings downgrades finding importance to 0 if they haven't been updated in maxAgeDays.
func (s *Store) DecayFindings(maxAgeDays int) (int, error) {
	cutoff := time.Now().AddDate(0, 0, -maxAgeDays)
	result, err := s.db.Exec(`UPDATE findings SET importance = 0 WHERE importance = 1 AND updated_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	rows, err := result.RowsAffected()
	return int(rows), err
}

// MaintainDB runs PRAGMA optimize for sqlite performance.
func (s *Store) MaintainDB() error {
	_, err := s.db.Exec(`PRAGMA optimize`)
	return err
}

// GetDBSize returns the physical size of the SQLite DB file in bytes.
func (s *Store) GetDBSize() (int64, error) {
	info, err := os.Stat(s.dbPath)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// --- Credential Sanitization ---

var credentialPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(password|passwd|secret|token|api[_-]?key)\s*[=:]\s*\S+`),
	regexp.MustCompile(`(?i)Basic\s+[A-Za-z0-9+/=]{10,}`),
	regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._~+/=-]{10,}`),
	regexp.MustCompile(`(?i)ssh\s+-[LRD]\s+\S+`),
	regexp.MustCompile(`(?i)Credentials?:\s*\S+\s*/\s*\S+`),
}

// SanitizeContent redacts credential patterns from content.
func SanitizeContent(content string) string {
	for _, re := range credentialPatterns {
		content = re.ReplaceAllString(content, "[REDACTED]")
	}
	return content
}

// --- Convenience Aliases ---

const dbTimeFormat = "2006-01-02T15:04:05Z"

// FormatDBTime formats a time.Time to the standard DB string format.
func FormatDBTime(t time.Time) string { return t.UTC().Format(dbTimeFormat) }

// ParseDBTime parses a DB time string back to time.Time.
func ParseDBTime(s string) time.Time {
	t, _ := time.Parse(dbTimeFormat, s)
	return t
}

// GetFindingByKey is an alias for GetFinding.
func (s *Store) GetFindingByKey(key string) (*Finding, error) { return s.GetFinding(key) }

// GetRecentMessages is an alias for GetMessages.
func (s *Store) GetRecentMessages(sessionID string, limit int) ([]*Message, error) {
	return s.GetMessages(sessionID, limit)
}

// GetAllProfile returns all profile entries up to limit.
func (s *Store) GetAllProfile(limit int) ([]*ProfileEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT key, value, category, hit_count, last_seen, created_at FROM profile ORDER BY hit_count DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []*ProfileEntry
	for rows.Next() {
		e := &ProfileEntry{}
		var lastSeen, createdAt string
		if err := rows.Scan(&e.Key, &e.Value, &e.Category, &e.HitCount, &lastSeen, &createdAt); err != nil {
			continue
		}
		e.LastSeen, _ = time.Parse(time.RFC3339, lastSeen)
		e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		entries = append(entries, e)
	}
	return entries, nil
}

// LastEvolutionTime is an alias for GetLastEvolutionTime.
func (s *Store) LastEvolutionTime() (time.Time, error) { return s.GetLastEvolutionTime() }

// GetFindingsByImportance returns findings with importance >= minImportance.
func (s *Store) GetFindingsByImportance(minImportance, limit int) ([]*Finding, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`
		SELECT id, key, content, tags, importance, source, session_id, domain, version, embedding, created_at, updated_at
		FROM findings WHERE importance >= ? ORDER BY importance DESC, updated_at DESC LIMIT ?`,
		minImportance, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanFindings(rows)
}

// CountFindingsByDomain counts findings in a specific domain.
func (s *Store) CountFindingsByDomain(domain string) (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM findings WHERE domain = ?`, domain).Scan(&count)
	return count, err
}

// CountMessagesSince returns the message count since a timestamp.
func (s *Store) CountMessagesSince(since time.Time) (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE created_at >= ?`, since.Format(time.RFC3339)).Scan(&count)
	return count, err
}

// GetEvolutions returns recent evolution records as SoulEvolution.
func (s *Store) GetEvolutions(limit int) ([]*SoulEvolution, error) {
	return s.GetRecentEvolutions(limit)
}

// Stats holds aggregate memory statistics.
type Stats struct {
	Findings          int
	PermanentFindings int
	ToolCalls         int
	ProfileItems      int
	Evolutions        int
	Sessions          int
	UniqueToolsUsed   int
	OldestFinding     time.Time
	NewestToolCall    time.Time
	TopTools          []ToolCallStat
}

// GetStats returns aggregate statistics across all memory tables.
func (s *Store) GetStats() Stats {
	var st Stats
	s.db.QueryRow(`SELECT COUNT(*) FROM findings`).Scan(&st.Findings)
	s.db.QueryRow(`SELECT COUNT(*) FROM findings WHERE importance >= 2`).Scan(&st.PermanentFindings)
	s.db.QueryRow(`SELECT COUNT(*) FROM tool_calls`).Scan(&st.ToolCalls)
	s.db.QueryRow(`SELECT COUNT(*) FROM profile`).Scan(&st.ProfileItems)
	st.Evolutions, _ = s.CountEvolutions()
	st.Sessions, _ = s.CountSessions()

	s.db.QueryRow(`SELECT COUNT(DISTINCT tool_name) FROM tool_calls`).Scan(&st.UniqueToolsUsed)

	var oldest, newest string
	s.db.QueryRow(`SELECT MIN(created_at) FROM findings`).Scan(&oldest)
	s.db.QueryRow(`SELECT MAX(created_at) FROM tool_calls`).Scan(&newest)
	if oldest != "" {
		st.OldestFinding, _ = time.Parse(time.RFC3339, oldest)
	}
	if newest != "" {
		st.NewestToolCall, _ = time.Parse(time.RFC3339, newest)
	}
	st.TopTools, _ = s.GetTopToolCalls(10)
	return st
}

// scanFindings helper to scan Finding rows (including embedding column).
func (s *Store) scanFindings(rows *sql.Rows) ([]*Finding, error) {
	var findings []*Finding
	for rows.Next() {
		f := &Finding{}
		var embedBytes []byte
		var createdAt, updatedAt string
		if err := rows.Scan(&f.ID, &f.Key, &f.Content, &f.Tags, &f.Importance, &f.Source, &f.SessionID, &f.Domain, &f.Version, &embedBytes, &createdAt, &updatedAt); err != nil {
			continue
		}
		if len(embedBytes) > 0 {
			f.Embedding, _ = BytesToFloat32Array(embedBytes)
		}
		f.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		f.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		findings = append(findings, f)
	}
	return findings, nil
}

// --- FTS Query Sanitization ---

func sanitizeFTSQuery(query string) string {
	// Remove FTS5 special characters that could cause syntax errors
	replacer := strings.NewReplacer(
		"\"", "",
		"'", "",
		"*", "",
		"-", " ",
		"+", " ",
		"(", "",
		")", "",
		":", " ",
		"^", "",
	)
	sanitized := replacer.Replace(query)
	sanitized = strings.TrimSpace(sanitized)

	// Split into words and join with OR for broader matching
	words := strings.Fields(sanitized)
	if len(words) == 0 {
		return ""
	}
	// Use implicit AND (just space-separated in FTS5)
	return strings.Join(words, " ")
}
