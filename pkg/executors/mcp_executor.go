// Copyright 2025 Rizome Labs, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package executors - E2B MCP executor for remote Python code execution
package executors

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// E2BMCPExecutor is a specialized executor for E2B MCP Server
// It manages sandbox lifecycle and code execution in E2B environments
type E2BMCPExecutor struct {
	// MCP client and session
	client  *mcp.Client
	session *mcp.ClientSession
	mu      sync.RWMutex

	// E2B configuration
	endpoint        string        // E2B MCP server endpoint
	templateID      string        // E2B template ID for sandbox
	sandboxID       string        // Current active sandbox ID
	sandboxTimeout  int           // Sandbox timeout in seconds
	defaultTimeout  time.Duration // Default execution timeout
	autoKillSandbox bool          // Auto-kill sandbox after execution

	// State
	connected      bool
	sandboxCreated bool
}

// E2BMCPExecutorOptions contains configuration for E2B MCP executor
type E2BMCPExecutorOptions struct {
	Endpoint        string        // E2B MCP server endpoint (required)
	TemplateID      string        // E2B template ID (default: "base" for Python)
	SandboxTimeout  int           // Sandbox timeout in seconds (default: 300)
	DefaultTimeout  time.Duration // Execution timeout (default: 30s)
	AutoKillSandbox bool          // Auto-kill sandbox after execution (default: true)
}

// NewE2BMCPExecutor creates a new E2B MCP executor
func NewE2BMCPExecutor(options *E2BMCPExecutorOptions) (*E2BMCPExecutor, error) {
	if options == nil {
		return nil, fmt.Errorf("options cannot be nil")
	}

	if options.Endpoint == "" {
		return nil, fmt.Errorf("endpoint is required")
	}

	// Set defaults
	if options.SandboxTimeout == 0 {
		options.SandboxTimeout = 300 // 5 minutes default
	}
	if options.DefaultTimeout == 0 {
		options.DefaultTimeout = 30 * time.Second
	}
	if options.TemplateID == "" {
		options.TemplateID = "base" // Default Python template
	}

	executor := &E2BMCPExecutor{
		endpoint:        options.Endpoint,
		templateID:      options.TemplateID,
		sandboxTimeout:  options.SandboxTimeout,
		defaultTimeout:  options.DefaultTimeout,
		autoKillSandbox: options.AutoKillSandbox,
		connected:       false,
		sandboxCreated:  false,
	}

	// Initialize MCP client
	executor.client = mcp.NewClient(
		&mcp.Implementation{
			Name:    "smolagents-e2b-executor",
			Version: "v1.0.0",
		},
		nil,
	)

	return executor, nil
}

// Connect establishes connection to the E2B MCP server
func (e2b *E2BMCPExecutor) Connect(ctx context.Context) error {
	e2b.mu.Lock()
	defer e2b.mu.Unlock()

	if e2b.connected {
		return fmt.Errorf("already connected")
	}

	// Create streamable client transport for HTTP
	transport := &mcp.StreamableClientTransport{
		Endpoint: e2b.endpoint,
	}

	// Connect to the E2B MCP server
	session, err := e2b.client.Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to E2B MCP server: %w", err)
	}

	e2b.session = session
	e2b.connected = true

	return nil
}

// CreateSandbox creates a new E2B sandbox environment
func (e2b *E2BMCPExecutor) CreateSandbox(ctx context.Context, timeout int) (string, error) {
	e2b.mu.Lock()
	defer e2b.mu.Unlock()

	if !e2b.connected {
		return "", fmt.Errorf("not connected to E2B MCP server")
	}

	if e2b.sandboxCreated {
		return "", fmt.Errorf("sandbox already created, call KillSandbox first")
	}

	// Use provided timeout or default
	if timeout == 0 {
		timeout = e2b.sandboxTimeout
	}

	// Call create_sandbox tool with templateID and timeout
	result, err := e2b.session.CallTool(ctx, &mcp.CallToolParams{
		Name: "create_sandbox",
		Arguments: map[string]interface{}{
			"templateID": e2b.templateID,
			"timeout":    timeout,
		},
	})

	if err != nil {
		return "", fmt.Errorf("failed to create sandbox: %w", err)
	}

	if result.IsError {
		errorMsg := "sandbox creation failed"
		if len(result.Content) > 0 {
			if textContent, ok := result.Content[0].(*mcp.TextContent); ok {
				errorMsg = textContent.Text
			}
		}
		return "", fmt.Errorf("%s", errorMsg)
	}

	// Extract sandbox ID from result
	// E2B MCP server returns JSON format: {"sandboxID": "xxx"}
	sandboxID := ""

	// First try StructuredContent if available
	if result.StructuredContent != nil {
		if contentMap, ok := result.StructuredContent.(map[string]interface{}); ok {
			if id, exists := contentMap["sandboxID"]; exists {
				if idStr, ok := id.(string); ok {
					sandboxID = idStr
				}
			}
		}
	}

	// If not found in StructuredContent, try parsing TextContent as JSON
	if sandboxID == "" && len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(*mcp.TextContent); ok {
			text := textContent.Text

			// Try to parse as JSON
			var jsonData map[string]interface{}
			if err := json.Unmarshal([]byte(text), &jsonData); err == nil {
				// Successfully parsed JSON
				if id, exists := jsonData["sandboxID"]; exists {
					if idStr, ok := id.(string); ok {
						sandboxID = idStr
					}
				}
			} else {
				// Not JSON, treat as plain text sandboxID
				sandboxID = text
			}
		}
	}

	if sandboxID == "" {
		return "", fmt.Errorf("failed to extract sandbox ID from response")
	}

	e2b.sandboxID = sandboxID
	e2b.sandboxCreated = true

	// Wait for sandbox to be ready (ports need time to open)
	// E2B sandboxes typically need 2-5 seconds to fully initialize
	time.Sleep(3 * time.Second)

	return sandboxID, nil
}

