// Package shim implements transparent observation between the IDE and backend MCP.
package shim

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sivakumar455/memcp/internal/observation"
)

type pendingCall struct {
	ToolName  string
	ArgsJSON  string
	StartTime time.Time
}

// Sniffer processes bidirectional JSON-RPC streams to intercept tools/call.
type Sniffer struct {
	observer *observation.Observer
	backend  string
	
	pending map[string]pendingCall
	mu      sync.Mutex
}

// NewSniffer creates a JSON-RPC sniffer.
func NewSniffer(observer *observation.Observer, backendName string) *Sniffer {
	return &Sniffer{
		observer: observer,
		backend:  backendName,
		pending:  make(map[string]pendingCall),
	}
}

// JSONRPC envelope structs for partial extraction without slowing down the stream
type rpcRequest struct {
	ID     interface{}            `json:"id"`
	Method string                 `json:"method"`
	Params map[string]interface{} `json:"params"`
}

type rpcResponse struct {
	ID     interface{}            `json:"id"`
	Method string                 `json:"method"` // Responses shouldn't have method, but let's be safe
	Result map[string]interface{} `json:"result"`
	Error  interface{}            `json:"error"`
}

// SniffRequest processes lines from IDE to backend (Stdin).
// Never blocks the pipe. Always execute async.
func (s *Sniffer) SniffRequest(line []byte) {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return
	}

	if req.Method == "tools/call" && req.Params != nil && req.ID != nil {
		toolName, _ := req.Params["name"].(string)
		args, _ := req.Params["arguments"].(map[string]interface{})
		
		// If arguments wasn't parsed as map[string]interface{}, check if it's already a string 
		// (though standard MCP is map). Provide best effort re-marshal.
		var argsJSON string
		if argsBytes, err := json.Marshal(args); err == nil {
			argsJSON = string(argsBytes)
		} else {
			argsJSON = "{}"
		}

		callID := normalizeID(req.ID)
		
		s.mu.Lock()
		s.pending[callID] = pendingCall{
			ToolName:  toolName,
			ArgsJSON:  argsJSON,
			StartTime: time.Now(),
		}
		s.mu.Unlock()
	}
}

// SniffResponse processes lines from backend to IDE (Stdout).
func (s *Sniffer) SniffResponse(line []byte) {
	var res rpcResponse
	if err := json.Unmarshal(line, &res); err != nil {
		return
	}

	// standard jsonrpc responses have ID but no Method
	if res.ID != nil && res.Method == "" {
		callID := normalizeID(res.ID)
		
		s.mu.Lock()
		pending, exists := s.pending[callID]
		if exists {
			delete(s.pending, callID)
		}
		s.mu.Unlock()

		if exists {
			elapsed := int(time.Since(pending.StartTime).Milliseconds())
			
			// Process response body stringification
			var resultStr string
			if res.Error != nil {
				eb, _ := json.Marshal(res.Error)
				resultStr = "Error: " + string(eb)
			} else if res.Result != nil {
				// Try to extract standard MCP 'content' array
				if content, ok := res.Result["content"].([]interface{}); ok {
					var sb strings.Builder
					for _, c := range content {
						if cmap, ok := c.(map[string]interface{}); ok {
							if txt, ok := cmap["text"].(string); ok {
								sb.WriteString(txt)
								sb.WriteString("\n")
							}
						}
					}
					resultStr = sb.String()
				}
				
				if resultStr == "" {
					// Fallback: dump raw result
					rb, _ := json.Marshal(res.Result)
					resultStr = string(rb)
				}
			}

			// Pass straight to observer
			s.observer.Observe(pending.ToolName, s.backend, pending.ArgsJSON, resultStr, elapsed)
		}
	}
}

func normalizeID(id interface{}) string {
	switch v := id.(type) {
	case string:
		return v
	case float64:
		// json decodes numbers as float64 by default
		return javaScriptNumberToString(v)
	default:
		return ""
	}
}

func javaScriptNumberToString(f float64) string {
	// If it has no decimal part, print as integer
	if f == float64(int64(f)) {
		return fmt.Sprintf("%d", int64(f))
	}
	return fmt.Sprintf("%f", f)
}
