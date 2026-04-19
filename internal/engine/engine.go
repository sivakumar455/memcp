package engine

import (
	"fmt"
	"log/slog"

	"github.com/sivakumar455/memcp/internal/config"
	"github.com/sivakumar455/memcp/internal/evolution"
	"github.com/sivakumar455/memcp/internal/memory"
	"github.com/sivakumar455/memcp/internal/persona"
	"github.com/sivakumar455/memcp/internal/session"
	"github.com/sivakumar455/memcp/internal/skills"
)

// Engine is the central orchestrator for memcp. It owns all subsystems.
type Engine struct {
	Store        *memory.Store
	Sessions     *session.Manager
	MemMgr       *MemoryManager
	CtxBuilder   *ContextBuilder
	Persona      *persona.Loader
	Skills       *skills.Manager
	SkillRouter  *skills.Router
	SkillEvolver *skills.Evolver
	Profiler     *ProfileBuilder
	Cfg          *config.Config
	EvoEngine    *evolution.Engine

	saveCh chan struct{}
}

// New creates and initializes a new Engine.
func New(cfg *config.Config) (*Engine, error) {
	// Open SQLite store
	store, err := memory.NewStore(cfg.Memory.DBPath)
	if err != nil {
		return nil, fmt.Errorf("initializing store: %w", err)
	}

	// Initialize subsystems
	sessMgr := session.NewManager(store)
	memMgr := NewMemoryManager(store)
	ctxBuilder := NewContextBuilder(store, cfg.Context)
	personaLoader := persona.NewLoader(cfg.Persona.SoulDir, cfg.Persona.MaxCharsPerFile, cfg.Persona.TotalMaxChars)
	skillsMgr := skills.NewManager(cfg.Skills.Dir)
	profiler := NewProfileBuilder(store, cfg.Profile)

	// Load skills and build router
	loadedSkills, err := skillsMgr.LoadAll()
	if err != nil {
		slog.Warn("failed to load skills for router", "error", err)
		loadedSkills = nil
	}

	// Build tool prefix and tag mappings from config (if any)
	// The Router also merges mappings from skill frontmatter.
	toolPrefixes := make(map[string]string)
	tagMapping := make(map[string]string)
	// No config-level domain_routing is defined in SkillsConfig yet,
	// so we rely on skill frontmatter metadata for routing rules.

	skillRouter := skills.NewRouter(loadedSkills, toolPrefixes, tagMapping)
	skillEvolver := skills.NewEvolver(store, skillsMgr, cfg.Skills.MaxPatternsPerSkill)

	saveCh := make(chan struct{}, 10)
	evoEngine := evolution.NewEngine(cfg.Evolution, store, personaLoader, saveCh, skillEvolver)

	e := &Engine{
		Store:        store,
		Sessions:     sessMgr,
		MemMgr:       memMgr,
		CtxBuilder:   ctxBuilder,
		Persona:      personaLoader,
		Skills:       skillsMgr,
		SkillRouter:  skillRouter,
		SkillEvolver: skillEvolver,
		Profiler:     profiler,
		Cfg:          cfg,
		EvoEngine:    evoEngine,
		saveCh:       saveCh,
	}

	e.EvoEngine.Start()

	slog.Info("memcp engine initialized", "db", cfg.Memory.DBPath)
	return e, nil
}

// NewObserverOnly creates a lightweight engine just for the Shim mode.
func NewObserverOnly(cfg *config.Config) (*Engine, error) {
	store, err := memory.NewStore(cfg.Memory.DBPath)
	if err != nil {
		return nil, fmt.Errorf("initializing store: %w", err)
	}

	memMgr := NewMemoryManager(store)
	sessMgr := session.NewManager(store)

	e := &Engine{
		Store:    store,
		Sessions: sessMgr,
		MemMgr:   memMgr,
		Cfg:      cfg,
	}

	slog.Info("memcp observer initialized", "db", cfg.Memory.DBPath)
	return e, nil
}

// Close shuts down the engine and releases resources.
func (e *Engine) Close() error {
	if e.EvoEngine != nil {
		e.EvoEngine.Stop()
	}
	if e.Store != nil {
		return e.Store.Close()
	}
	return nil
}

// Recall assembles tiered context for the given query and session.
func (e *Engine) Recall(query, sessionName string) (string, error) {
	sessionID := ""
	if sessionName != "" {
		// Try to resolve session by name
		sess, err := e.Sessions.Switch(sessionName)
		if err == nil {
			sessionID = sess.ID
		}
	}
	if sessionID == "" {
		// Use current session
		sess, err := e.Sessions.Current()
		if err != nil {
			return "", fmt.Errorf("resolving session: %w", err)
		}
		sessionID = sess.ID
	}

	// Load Tier 0 Persona context
	personaText, err := e.Persona.LoadForContext()
	if err != nil {
		slog.Warn("failed to load persona context", "error", err)
	}

	// Load Tier 0 Skills context
	skillsText, err := e.Skills.LoadForContext(e.Cfg.Skills.MaxCharsPerSkill, query, "")
	if err != nil {
		slog.Warn("failed to load skills context", "error", err)
	}

	// Load Tier 0 Profile context
	profileText := ""
	if e.Profiler != nil {
		profileText = e.Profiler.CompactProfile(0)
	}

	opts := BuildOptions{
		Query:         query,
		SessionID:     sessionID,
		ContextWindow: e.Cfg.Memory.ContextWindow,
		PersonaText:   personaText,
		SkillsText:    skillsText,
		ProfileText:   profileText,
	}

	result, err := e.CtxBuilder.Build(opts)
	if err != nil {
		return "", fmt.Errorf("building context: %w", err)
	}

	// Record the recall as a message in the session
	_ = e.Store.SaveMessage(sessionID, "system", fmt.Sprintf("[recall] query=%q", query))
	_ = e.Store.TouchSession(sessionID)

	return result, nil
}

// Save persists a finding through the ADD/UPDATE/NOOP pipeline.
func (e *Engine) Save(key, content, tags string, importance int, domain string) (*SaveResult, error) {
	sessionID := ""
	sess, err := e.Sessions.Current()
	if err == nil {
		sessionID = sess.ID
	}

	// Auto-classify domain via SkillRouter if not explicitly provided
	if domain == "" && e.SkillRouter != nil {
		domain = e.SkillRouter.Classify("", "", tags)
	}

	result, err := e.MemMgr.SaveExplicit(key, content, tags, importance, sessionID, domain, "manual")
	if err != nil {
		return nil, err
	}

	// Signal evolution loop
	if result.Action != "NOOP" {
		select {
		case e.saveCh <- struct{}{}:
		default:
			// Channel full, evolution will pick it up later
		}
	}

	// Record in session
	if sessionID != "" {
		_ = e.Store.SaveMessage(sessionID, "system",
			fmt.Sprintf("[save] %s: %s (key=%s, tags=%s)", result.Action, result.Message, key, tags))
		_ = e.Store.TouchSession(sessionID)
	}

	slog.Info("save", "action", result.Action, "key", key)
	return result, nil
}

// SaveCh returns the channel that signals save events (for evolution loop).
func (e *Engine) SaveCh() <-chan struct{} {
	return e.saveCh
}
