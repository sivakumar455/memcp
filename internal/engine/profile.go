package engine

import (
	"fmt"
	"strings"

	"github.com/sivakumar455/memcp/internal/config"
	"github.com/sivakumar455/memcp/internal/memory"
)

// ProfileBuilder tracks user behavior patterns from tool call metadata
// and generates a compact profile for context injection.
type ProfileBuilder struct {
	store *memory.Store
	cfg   config.ProfileConfig
}

// NewProfileBuilder creates a new ProfileBuilder.
func NewProfileBuilder(store *memory.Store, cfg config.ProfileConfig) *ProfileBuilder {
	return &ProfileBuilder{store: store, cfg: cfg}
}

// Track records user behavior signals from a tool call.
// Implements the ProfileTracker interface used by the Observer.
func (pb *ProfileBuilder) Track(toolName string, args map[string]interface{}) {
	if !pb.cfg.Enabled {
		return
	}

	// Track tool usage
	_ = pb.store.UpsertProfile(
		fmt.Sprintf("tool:%s", toolName),
		toolName,
		"tool",
	)

	// Track environment
	if env, ok := extractStringArg(args, "env", "environment"); ok {
		_ = pb.store.UpsertProfile(
			fmt.Sprintf("env:%s", env),
			env,
			"environment",
		)
	}

	// Track namespace
	if ns, ok := extractStringArg(args, "namespace", "ns"); ok {
		_ = pb.store.UpsertProfile(
			fmt.Sprintf("ns:%s", ns),
			ns,
			"namespace",
		)
	}

	// Track microservice
	if ms, ok := extractStringArg(args, "ms_name", "service", "microservice"); ok {
		_ = pb.store.UpsertProfile(
			fmt.Sprintf("ms:%s", ms),
			ms,
			"microservice",
		)
	}

	// Infer and track topic from tool name
	if topic := inferTopic(toolName); topic != "" {
		_ = pb.store.UpsertProfile(
			fmt.Sprintf("topic:%s", topic),
			topic,
			"topic",
		)
	}
}

// RecordToolUsage is an alias for Track.
func (pb *ProfileBuilder) RecordToolUsage(toolName, backend string, args map[string]interface{}) {
	pb.Track(toolName, args)
}

// CompactProfile generates a formatted profile string for Tier 0 context injection.
func (pb *ProfileBuilder) CompactProfile(topN int) string {
	if !pb.cfg.Enabled {
		return ""
	}
	if topN <= 0 {
		topN = pb.cfg.TopNInContext
	}
	if topN <= 0 {
		topN = 10
	}

	categories := []struct {
		category string
		label    string
	}{
		{"environment", "Primary environments"},
		{"topic", "Focus areas"},
		{"microservice", "Key microservices"},
		{"tool", "Frequent tools"},
		{"namespace", "Active namespaces"},
	}

	var lines []string
	for _, cat := range categories {
		entries, err := pb.store.GetProfileByCategory(cat.category, topN)
		if err != nil || len(entries) == 0 {
			continue
		}

		var parts []string
		for _, e := range entries {
			parts = append(parts, fmt.Sprintf("%s (%d)", e.Value, e.HitCount))
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", cat.label, strings.Join(parts, ", ")))
	}

	if len(lines) == 0 {
		return ""
	}

	return "--- User Profile ---\n" + strings.Join(lines, "\n")
}

// --- Topic Inference ---

// topicRules maps tool name keywords to topic names.
var topicRules = []struct {
	keywords []string
	topic    string
}{
	{[]string{"deploy", "release"}, "deployment"},
	{[]string{"k8s", "kube", "kubectl"}, "k8s-troubleshoot"},
	{[]string{"db", "query", "sql", "database"}, "database"},
	{[]string{"log", "analysis"}, "log-analysis"},
	{[]string{"environment", "env"}, "environment-management"},
	{[]string{"api", "test", "http"}, "api-testing"},
	{[]string{"monitor", "metric", "alert"}, "monitoring"},
	{[]string{"build", "ci", "pipeline"}, "ci-cd"},
	{[]string{"git", "commit", "branch", "pr"}, "version-control"},
}

func inferTopic(toolName string) string {
	lower := strings.ToLower(toolName)
	for _, rule := range topicRules {
		for _, kw := range rule.keywords {
			if strings.Contains(lower, kw) {
				return rule.topic
			}
		}
	}
	return ""
}

// extractStringArg tries to extract a string value from args using multiple possible keys.
func extractStringArg(args map[string]interface{}, keys ...string) (string, bool) {
	for _, k := range keys {
		if v, ok := args[k]; ok {
			s := fmt.Sprintf("%v", v)
			if s != "" && s != "<nil>" {
				return s, true
			}
		}
	}
	return "", false
}
