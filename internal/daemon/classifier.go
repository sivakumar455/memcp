// Package daemon implements background queue polling and task management.
package daemon

import (
	"regexp"

	"github.com/sivakumar455/memcp/internal/config"
)

// Classifier evaluates Events against rules to determine the resulting action.
type Classifier struct {
	rules []config.RuleConfig
}

// NewClassifier initializes a Classifier with the provided rules.
func NewClassifier(rules []config.RuleConfig) *Classifier {
	return &Classifier{
		rules: rules,
	}
}

// Classify determines the appropriate action and priority for an event.
// Returns action (e.g. "queue_review", "auto_save", "ignore") and priority.
func (c *Classifier) Classify(e Event) (action, priority string) {
	// Default fallbacks
	action = "queue_review"
	priority = e.Priority
	if priority == "" {
		priority = "normal"
	}

	for _, rule := range c.rules {
		if c.matches(e, rule.Match) {
			action = rule.Action
			if rule.Priority != "" {
				priority = rule.Priority
			}
			break // First matching rule wins
		}
	}

	return action, priority
}

func (c *Classifier) matches(e Event, m config.RuleMatch) bool {
	// Source match
	if m.Source != "" && m.Source != e.Source {
		return false
	}

	// Field target
	if m.Field != "" {
		targetVal := ""
		switch m.Field {
		case "category":
			targetVal = e.Category
		case "priority":
			targetVal = e.Priority
		case "title":
			targetVal = e.Title
		}

		// Exact match
		if m.Value != "" && targetVal != m.Value {
			return false
		}

		// Regex match
		if m.Pattern != "" {
			matched, err := regexp.MatchString(m.Pattern, targetVal)
			if err != nil || !matched {
				return false
			}
		}
	}

	return true
}
