// Package observation implements the automated fact extraction engine (Observer).
package observation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"

	"github.com/sivakumar455/memcp/internal/config"
	"github.com/sivakumar455/memcp/internal/engine"
	"github.com/sivakumar455/memcp/internal/memory"
)

var (
	// regexes for result parsing
	errRegex          = regexp.MustCompile(`(?i)(error|exception|failed|timeout|refused|unavailable|OOMKilled|CrashLoopBackOff)`)
	traceRegex        = regexp.MustCompile(`(?i)(?:trace[_-]?id|x-b3-traceid|correlation[_-]?id)[:\s="]*([a-f0-9]{16,32})`)
	httpStatusRegex   = regexp.MustCompile(`(?i)(?:HTTP/\d\.\d\s+)?(4\d\d|5\d\d)\s+([A-Za-z\s]+)`)
	goErrChainRegex   = regexp.MustCompile(`(?i)[a-z0-9_]+:\s+.*:\s+.*`)
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

	// Mapping definitions
	type argMapping struct {
		Prefix     string
		Action     string
		Tags       string
		Importance int
	}

	mappings := map[string]argMapping{
		"env":          {"env", "Accessed environment/namespace", "environment", 1},
		"environment":  {"env", "Accessed environment/namespace", "environment", 1},
		"namespace":    {"env", "Accessed environment/namespace", "environment", 1},
		"ms_name":      {"ms", "Investigated microservice", "microservice", 1},
		"service":      {"ms", "Investigated microservice", "microservice", 1},
		"microservice": {"ms", "Investigated microservice", "microservice", 1},
		"pod":          {"pod", "Examined pod/container", "state", 0},
		"container":    {"pod", "Examined pod/container", "state", 0},
		"cluster":      {"cluster", "Accessed cluster", "infrastructure", 1},
		"region":       {"region", "Accessed region", "infrastructure", 1},
		"host":         {"host", "Accessed host/server", "infrastructure", 1},
		"server":       {"host", "Accessed host/server", "infrastructure", 1},
		"url":          {"url", "Accessed URL/endpoint", "network", 0},
		"endpoint":     {"url", "Accessed URL/endpoint", "network", 0},
		"database":     {"db", "Accessed database", "infrastructure", 1},
		"db":           {"db", "Accessed database", "infrastructure", 1},
		"query":        {"query", "Executed query", "data", 0},
	}

	for k, v := range args {
		valStr := fmt.Sprintf("%v", v)
		valStr = summarizeString(valStr, 100)
		kLower := strings.ToLower(k)

		if mapping, ok := mappings[kLower]; ok {
			facts = append(facts, extractedFact{
				Key:        fmt.Sprintf("%s:%s", mapping.Prefix, valStr),
				Content:    fmt.Sprintf("%s: %s", mapping.Action, valStr),
				Tags:       mapping.Tags,
				Importance: mapping.Importance,
			})
		}
	}
	return facts
}

func (o *Observer) extractFromResult(toolName, resultText string) []extractedFact {
	var facts []extractedFact
	var errLines []string

	// 1. Try structured JSON parsing first
	var jsonObj map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(resultText)), &jsonObj); err == nil {
		hasError := false
		var errDesc string

		if errVal, ok := jsonObj["error"]; ok {
			hasError = true
			errDesc = fmt.Sprintf("Error object: %v", errVal)
		} else if errCode, ok := jsonObj["errorCode"]; ok {
			hasError = true
			msg := jsonObj["errorMessage"]
			errDesc = fmt.Sprintf("Error code %v: %v", errCode, msg)
		}

		if hasError {
			errLines = append(errLines, summarizeString(errDesc, 200))
		}
	}

	// 2. Scan lines for other patterns
	lines := strings.Split(resultText, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Trace IDs
		matches := traceRegex.FindStringSubmatch(trimmed)
		if len(matches) > 1 {
			facts = append(facts, extractedFact{
				Key:        fmt.Sprintf("trace:%s", matches[1]),
				Content:    fmt.Sprintf("Observed trace ID in %s result: %s", toolName, matches[1]),
				Tags:       "reference,trace",
				Importance: 1,
			})
		}

		// Errors (collect up to 5)
		if len(errLines) < 5 {
			isErr := errRegex.MatchString(trimmed) || 
				httpStatusRegex.MatchString(trimmed) ||
				goErrChainRegex.MatchString(trimmed)
				
			if isErr && !strings.Contains(strings.ToLower(trimmed), "no error") {
				errLines = append(errLines, summarizeString(trimmed, 200))
			}
		}
	}

	if len(errLines) > 0 {
		errContent := "Observed error logs:\n" + strings.Join(errLines, "\n")
		// Use content hash for key so recurring identical errors update the same finding
		hash := sha256.Sum256([]byte(toolName + ":" + strings.Join(errLines, "|")))
		hashStr := hex.EncodeToString(hash[:8]) // 16-char hex
		facts = append(facts, extractedFact{
			Key:        fmt.Sprintf("obs:%s:%s", toolName, hashStr),
			Content:    errContent,
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
