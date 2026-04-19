package daemon

import (
	"testing"

	"github.com/sivakumar455/memcp/internal/config"
)

func TestClassifier(t *testing.T) {
	rules := []config.RuleConfig{
		{
			Match:    config.RuleMatch{Source: "jira", Field: "category", Value: "bug"},
			Priority: "high",
			Action:   "queue_review",
		},
		{
			Match:    config.RuleMatch{Source: "github", Field: "title", Pattern: "(?i)urgent"},
			Priority: "critical",
			Action:   "auto_save_and_queue",
		},
	}

	c := NewClassifier(rules)

	// Test 1: matches Jira bug
	action, prio := c.Classify(Event{
		Source:   "jira",
		Category: "bug",
		Title:    "Something broke",
	})
	if action != "queue_review" || prio != "high" {
		t.Errorf("expected queue_review/high, got %s/%s", action, prio)
	}

	// Test 2: matches Github urgent
	action, prio = c.Classify(Event{
		Source: "github",
		Title:  "[URGENT] prod down",
	})
	if action != "auto_save_and_queue" || prio != "critical" {
		t.Errorf("expected auto_save_and_queue/critical, got %s/%s", action, prio)
	}

	// Test 3: no match, uses default
	action, prio = c.Classify(Event{
		Source:   "slack",
		Priority: "low", // Event specifies priority but rule doesn't catch it
	})
	if action != "queue_review" || prio != "low" {
		t.Errorf("expected queue_review/low, got %s/%s", action, prio)
	}

	// Test 4: entirely empty falls back to normal
	action, prio = c.Classify(Event{})
	if action != "queue_review" || prio != "normal" {
		t.Errorf("expected queue_review/normal, got %s/%s", action, prio)
	}
}
