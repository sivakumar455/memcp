// Package common provides shared utilities across memcp subsystems.
package common

// StopWords contains common words excluded from keyword extraction
// in both evolution and skill evolution pipelines.
var StopWords = map[string]bool{
	"the": true, "and": true, "for": true, "not": true, "with": true,
	"from": true, "this": true, "that": true, "are": true, "was": true,
	"has": true, "had": true, "env": true, "get": true, "set": true,
	"obs": true, "pod": true, "trace": true, "tool": true, "error": true,
	"failed": true, "issue": true, "status": true, "value": true, "use": true,
	"com": true, "org": true, "net": true, "www": true, "https": true, "http": true,
}
