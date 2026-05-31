package daemon

import (
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"

	"github.com/sivakumar455/memcp/internal/config"
)

// ClassifierAction represents the action to take for a classified event.
type ClassifierAction string

const (
	ActionAutoSave      ClassifierAction = "auto_save"
	ActionQueueReview   ClassifierAction = "queue_review"
	ActionAutoSaveQueue ClassifierAction = "auto_save_and_queue"
	ActionIgnore        ClassifierAction = "ignore"
	ActionAutoRun       ClassifierAction = "auto_run"
)

// ClassifiedEvent pairs an Event with its classified action and priority.
type ClassifiedEvent struct {
	Event    Event
	Priority string
	Action   ClassifierAction
}

// Classifier evaluates Events against rules to determine the resulting action.
type Classifier struct {
	rules    []config.ClassifierRule
	compiled []*compiledRule
}

type compiledRule struct {
	rule    config.ClassifierRule
	pattern *regexp.Regexp
}

// NewClassifier initializes a Classifier with the provided rules.
func NewClassifier(rules []config.ClassifierRule) *Classifier {
	c := &Classifier{rules: rules}
	for _, r := range rules {
		cr := &compiledRule{rule: r}
		if r.Match.Pattern != "" {
			re, err := regexp.Compile(r.Match.Pattern)
			if err != nil {
				slog.Warn("Invalid classifier rule pattern", "pattern", r.Match.Pattern, "error", err)
				continue
			}
			cr.pattern = re
		}
		c.compiled = append(c.compiled, cr)
	}
	slog.Info("Classifier initialized", "rules", len(c.compiled))
	return c
}

// Classify determines the appropriate action and priority for an event.
func (c *Classifier) Classify(event Event) ClassifiedEvent {
	result := ClassifiedEvent{
		Event:    event,
		Priority: "normal",
		Action:   ActionQueueReview,
	}

	for _, cr := range c.compiled {
		if !c.matches(cr, event) {
			continue
		}
		if cr.rule.Priority != "" {
			result.Priority = cr.rule.Priority
		}
		if cr.rule.Action != "" {
			result.Action = ClassifierAction(cr.rule.Action)
		}
		slog.Debug("Classifier matched rule",
			"source", event.Source, "source_id", event.SourceID,
			"matched_field", cr.rule.Match.Field, "action", result.Action, "priority", result.Priority,
		)
		return result
	}

	return result
}

func (c *Classifier) matches(cr *compiledRule, event Event) bool {
	rule := cr.rule.Match

	if rule.Source != "" && rule.Source != event.Source {
		return false
	}

	if rule.Field == "" {
		return true
	}

	fieldValue := getFieldValue(event, rule.Field)

	if rule.Value != "" {
		return strings.EqualFold(fieldValue, rule.Value)
	}

	if cr.pattern != nil {
		return cr.pattern.MatchString(fieldValue)
	}

	return fieldValue != ""
}

func getFieldValue(event Event, field string) string {
	switch strings.ToLower(field) {
	case "source":
		return event.Source
	case "source_id", "sourceid", "key":
		return event.SourceID
	case "title":
		return event.Title
	case "summary":
		return event.Summary
	case "category", "type":
		return event.Category
	default:
		if event.RawData != nil {
			if v, ok := event.RawData[field]; ok {
				switch val := v.(type) {
				case string:
					return val
				default:
					b, _ := json.Marshal(val)
					return string(b)
				}
			}
		}
		return ""
	}
}
