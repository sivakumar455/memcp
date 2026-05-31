package llm

import "encoding/json"

// ParseArgs unmarshals tool call JSON arguments into a struct.
func ParseArgs(argsJSON string, target any) error {
	return json.Unmarshal([]byte(argsJSON), target)
}
