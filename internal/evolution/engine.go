package evolution

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sivakumar455/memcp/internal/common"
	"github.com/sivakumar455/memcp/internal/config"
	"github.com/sivakumar455/memcp/internal/memory"
	"github.com/sivakumar455/memcp/internal/persona"
	"github.com/sivakumar455/memcp/internal/skills"
)

// stopWords is aliased from the shared common package.
var stopWords = common.StopWords

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
	return e.runWithThreshold(true)
}

// ForceRun executes an evolution cycle unconditionally, bypassing thresholds.
func (e *Engine) ForceRun() error {
	return e.runWithThreshold(false)
}

func (e *Engine) runWithThreshold(checkThresholds bool) error {
	// Prevent concurrent executions
	if !e.mu.TryLock() {
		return nil // Skipped, already running
	}
	defer e.mu.Unlock()

	// Threshold gating: skip evolution if not enough new data has accumulated
	if checkThresholds && e.cfg.MinFindings > 0 {
		lastEvo, _ := e.store.GetLastEvolutionTime()
		newFindings, err := e.store.GetFindingsSince(lastEvo)
		if err == nil && len(newFindings) < e.cfg.MinFindings {
			slog.Debug("evolution skipped: insufficient new findings",
				"new", len(newFindings), "threshold", e.cfg.MinFindings)
			return nil
		}
	}

	slog.Info("running evolution cycle")

	var changes []string

	// 0. Database Maintenance (Decay & Prune)
	if decayed, err := e.store.DecayFindings(60); err == nil && decayed > 0 {
		changes = append(changes, fmt.Sprintf("decayed %d findings", decayed))
	} else if err != nil {
		slog.Error("decay error", "error", err)
	}
	if pruned, err := e.store.PruneTransientFindings(14); err == nil && pruned > 0 {
		changes = append(changes, fmt.Sprintf("pruned %d findings", pruned))
	} else if err != nil {
		slog.Error("prune error", "error", err)
	}

	// Count findings after prune for audit trail
	findingsCount, _ := e.store.CountFindings()

	// 1. Run full compaction on MEMORY.md
	if err := e.compactor.RunFullCompaction(); err != nil {
		slog.Error("compaction error", "error", err)
	} else {
		changes = append(changes, "compacted MEMORY.md")
	}

	// 2. Extract patterns to update IDENTITY.md
	if err := e.updateIdentityPatterns(); err != nil {
		return fmt.Errorf("updating IDENTITY.md patterns: %w", err)
	}
	changes = append(changes, "updated IDENTITY.md patterns")

	// 3. Run per-skill evolution
	if e.skillEvolver != nil {
		if err := e.skillEvolver.EvolveAll(); err != nil {
			slog.Error("skill evolution error", "error", err)
		} else {
			changes = append(changes, "evolved skills")
		}
	}

	// 4. DB Maintenance
	if err := e.store.MaintainDB(); err != nil {
		slog.Error("db maintenance error", "error", err)
	}

	// Record in audit trail with actual data
	e.recordEvolution(findingsCount, strings.Join(changes, "; "))

	return nil
}

func (e *Engine) updateIdentityPatterns() error {
	findings, err := e.store.ListFindings(1000, 0)
	if err != nil {
		return err
	}

	tagCounts := make(map[string]int)

	// Core Insights from specific high-importance findings
	var insights []string

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

		// Permanent Findings -> Core Insights
		if f.Importance == 2 {
			firstLine := strings.Split(f.Content, "\n")[0]
			summary := firstLine
			if len(summary) > 200 {
				summary = summary[:197] + "..."
			}
			insights = append(insights, fmt.Sprintf("- **%s**: %s", f.Key, summary))
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

	// Extracted Core Insights
	if len(insights) > 0 {
		sb.WriteString("### Core Insights (Permanent Knowledge)\n")
		for i, insight := range insights {
			if i >= maxPatterns {
				break // capping insights length
			}
			sb.WriteString(insight + "\n")
		}
		sb.WriteString("\n")
	}

	if len(topics) == 0 && len(insights) == 0 {
		sb.WriteString("(No recurring patterns or core insights learned yet.)\n")
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

func (e *Engine) recordEvolution(findingsCount int, contentSummary string) {
	if err := e.store.SaveEvolution("auto", "all", contentSummary,
		fmt.Sprintf("Processed %d findings", findingsCount),
		findingsCount, 0); err != nil {
		slog.Error("failed to record evolution", "error", err)
	}
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