// wrapPythonCode wraps Python code with final_answer function definition
func wrapPythonCode(code string) string {
	finalAnswerDef := `import json
import sys

def final_answer(answer):
    """Provides the final answer to the task."""
    output = {
        "is_final_answer": True,
        "final_answer": answer,
        "variables": {},
        "logs": ""
    }
    json_output = json.dumps(output)
    print(json_output, file=sys.stdout)
    sys.exit(0)

`
	return finalAnswerDef + code
}

// ExecuteCodeInSandbox executes Python code in the sandbox
// It includes retry logic for port readiness issues (502 errors)
func (e2b *E2BMCPExecutor) ExecuteCodeInSandbox(ctx context.Context, sandboxID, code string) (*ExecutionResult, error) {
	e2b.mu.RLock()
	connected := e2b.connected
	e2b.mu.RUnlock()

	if !connected {
		return nil, fmt.Errorf("not connected to E2B MCP server")
	}

	if sandboxID == "" {
		return nil, fmt.Errorf("sandbox ID is required")
	}

	// Wrap code with final_answer function definition
	wrappedCode := wrapPythonCode(code)

	// Retry logic for port readiness (502 errors)
	maxRetries := 3
	retryDelay := 2 * time.Second
	startTime := time.Now()
	var lastErr error
	var lastResult *mcp.CallToolResult

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Wait before retry
			select {
			case <-ctx.Done():
				return &ExecutionResult{
					ExitCode: 1,
					Stderr:   "context cancelled during retry",
					Duration: time.Since(startTime),
				}, ctx.Err()
			case <-time.After(retryDelay):
			}
		}

		// Call execute_code_sandbox tool
		result, err := e2b.session.CallTool(ctx, &mcp.CallToolParams{
			Name: "execute_code_sandbox",
			Arguments: map[string]interface{}{
				"sandbox_id": sandboxID,
				"code":       wrappedCode,
			},
		})

		if err != nil {
			// Check if it's a 502 error (port not ready)
			errStr := err.Error()
			if attempt < maxRetries-1 && (strings.Contains(errStr, "502") || strings.Contains(errStr, "port is not open")) {
				lastErr = err
				continue // Retry
			}
			duration := time.Since(startTime)
			return &ExecutionResult{
				ExitCode: 1,
				Stderr:   err.Error(),
				Duration: duration,
			}, fmt.Errorf("code execution failed: %w", err)
		}

		// Check if result indicates port not ready
		if result.IsError {
			errorMsg := "code execution failed"
			if len(result.Content) > 0 {
				if textContent, ok := result.Content[0].(*mcp.TextContent); ok {
					errorMsg = textContent.Text
				}
			}

			// Check for 502/port errors and retry
			if attempt < maxRetries-1 && (strings.Contains(errorMsg, "502") || strings.Contains(errorMsg, "port is not open")) {
				lastResult = result
				lastErr = fmt.Errorf("%s", errorMsg)
				continue // Retry
			}

			duration := time.Since(startTime)
			return &ExecutionResult{
				ExitCode: 1,
				Stderr:   errorMsg,
				Duration: duration,
			}, fmt.Errorf("%s", errorMsg)
		}

		// Success!
		lastResult = result
		break
	}

	// If we exhausted retries
	if lastResult == nil || lastResult.IsError {
		duration := time.Since(startTime)
		errorMsg := "code execution failed after retries"
		if lastErr != nil {
			errorMsg = lastErr.Error()
		}
		return &ExecutionResult{
			ExitCode: 1,
			Stderr:   errorMsg,
			Duration: duration,
		}, fmt.Errorf("%s", errorMsg)
	}

	duration := time.Since(startTime)

	// Parse execution result
	executionResult := &ExecutionResult{
		ExitCode: 0,
		Duration: duration,
	}

	// Extract output from content (use lastResult which is guaranteed to be set here)
	if lastResult != nil && len(lastResult.Content) > 0 {
		var fullOutput strings.Builder
		var stdoutText strings.Builder
		var stderrText strings.Builder
		
		for _, content := range lastResult.Content {
			if textContent, ok := content.(*mcp.TextContent); ok {
				fullOutput.WriteString(textContent.Text)
				fullOutput.WriteString("\n")
			}
		}
		
		outputText := fullOutput.String()
		executionResult.Output = outputText
		executionResult.Stdout = outputText
		
		// Parse JSON Lines format from E2B
		// E2B returns JSON Lines: each line is a JSON object
		lines := strings.Split(outputText, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			
			// Try to parse as JSON
			var jsonObj map[string]interface{}
			if err := json.Unmarshal([]byte(line), &jsonObj); err != nil {
				// Not JSON, treat as regular stdout
				stdoutText.WriteString(line)
				stdoutText.WriteString("\n")
				continue
			}
			
			// Check for final_answer output
			if isFinal, ok := jsonObj["is_final_answer"].(bool); ok && isFinal {
				executionResult.IsFinalAnswer = true
				if finalAnswer, exists := jsonObj["final_answer"]; exists {
					executionResult.FinalAnswer = finalAnswer
					executionResult.Output = finalAnswer
				}
				if logs, ok := jsonObj["logs"].(string); ok {
					executionResult.Logs = logs
				}
				// Found final answer, return early
				return executionResult, nil
			}
			
			// Handle different E2B output types
			if outputType, ok := jsonObj["type"].(string); ok {
				switch outputType {
				case "stdout":
					if text, ok := jsonObj["text"].(string); ok {
						// Check each line of stdout for final_answer JSON
						textLines := strings.Split(text, "\n")
						foundFinalAnswer := false
						for _, textLine := range textLines {
							textLineTrimmed := strings.TrimSpace(textLine)
							if textLineTrimmed == "" {
								continue
							}
							var finalAnswerJSON map[string]interface{}
							if err := json.Unmarshal([]byte(textLineTrimmed), &finalAnswerJSON); err == nil {
								if isFinal, ok := finalAnswerJSON["is_final_answer"].(bool); ok && isFinal {
									executionResult.IsFinalAnswer = true
									if finalAnswer, exists := finalAnswerJSON["final_answer"]; exists {
										executionResult.FinalAnswer = finalAnswer
										executionResult.Output = finalAnswer
									}
									if logs, ok := finalAnswerJSON["logs"].(string); ok {
										executionResult.Logs = logs
									}
									foundFinalAnswer = true
									break
								}
							}
						}
						if foundFinalAnswer {
							// Found final answer, return early
							return executionResult, nil
						}
						// Not final_answer JSON, treat as regular stdout
						stdoutText.WriteString(text)
					}
				case "stderr", "error":
					if value, ok := jsonObj["value"].(string); ok {
						stderrText.WriteString(value)
						stderrText.WriteString("\n")
					}
					if name, ok := jsonObj["name"].(string); ok {
						stderrText.WriteString(name + ": ")
					}
				}
			}
		}
		
		// Set stdout and stderr
		if stdoutText.Len() > 0 {
			executionResult.Stdout = strings.TrimSpace(stdoutText.String())
		}
		if stderrText.Len() > 0 {
			executionResult.Stderr = strings.TrimSpace(stderrText.String())
			if executionResult.Stderr != "" {
				executionResult.ExitCode = 1
			}
		}
		
		// Try to parse stdout as JSON (in case final_answer was printed but not detected above)
		if executionResult.Stdout != "" {
			var jsonOutput map[string]interface{}
			if err := json.Unmarshal([]byte(executionResult.Stdout), &jsonOutput); err == nil {
				if isFinal, ok := jsonOutput["is_final_answer"].(bool); ok && isFinal {
					executionResult.IsFinalAnswer = true
					if finalAnswer, exists := jsonOutput["final_answer"]; exists {
						executionResult.FinalAnswer = finalAnswer
						executionResult.Output = finalAnswer
					}
					if logs, ok := jsonOutput["logs"].(string); ok {
						executionResult.Logs = logs
					}
				}
			}
		}
	}

	return executionResult, nil
}

