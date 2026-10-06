package mcpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A result's JSON occurs twice on the wire, once as structured output and once
// as escaped text. Bound individual events, not a successful response stream.
const httpEventLimit = 32 << 20

// The pinned SDK can emit raw input in both HTTP and JSON-RPC errors. Buffer
// only one frame, sanitize errors, and retain incremental SSE flush behavior.
type httpPrivacyWriter struct {
	http.ResponseWriter
	status    int
	pending   []byte
	failed    error
	committed bool
	errorJSON bool
}

func (w *httpPrivacyWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.errorJSON = status >= 400 && w.Header().Get("Content-Type") == "application/json"
	if w.errorJSON {
		return
	} // Validate and redact the envelope before commitment.
	w.commit()
	if status >= 400 {
		_, w.failed = io.WriteString(w.ResponseWriter, "MCP request rejected\n")
	}
}

func (w *httpPrivacyWriter) commit() {
	w.Header().Del("Mcp-Session-Id")
	w.Header().Del("Content-Length")
	if w.status >= 400 && !w.errorJSON {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	w.ResponseWriter.WriteHeader(w.status)
	w.committed = true
}

func (w *httpPrivacyWriter) Write(data []byte) (int, error) {
	if w.status >= 400 && (!w.errorJSON || w.committed) {
		return len(data), w.failed
	}
	if w.failed != nil {
		return 0, w.failed
	}
	count := len(data)
	for len(data) != 0 {
		n := min(len(data), httpEventLimit+1-len(w.pending))
		w.pending = append(w.pending, data[:n]...)
		data = data[n:]
		if stringsSSE(w.Header()) {
			for {
				end := bytes.Index(w.pending, []byte("\n\n"))
				if end < 0 {
					break
				}
				if end+2 > httpEventLimit {
					return w.fail(errors.New("MCP frame is too large"))
				}
				frame, err := sanitizeEvent(w.pending[:end+2])
				if err != nil {
					return w.fail(err)
				}
				if err := w.emit(frame); err != nil {
					return w.fail(err)
				}
				w.pending = w.pending[end+2:]
			}
		} else if json.Valid(w.pending) {
			if len(w.pending) > httpEventLimit {
				return w.fail(errors.New("MCP frame is too large"))
			}
			frame, err := sanitizeRPC(w.pending)
			if w.errorJSON {
				var envelope struct {
					Error json.RawMessage `json:"error"`
				}
				if json.Unmarshal(frame, &envelope) != nil || len(envelope.Error) == 0 || bytes.Equal(envelope.Error, []byte("null")) {
					err = errors.New("invalid MCP error envelope")
				}
			}
			if err != nil {
				return w.fail(err)
			}
			if err := w.emit(frame); err != nil {
				return w.fail(err)
			}
			w.pending = nil
		}
		if len(w.pending) > httpEventLimit {
			return w.fail(errors.New("MCP frame is too large"))
		}
	}
	return count, nil
}

func stringsSSE(header http.Header) bool { return header.Get("Content-Type") == "text/event-stream" }

func (w *httpPrivacyWriter) emit(data []byte) error {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if !w.committed {
		w.commit()
	}
	n, err := w.ResponseWriter.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	return err
}

func (w *httpPrivacyWriter) fail(err error) (int, error) {
	w.pending = nil
	w.failed = err
	if w.status == 0 {
		w.WriteHeader(http.StatusInternalServerError)
	}
	return 0, err
}

func (w *httpPrivacyWriter) Flush() {
	if w.errorJSON && !w.committed {
		return
	}
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *httpPrivacyWriter) finish() {
	w.pending = nil // Never publish incomplete frames.
	if w.errorJSON && !w.committed {
		w.errorJSON = false
		w.commit()
		_, _ = io.WriteString(w.ResponseWriter, "MCP request rejected\n")
	}
}

func sanitizeEvent(frame []byte) ([]byte, error) {
	var payload, metadata bytes.Buffer
	for _, line := range bytes.Split(frame[:len(frame)-2], []byte("\n")) {
		if bytes.HasPrefix(line, []byte("data:")) {
			if payload.Len() > 0 {
				payload.WriteByte('\n')
			}
			payload.Write(bytes.TrimPrefix(line[5:], []byte(" ")))
		} else {
			metadata.Write(line)
			metadata.WriteByte('\n')
		}
	}
	if payload.Len() == 0 {
		return frame, nil
	}
	data, err := sanitizeRPC(payload.Bytes())
	if err != nil {
		return nil, err
	}
	metadata.WriteString("data: ")
	metadata.Write(data)
	metadata.WriteString("\n\n")
	return metadata.Bytes(), nil
}

func sanitizeRPC(data []byte) ([]byte, error) {
	var message map[string]json.RawMessage
	if err := json.Unmarshal(data, &message); err != nil || !bytes.Equal(message["jsonrpc"], []byte(`"2.0"`)) {
		return nil, errors.New("invalid MCP response frame")
	}
	if failure := message["error"]; len(failure) != 0 && !bytes.Equal(failure, []byte("null")) {
		var details struct {
			Code int             `json:"code"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(failure, &details); err != nil {
			return nil, errors.New("invalid MCP error frame")
		}
		safe := struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Data    any    `json:"data,omitempty"`
		}{Code: details.Code, Message: "MCP request failed"}
		if details.Code == mcp.CodeUnsupportedProtocolVersion {
			var versions struct {
				Supported []string `json:"supported"`
			}
			known := mcp.SupportedProtocolVersions()
			valid := json.Unmarshal(details.Data, &versions) == nil && len(versions.Supported) > 0 && len(versions.Supported) <= len(known)
			for _, version := range versions.Supported {
				valid = valid && slices.Contains(known, version)
			}
			if valid {
				safe.Data = versions
			} // Keep only verified public revision names.
		}
		message["error"], _ = json.Marshal(safe)
		return json.Marshal(message)
	}
	return data, nil
}
