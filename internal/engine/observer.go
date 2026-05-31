package engine

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sivakumar455/memcp/internal/config"
	"github.com/sivakumar455/memcp/internal/memory"
	"github.com/sivakumar455/memcp/internal/skills"
)

// Fact is a piece of knowledge extracted from a tool call.
type Fact struct {
	Key      string
	Content  string
	Tags     string
	Category string
	Domain   string
}

// Observer watches tool calls and extracts facts for the memory system.
type Observer struct {
	store         *memory.Store
	memMgr        *MemoryManager
	profiler      *ProfileBuilder
	router        *skills.Router
	cfg           config.ObservationConfig
	sessionID     string
	totalObserved int
}

func NewObserver(store *memory.Store, memMgr *MemoryManager, profiler *ProfileBuilder, router *skills.Router, cfg config.ObservationConfig) *Observer {
	return &Observer{
		store:    store,
		memMgr:   memMgr,
		profiler: profiler,
		router:   router,
		cfg:      cfg,
	}
}

func (o *Observer) SetSessionID(id string) { o.sessionID = id }

// Observe records a tool call and extracts facts from its args and result.
func (o *Observer) Observe(toolName, backend string, args map[string]any, resultText string, elapsed time.Duration) {
	if !o.cfg.Enabled {
		return
	}

	o.totalObserved++
	argsSummary := observerSummarizeArgs(args)
	resultSummary := observerSummarizeResult(resultText, o.cfg.MaxResultSummary)

	tc := &memory.ToolCall{
		ID:            uuid.New().String(),
		SessionID:     o.sessionID,
		ToolName:      toolName,
		Backend:       backend,
		ArgsSummary:   memory.SanitizeContent(argsSummary),
		ResultSummary: memory.SanitizeContent(resultSummary),
		ElapsedMs:     int(elapsed.Milliseconds()),
		CreatedAt:     time.Now(),
	}

	facts := o.extractFacts(toolName, args, resultText)

	if o.router != nil {
		toolDomain := o.router.ClassifyTool(toolName)
		for i := range facts {
			if facts[i].Domain == "" {
				if toolDomain != "" {
					facts[i].Domain = toolDomain
				} else {
					facts[i].Domain = o.router.ClassifyTags(facts[i].Tags)
				}
			}
		}
	}

	tc.ExtractedFacts = len(facts)

	if err := o.store.SaveToolCall(tc); err != nil {
		slog.Warn("Failed to save tool call", "tool", toolName, "error", err)
	}

	if len(facts) > 0 {
		if o.cfg.AsyncUpsert {
			go o.processFacts(facts)
		} else {
			o.processFacts(facts)
		}
	}

	if o.profiler != nil {
		if o.cfg.AsyncUpsert {
			go o.profiler.RecordToolUsage(toolName, backend, args)
		} else {
			o.profiler.RecordToolUsage(toolName, backend, args)
		}
	}

	slog.Debug("Observation complete", "tool", toolName, "facts_extracted", len(facts), "total_observed", o.totalObserved)
}

func (o *Observer) processFacts(facts []Fact) {
	for _, f := range facts {
		if err := o.memMgr.ProcessFact(f); err != nil {
			slog.Warn("Failed to process fact", "key", f.Key, "error", err)
		}
	}
}

func (o *Observer) extractFacts(toolName string, args map[string]any, result string) []Fact {
	var facts []Fact
	facts = append(facts, o.extractFromArgs(toolName, args)...)
	facts = append(facts, o.extractFromResult(toolName, result)...)
	if len(facts) > o.cfg.MaxFactsPerCall {
		facts = facts[:o.cfg.MaxFactsPerCall]
	}
	return facts
}

func (o *Observer) extractFromArgs(toolName string, args map[string]any) []Fact {
	var facts []Fact
	if env, ok := observerStringArg(args, "env"); ok && env != "" {
		ns, _ := observerStringArg(args, "namespace")
		content := fmt.Sprintf("Accessed environment %s", env)
		if ns != "" {
			content += fmt.Sprintf(", namespace %s", ns)
		}
		facts = append(facts, Fact{Key: "env:" + env, Content: content, Tags: "environment", Category: "environment"})
	}
	if ms, ok := observerStringArg(args, "ms_name"); ok && ms != "" {
		facts = append(facts, Fact{Key: "ms:" + ms, Content: fmt.Sprintf("Investigated microservice %s", ms), Tags: "microservice", Category: "microservice"})
	}
	if pod, ok := observerStringArg(args, "pod"); ok && pod != "" {
		facts = append(facts, Fact{Key: "pod:" + pod, Content: fmt.Sprintf("Examined pod %s via %s", pod, toolName), Tags: "k8s", Category: "state"})
	}
	return facts
}

var (
	obsErrorPatternRe = regexp.MustCompile(`(?i)(error|exception|failed|timeout|refused|unavailable|OOMKilled|CrashLoopBackOff)`)
	obsTraceIDRe      = regexp.MustCompile(`(?i)(?:trace[_-]?id|x-b3-traceid|correlation[_-]?id)[:\s="]*([a-f0-9]{16,32})`)
)

func (o *Observer) extractFromResult(toolName string, result string) []Fact {
	if result == "" || len(result) < 20 {
		return nil
	}
	var facts []Fact
	if obsErrorPatternRe.MatchString(result) {
		summary := observerExtractErrorSummary(result, 300)
		if summary != "" {
			facts = append(facts, Fact{
				Key: fmt.Sprintf("obs:%s:%d", toolName, time.Now().Unix()), Content: summary,
				Tags: "observation,error", Category: "observation",
			})
		}
	}
	if matches := obsTraceIDRe.FindStringSubmatch(result); len(matches) > 1 {
		facts = append(facts, Fact{
			Key: "trace:" + matches[1], Content: fmt.Sprintf("Trace ID %s found in %s result", matches[1], toolName),
			Tags: "reference,trace", Category: "reference",
		})
	}
	return facts
}

func observerExtractErrorSummary(result string, maxLen int) string {
	lines := strings.Split(result, "\n")
	var errorLines []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if obsErrorPatternRe.MatchString(line) {
			if len(line) > 200 {
				line = line[:200] + "..."
			}
			errorLines = append(errorLines, line)
			if len(errorLines) >= 5 {
				break
			}
		}
	}
	if len(errorLines) == 0 {
		return ""
	}
	summary := strings.Join(errorLines, "; ")
	if len(summary) > maxLen {
		summary = summary[:maxLen] + "..."
	}
	return summary
}

func observerSummarizeArgs(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	var parts []string
	for k, v := range args {
		s := fmt.Sprintf("%v", v)
		if len(s) > 100 {
			s = s[:100] + "..."
		}
		parts = append(parts, fmt.Sprintf("%s=%s", k, s))
	}
	summary := strings.Join(parts, ", ")
	if len(summary) > 500 {
		summary = summary[:500] + "..."
	}
	return summary
}

func observerSummarizeResult(result string, maxLen int) string {
	if result == "" {
		return ""
	}
	result = strings.TrimSpace(result)
	if maxLen <= 0 {
		maxLen = 500
	}
	if len(result) <= maxLen {
		return result
	}
	headSize := maxLen * 70 / 100
	tailSize := maxLen - headSize - 20
	if tailSize < 0 {
		tailSize = 0
	}
	return result[:headSize] + " [...] " + result[len(result)-tailSize:]
}

func observerStringArg(args map[string]any, key string) (string, bool) {
	v, ok := args[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}
