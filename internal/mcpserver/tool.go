// Package mcpserver adapts decisions without owning services or transports.
package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"time"
	"unicode/utf8"

	"github.com/bitbrew-dev/jevwise/internal/buildinfo"
	"github.com/bitbrew-dev/jevwise/internal/debuglog"
	"github.com/bitbrew-dev/jevwise/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const inputLimit = 1 << 20
const outputLimit = 8 << 20

var inputSchema = json.RawMessage(`{
 "type":"object","additionalProperties":false,"required":["prompt","options"],
 "properties":{"prompt":{"type":"string","minLength":1},
 "options":{"type":"array","minItems":2,"items":{"type":"string","minLength":1}}}
}`)
var outputSchema = json.RawMessage(`{
 "type":"object","additionalProperties":false,
 "required":["model","choice","confidence","probabilities"],
 "properties":{"model":{"type":"string"},"choice":{"type":"string"},
 "confidence":{"type":"number"},
 "probabilities":{"type":["object","null"],"additionalProperties":{"type":"number"}},
 "usage":{"type":"object","additionalProperties":false,"required":["input_tokens","output_tokens"],
 "properties":{"input_tokens":{"type":["integer","null"]},"output_tokens":{"type":["integer","null"]}}}
 }
}`)

type toolError struct {
	message string
	cause   error
}

func (e *toolError) Error() string { return e.message }
func (e *toolError) Unwrap() error { return e.cause }

func toolFailure(message string, cause error) (*mcp.CallToolResult, error) {
	result := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: message}}}
	result.SetError(&toolError{message, cause}) // Cause is server-only, never on the wire.
	return result, nil
}

// New registers one decide tool. It never closes the caller-owned service.
// Each call has a timeout and is canceled when either request or lifetime ends.
func New(lifetime context.Context, svc service.DecisionService, timeout time.Duration) (*mcp.Server, error) {
	if lifetime == nil || svc == nil || timeout <= 0 {
		return nil, errors.New("invalid decision server configuration")
	}
	switch reflect.ValueOf(svc).Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if reflect.ValueOf(svc).IsNil() {
			return nil, errors.New("decision service is unavailable")
		}
	}
	if err := lifetime.Err(); err != nil {
		return nil, &toolError{"decision server lifetime has ended", err}
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "jevwise", Version: buildinfo.Version}, &mcp.ServerOptions{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	server.AddTool(&mcp.Tool{Name: "decide", Description: "Choose among distinct options using Jev probabilities.",
		InputSchema: inputSchema, OutputSchema: outputSchema}, decisionHandler(lifetime, svc, timeout))
	return server, nil
}

func decisionHandler(lifetime context.Context, svc service.DecisionService, timeout time.Duration) mcp.ToolHandler {
	return func(ctx context.Context, call *mcp.CallToolRequest) (result *mcp.CallToolResult, err error) {
		finish := debuglog.Trace(lifetime, "mcp.decide")
		defer func() {
			loggedErr := err
			if loggedErr == nil && result != nil && result.IsError {
				loggedErr = result.GetError()
			}
			finish(loggedErr)
		}()
		if ctx == nil || call == nil || call.Params == nil || len(call.Params.Arguments) > inputLimit || !utf8.Valid(call.Params.Arguments) {
			return toolFailure("invalid decision input", errors.New("invalid tool request"))
		}
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		stop := context.AfterFunc(lifetime, cancel)
		defer stop()
		callError := func() error {
			if err := lifetime.Err(); err != nil {
				cancel()
				return err
			}
			return ctx.Err()
		}
		if err := callError(); err != nil {
			return toolFailure("decision canceled or timed out", err)
		}
		decoder := json.NewDecoder(bytes.NewReader(call.Params.Arguments))
		if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
			return toolFailure("invalid decision input", errors.New("expected input object"))
		}
		fields := make(map[string]json.RawMessage, 2)
		for decoder.More() {
			token, err := decoder.Token()
			name, ok := token.(string)
			if err != nil || !ok || (name != "prompt" && name != "options") || fields[name] != nil {
				return toolFailure("invalid decision input", errors.New("unknown or duplicate input field"))
			}
			var value json.RawMessage
			if err := decoder.Decode(&value); err != nil {
				return toolFailure("invalid decision input", err)
			}
			fields[name] = value
		}
		if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
			return toolFailure("invalid decision input", errors.New("unterminated input object"))
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return toolFailure("invalid decision input", errors.New("trailing input"))
		}
		var request service.Request
		if err := json.Unmarshal(fields["prompt"], &request.Prompt); err != nil {
			return toolFailure("invalid decision input", err)
		}
		if err := json.Unmarshal(fields["options"], &request.Options); err != nil {
			return toolFailure("invalid decision input", err)
		}

		if err := request.Validate(); err != nil {
			return toolFailure("invalid decision input", err)
		}
		if err := callError(); err != nil {
			return toolFailure("decision canceled or timed out", err)
		}
		response, err := svc.Decide(ctx, request)
		if ctxErr := callError(); ctxErr != nil {
			return toolFailure("decision canceled or timed out", ctxErr)
		}
		if err != nil {
			return toolFailure("cannot obtain Jev decision", err)
		}
		// The concrete Response fields match outputSchema; Marshal rejects non-JSON numbers.
		output, err := json.Marshal(response)
		if err != nil {
			return toolFailure("cannot encode Jev decision", err)
		}
		if len(output) > outputLimit {
			return toolFailure("cannot encode Jev decision", errors.New("decision output is too large"))
		}
		if err := callError(); err != nil {
			return toolFailure("decision canceled or timed out", err)
		}
		return &mcp.CallToolResult{StructuredContent: json.RawMessage(output),
			Content: []mcp.Content{&mcp.TextContent{Text: string(output)}}}, nil
	}
}
