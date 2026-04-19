package evolution

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sivakumar455/memcp/internal/config"
	"github.com/sivakumar455/memcp/internal/memory"
	"github.com/sivakumar455/memcp/internal/persona"
	"github.com/sivakumar455/memcp/internal/skills"
)

var stopWords = map[string]bool{
	"the": true, "and": true, "for": true, "not": true, "with": true,
	"from": true, "this": true, "that": true, "are": true, "was": true,
	"has": true, "had": true, "env": true, "get": true, "set": true,
	"obs": true, "pod": true, "trace": true, "tool": true, "error": true,
	"failed": true, "issue": true, "status": true, "value": true, "use": true,
	"com": true, "org": true, "net": true, "www": true, "https": true, "http": true,
}

// Engine runs the background evolution loop.
type Engine struct {
	store        *memory.Store
	persona      *persona.Loader
	compactor    *Compactor
	skillEvolver *skills.Evolver
	cfg          config.EvolutionConfig
	
	saveCh    <-chan struct{}
	
	mu        sync.Mutex // Prevents concurrent evolutions
	
	wg        sync.WaitGroup
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewEngine creates a new Evolution Engine.
func NewEngine(cfg config.EvolutionConfig, store *memory.Store, persona *persona.Loader, saveCh <-chan struct{}, skillEvolver *skills.Evolver) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	return &Engine{
		store:        store,
		persona:      persona,
		compactor:    NewCompactor(store, persona),
		skillEvolver: skillEvolver,
		cfg:          cfg,
		saveCh:       saveCh,
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Start runs the background loop.
func (e *Engine) Start() {
	if !e.cfg.Enabled {
		slog.Info("evolution engine disabled in config")
		return
	}

	e.wg.Add(1)
	go e.loop()
}

// Stop signals the loop to shut down and waits for it.
func (e *Engine) Stop() {
	e.cancel()
	e.wg.Wait()
}

// RunCompaction runs full compaction on MEMORY.md without a full evolution cycle.
func (e *Engine) RunCompaction() error {
	return e.compactor.RunFullCompaction()
}

func (e *Engine) loop() {
	defer e.wg.Done()
	slog.Info("evolution engine started")

	// Initial evolution on startup to ensure consistency
	_ = e.Run()

	for {
		select {
		case <-e.ctx.Done():
			return
		case <-e.saveCh:
			// Debounce rapid saves
			time.Sleep(500 * time.Millisecond)
			
			// Drain any additional signals that piled up during sleep
drainLoop:
			for {
				select {
				case <-e.saveCh:
				default:
					break drainLoop
				}
			}

			// Check if we should shut down before running
			select {
			case <-e.ctx.Done():
				return
			default:
			}

			if err := e.Run(); err != nil {
				slog.Error("evolution pipeline failure", "error", err)
			}
		}
	}
}

// Run executes a single evolution cycle.
func (e *Engine) Run() error {
	// Prevent concurrent executions
	if !e.mu.TryLock() {
		return nil // Skipped, already running
	}
	defer e.mu.Unlock()

	// In the MVP, we just do a full compaction and pattern extraction on each run.
	// We optimize this by checking thresholds if needed, but since data is small,
	// Full extraction is fine.
	
	// Phase 3 requirements: 
	// 1. Run full compaction on MEMORY.md if needed. (For simplicity, we always run it here or 
	// just let the compactor rewrite).
	slog.Info("running evolution cycle")

	if err := e.compactor.RunFullCompaction(); err != nil {
		slog.Error("compaction error", "error", err)
	}

	// 2. Extract patterns to update IDENTITY.md
	if err := e.updateIdentityPatterns(); err != nil {
		return fmt.Errorf("updating IDENTITY.md patterns: %w", err)
	}

	// 3. Run per-skill evolution
	if e.skillEvolver != nil {
		if err := e.skillEvolver.EvolveAll(); err != nil {
			slog.Error("skill evolution error", "error", err)
		}
	}

	// Record in audit trail
	e.recordEvolution()

	return nil
}

func (e *Engine) updateIdentityPatterns() error {
	findings, err := e.store.ListFindings(1000, 0)
	if err != nil {
		return err
	}

	tagCounts := make(map[string]int)
	wordCounts := make(map[string]int)

	for _, f := range findings {
		// Tags
		if f.Tags != "" {
			for _, tag := range strings.Split(f.Tags, ",") {
				t := strings.ToLower(strings.TrimSpace(tag))
				if t != "" {
					tagCounts[t]++
				}
			}
		}

		// Keywords from keys (e.g. timeout-rootcause -> timeout, rootcause)
		words := strings.FieldsFunc(f.Key, func(r rune) bool {
			return r == '-' || r == '_' || r == '/' || r == ':' || r == ' '
		})
		for _, w := range words {
			w = strings.ToLower(w)
			if !stopWords[w] && len(w) > 2 {
				wordCounts[w]++
			}
		}
	}

	var sb strings.Builder
	sb.WriteString("\n## Learned Patterns\n\n*Auto-generated from accumulated findings. Updated by soul evolution.*\n\n")

	// Recurring topics (Tags count >= 2)
	topics := sortMap(tagCounts, 2)
	maxPatterns := e.cfg.MaxIdentityPatterns
	if maxPatterns <= 0 {
		maxPatterns = 30
	}

	if len(topics) > 0 {
		sb.WriteString("### Recurring Topics\n")
		for i, topic := range topics {
			if i >= maxPatterns/2 {
				break
			}
			sb.WriteString(fmt.Sprintf("- **%s** (seen %d times)\n", topic.Key, topic.Val))
		}
		sb.WriteString("\n")
	}

	// Frequent Keywords
	keywords := sortMap(wordCounts, 3)
	if len(keywords) > 0 {
		sb.WriteString("### Frequent Keywords in Issues\n")
		for i, kw := range keywords {
			if i >= maxPatterns/2 {
				break
			}
			sb.WriteString(fmt.Sprintf("- **%s** (seen %d times)\n", kw.Key, kw.Val))
		}
		sb.WriteString("\n")
	}

	if len(topics) == 0 && len(keywords) == 0 {
		sb.WriteString("(No recurring patterns learned yet.)\n")
	}

	// Now read IDENTITY.md, find "## Learned Patterns", and replace everything after it.
	identity, err := e.persona.ReadFile(persona.FileIdentity)
	if err != nil {
		return err
	}

	idx := strings.Index(identity, "## Learned Patterns")
	if idx != -1 {
		identity = identity[:idx]
	}

	// Append the new section
	identity += sb.String()

	return e.persona.UpdateFile(persona.FileIdentity, identity)
}

func (e *Engine) recordEvolution() {
	_, _ = e.store.DB().Exec(`
		INSERT INTO soul_evolutions (evolution_type, target_file, content_added, findings_count)
		VALUES ('auto', 'all', '', 0)
	`)
}

type kv struct {
	Key string
	Val int
}

func sortMap(m map[string]int, threshold int) []kv {
	var kvs []kv
	for k, v := range m {
		if v >= threshold {
			kvs = append(kvs, kv{k, v})
		}
	}
	sort.Slice(kvs, func(i, j int) bool {
		if kvs[i].Val == kvs[j].Val {
			return kvs[i].Key < kvs[j].Key
		}
		return kvs[i].Val > kvs[j].Val
	})
	return kvs
}
