// Package observation implements the automated fact extraction engine (Observer).
package observation

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/sivakumar455/memcp/internal/config"
	"github.com/sivakumar455/memcp/internal/engine"
	"github.com/sivakumar455/memcp/internal/memory"
)

var (
	// regexes for result parsing
	errRegex   = regexp.MustCompile(`(?i)(error|exception|failed|timeout|refused|unavailable|OOMKilled|CrashLoopBackOff)`)
	traceRegex = regexp.MustCompile(`(?i)(?:trace[_-]?id|x-b3-traceid|correlation[_-]?id)[:\s="]*([a-f0-9]{16,32})`)
)

// ProfileTracker is an interface for recording user behavior patterns.
// This avoids a circular dependency between observation and engine packages.
type ProfileTracker interface {
	Track(toolName string, args map[string]interface{})
}

// Observer extracts facts from tool calls automatically.
type Observer struct {
	store    *memory.Store
	manager  *engine.MemoryManager
	profiler ProfileTracker
	cfg      config.ObservationConfig
	wg       sync.WaitGroup
}

// New creates a new Observer.
func New(store *memory.Store, manager *engine.MemoryManager, cfg config.ObservationConfig, profiler ProfileTracker) *Observer {
	return &Observer{
		store:    store,
		manager:  manager,
		profiler: profiler,
		cfg:      cfg,
	}
}

// Wait waits for all async observations to complete.
func (o *Observer) Wait() {
	o.wg.Wait()
}

// Observe processes a completed tool call.
func (o *Observer) Observe(toolName, backendOpts, rawArgs, rawResult string, elapsedMs int) {
	if !o.cfg.Enabled {
		return
	}

	if o.cfg.AsyncUpsert {
		o.wg.Add(1)
		go func() {
			defer o.wg.Done()
			o.process(toolName, backendOpts, rawArgs, rawResult, elapsedMs)
		}()
	} else {
		o.process(toolName, backendOpts, rawArgs, rawResult, elapsedMs)
	}
}

func (o *Observer) process(toolName, backend, rawArgs, rawResult string, elapsedMs int) {
	// 1. Summarize
	argsSummary := summarizeJSON(rawArgs, 100)
	resultSummary := summarizeString(rawResult, o.cfg.MaxResultSummary)

	// 2. Extract Facts
	var extracted []extractedFact
	extracted = append(extracted, o.extractFromArgs(rawArgs)...)
	extracted = append(extracted, o.extractFromResult(toolName, rawResult)...)

	// Trim to max facts limit
	if o.cfg.MaxFactsPerCall > 0 && len(extracted) > o.cfg.MaxFactsPerCall {
		extracted = extracted[:o.cfg.MaxFactsPerCall]
	}

	// 3. Save Tool Call
	tc := &memory.ToolCall{
		ToolName:       toolName,
		Backend:        backend,
		ArgsSummary:    argsSummary,
		ResultSummary:  resultSummary,
		ExtractedFacts: len(extracted),
		ElapsedMs:      elapsedMs,
	}
	if err := o.store.SaveToolCall(tc); err != nil {
		slog.Error("shim observer failed to save tool call", "error", err)
	}

	// 4. Save Facts to Memory Manager
	for _, fact := range extracted {
		_, err := o.manager.SaveExplicit(
			fact.Key,
			fact.Content,
			fact.Tags,
			fact.Importance,
			"", // sessionID not tied to shim directly
			"", // domain auto
			"shim",
		)
		if err != nil {
			slog.Error("shim observer failed to save fact", "fact", fact.Key, "error", err)
		}
	}

	// 5. Update user profile
	if o.profiler != nil {
		var parsedArgs map[string]interface{}
		if err := json.Unmarshal([]byte(rawArgs), &parsedArgs); err == nil {
			o.profiler.Track(toolName, parsedArgs)
		}
	}
}

type extractedFact struct {
	Key        string
	Content    string
	Tags       string
	Importance int
}

func (o *Observer) extractFromArgs(rawArgs string) []extractedFact {
	var facts []extractedFact
	var args map[string]interface{}
	
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return facts
	}

	for k, v := range args {
		valStr := fmt.Sprintf("%v", v)
		valStr = summarizeString(valStr, 100)

		switch k {
		case "env", "environment", "namespace":
			facts = append(facts, extractedFact{
				Key:        fmt.Sprintf("env:%s", valStr),
				Content:    fmt.Sprintf("Accessed environment/namespace: %s", valStr),
				Tags:       "environment",
				Importance: 1,
			})
		case "ms_name", "service", "microservice":
			facts = append(facts, extractedFact{
				Key:        fmt.Sprintf("ms:%s", valStr),
				Content:    fmt.Sprintf("Investigated microservice: %s", valStr),
				Tags:       "microservice",
				Importance: 1,
			})
		case "pod", "container":
			facts = append(facts, extractedFact{
				Key:        fmt.Sprintf("pod:%s", valStr),
				Content:    fmt.Sprintf("Examined pod/container: %s", valStr),
				Tags:       "state",
				Importance: 0, // usually transient logic
			})
		}
	}
	return facts
}

func (o *Observer) extractFromResult(toolName, resultText string) []extractedFact {
	var facts []extractedFact
	lines := strings.Split(resultText, "\n")
	
	var errLines []string
	
	// Scan lines
	for _, line := range lines {
		// Trace IDs
		matches := traceRegex.FindStringSubmatch(line)
		if len(matches) > 1 {
			facts = append(facts, extractedFact{
				Key:        fmt.Sprintf("trace:%s", matches[1]),
				Content:    fmt.Sprintf("Observed trace ID in %s result: %s", toolName, matches[1]),
				Tags:       "reference,trace",
				Importance: 1,
			})
		}

		// Errors (collect up to 5)
		if len(errLines) < 5 && errRegex.MatchString(line) {
			errLines = append(errLines, summarizeString(strings.TrimSpace(line), 200))
		}
	}

	if len(errLines) > 0 {
		facts = append(facts, extractedFact{
			Key:        fmt.Sprintf("obs:%s:%d", toolName, time.Now().Unix()),
			Content:    "Observed error logs:\n" + strings.Join(errLines, "\n"),
			Tags:       "observation,error",
			Importance: 1,
		})
	}

	return facts
}

func summarizeJSON(raw string, maxChars int) string {
	// Just remove newlines to flatten
	flat := strings.ReplaceAll(raw, "\n", " ")
	return summarizeString(flat, maxChars)
}

func summarizeString(s string, maxChars int) string {
	if maxChars <= 0 {
		maxChars = 500
	}
	if len(s) <= maxChars {
		return s
	}

	// 70% start, 30% end
	keepStart := int(float64(maxChars) * 0.70)
	keepEnd := maxChars - keepStart - 7 // 7 for " [...] "

	if keepEnd <= 0 {
		return s[:maxChars]
	}

	return s[:keepStart] + " [...] " + s[len(s)-keepEnd:]
}