// KillSandbox terminates the sandbox environment
func (e2b *E2BMCPExecutor) KillSandbox(ctx context.Context, sandboxID string) error {
	e2b.mu.Lock()
	defer e2b.mu.Unlock()

	if !e2b.connected {
		return fmt.Errorf("not connected to E2B MCP server")
	}

	if sandboxID == "" {
		return fmt.Errorf("sandbox ID is required")
	}

	// Call kill_sandbox tool
	result, err := e2b.session.CallTool(ctx, &mcp.CallToolParams{
		Name: "kill_sandbox",
		Arguments: map[string]interface{}{
			"sandbox_id": sandboxID,
		},
	})

	if err != nil {
		return fmt.Errorf("failed to kill sandbox: %w", err)
	}

	if result.IsError {
		errorMsg := "sandbox termination failed"
		if len(result.Content) > 0 {
			if textContent, ok := result.Content[0].(*mcp.TextContent); ok {
				errorMsg = textContent.Text
			}
		}
		return fmt.Errorf("%s", errorMsg)
	}

	// Clear sandbox state if it matches
	if e2b.sandboxID == sandboxID {
		e2b.sandboxID = ""
		e2b.sandboxCreated = false
	}

	return nil
}

// Execute executes Python code (implements ExecutorInterface)
// This method manages the full sandbox lifecycle: create, execute, cleanup
func (e2b *E2BMCPExecutor) Execute(language, code string, options map[string]interface{}) (*ExecutionResult, error) {
	// Only Python is supported for E2B
	if language != "" && language != "python" {
		return nil, fmt.Errorf("E2B executor only supports Python, got: %s", language)
	}

	// Prepare context with timeout
	timeout := e2b.defaultTimeout
	if options != nil {
		if t, ok := options["timeout"].(time.Duration); ok {
			timeout = t
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Get sandbox timeout from options
	sandboxTimeout := e2b.sandboxTimeout
	if options != nil {
		if t, ok := options["sandbox_timeout"].(int); ok {
			sandboxTimeout = t
		}
	}

	// Check if we should reuse existing sandbox
	reuseExisting := false
	if options != nil {
		if reuse, ok := options["reuse_sandbox"].(bool); ok {
			reuseExisting = reuse
		}
	}

	var sandboxID string
	var err error

	// Create sandbox if needed
	e2b.mu.RLock()
	existingSandbox := e2b.sandboxCreated && e2b.sandboxID != ""
	currentSandboxID := e2b.sandboxID
	e2b.mu.RUnlock()

	if reuseExisting && existingSandbox {
		sandboxID = currentSandboxID
	} else {
		// Kill existing sandbox if any and not reusing
		if existingSandbox {
			_ = e2b.KillSandbox(ctx, currentSandboxID)
		}

		// Create new sandbox
		sandboxID, err = e2b.CreateSandbox(ctx, sandboxTimeout)
		if err != nil {
			return &ExecutionResult{
				ExitCode: 1,
				Stderr:   fmt.Sprintf("failed to create sandbox: %v", err),
			}, err
		}
	}

	// Ensure sandbox cleanup if auto-kill is enabled
	if e2b.autoKillSandbox && !reuseExisting {
		defer func() {
			killCtx, killCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer killCancel()
			_ = e2b.KillSandbox(killCtx, sandboxID)
		}()
	}

	// Execute code in sandbox
	result, err := e2b.ExecuteCodeInSandbox(ctx, sandboxID, code)
	if err != nil {
		return result, err
	}

	return result, nil
}

// ExecutePython is a convenience method for executing Python code
func (e2b *E2BMCPExecutor) ExecutePython(code string) (*ExecutionResult, error) {
	return e2b.Execute("python", code, nil)
}

// ExecutePythonWithOptions executes Python code with custom options
func (e2b *E2BMCPExecutor) ExecutePythonWithOptions(code string, options map[string]interface{}) (*ExecutionResult, error) {
	return e2b.Execute("python", code, options)
}

// GetSandboxID returns the current active sandbox ID
func (e2b *E2BMCPExecutor) GetSandboxID() string {
	e2b.mu.RLock()
	defer e2b.mu.RUnlock()
	return e2b.sandboxID
}

// IsSandboxCreated returns whether a sandbox is currently active
func (e2b *E2BMCPExecutor) IsSandboxCreated() bool {
	e2b.mu.RLock()
	defer e2b.mu.RUnlock()
	return e2b.sandboxCreated
}

// IsConnected returns whether the executor is connected
func (e2b *E2BMCPExecutor) IsConnected() bool {
	e2b.mu.RLock()
	defer e2b.mu.RUnlock()
	return e2b.connected
}

// GetServerInfo returns information about the E2B executor
func (e2b *E2BMCPExecutor) GetServerInfo() map[string]interface{} {
	e2b.mu.RLock()
	defer e2b.mu.RUnlock()

	return map[string]interface{}{
		"type":              "e2b-mcp",
		"connected":         e2b.connected,
		"endpoint":          e2b.endpoint,
		"template_id":       e2b.templateID,
		"sandbox_id":        e2b.sandboxID,
		"sandbox_created":   e2b.sandboxCreated,
		"sandbox_timeout":   e2b.sandboxTimeout,
		"default_timeout":   e2b.defaultTimeout.Seconds(),
		"auto_kill_sandbox": e2b.autoKillSandbox,
	}
}

// ListTools lists available tools on the E2B MCP server
func (e2b *E2BMCPExecutor) ListTools(ctx context.Context) ([]*mcp.Tool, error) {
	e2b.mu.RLock()
	if !e2b.connected {
		e2b.mu.RUnlock()
		return nil, fmt.Errorf("not connected to E2B MCP server")
	}
	e2b.mu.RUnlock()

	result, err := e2b.session.ListTools(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}

	return result.Tools, nil
}

// Close closes the connection and cleans up resources
func (e2b *E2BMCPExecutor) Close() error {
	e2b.mu.Lock()
	defer e2b.mu.Unlock()

	var errors []error

	// Kill sandbox if it exists
	if e2b.sandboxCreated && e2b.sandboxID != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_, err := e2b.session.CallTool(ctx, &mcp.CallToolParams{
			Name: "kill_sandbox",
			Arguments: map[string]interface{}{
				"sandbox_id": e2b.sandboxID,
			},
		})
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to kill sandbox: %w", err))
		}
	}

	// Close MCP session
	if e2b.connected && e2b.session != nil {
		if err := e2b.session.Close(); err != nil {
			errors = append(errors, fmt.Errorf("failed to close MCP session: %w", err))
		}
	}

	e2b.connected = false
	e2b.sandboxCreated = false
	e2b.sandboxID = ""
	e2b.session = nil

	if len(errors) > 0 {
		return fmt.Errorf("errors during close: %v", errors)
	}

	return nil
}

// Reconnect closes the current connection and establishes a new one
func (e2b *E2BMCPExecutor) Reconnect(ctx context.Context) error {
	if err := e2b.Close(); err != nil {
		return fmt.Errorf("failed to close existing connection: %w", err)
	}

	return e2b.Connect(ctx)
}

// ============================================================================
// Compatibility methods for ReactCodeAgent
// ============================================================================

// ExecuteWithResult executes Python code and returns a structured result
// This method is compatible with GoExecutor.ExecuteWithResult
func (e2b *E2BMCPExecutor) ExecuteWithResult(code string) (*ExecutionResult, error) {
	return e2b.Execute("python", code, nil)
}

// ExecuteRaw executes Python code with options (ignores authorizedPackages as E2B handles this internally)
// This method is compatible with GoExecutor.ExecuteRaw
func (e2b *E2BMCPExecutor) ExecuteRaw(code string, authorizedPackages []string) (*ExecutionResult, error) {
	// E2B sandbox manages packages internally, so we ignore authorizedPackages
	return e2b.Execute("python", code, nil)
}

// Reset resets the executor state by killing any existing sandbox
func (e2b *E2BMCPExecutor) Reset() error {
	e2b.mu.Lock()
	defer e2b.mu.Unlock()

	if e2b.sandboxCreated && e2b.sandboxID != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := e2b.KillSandbox(ctx, e2b.sandboxID); err != nil {
			return fmt.Errorf("failed to reset executor: %w", err)
		}
	}

	return nil
}

// GetState returns an empty state (E2B doesn't expose internal sandbox state)
func (e2b *E2BMCPExecutor) GetState() map[string]interface{} {
	return make(map[string]interface{})
}

// SendTools is a no-op for E2B executor (tools are managed by MCP server)
func (e2b *E2BMCPExecutor) SendTools(tools map[string]interface{}) error {
	// E2B MCP server manages tools internally
	return nil
}

// SendVariables is a no-op for E2B executor (variables persist in sandbox if reused)
func (e2b *E2BMCPExecutor) SendVariables(variables map[string]interface{}) error {
	// Variables persist in sandbox between executions if reuse_sandbox is true
	return nil
}
