package typesafe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// APIErrorKind classifies HTTP failures while preserving their status code.
type APIErrorKind string

const requestIDHeader = "X-Typesafe-Request-Id"

const (
	BadRequest          APIErrorKind = "bad_request"
	Authentication      APIErrorKind = "authentication"
	PermissionDenied    APIErrorKind = "permission_denied"
	NotFound            APIErrorKind = "not_found"
	UnprocessableEntity APIErrorKind = "unprocessable_entity"
	RateLimit           APIErrorKind = "rate_limit"
	InternalServer      APIErrorKind = "internal_server"
	Other               APIErrorKind = "other"
)

// APIError contains copied response metadata. Body and Headers are inspectable
// but may contain sensitive service data; do not log them indiscriminately.
// Error formats the sanitized endpoint, status, server message and request ID.
type APIError struct {
	Kind       APIErrorKind
	StatusCode int
	Method     string
	URL        string
	RequestID  string
	Message    string
	Headers    http.Header
	Body       []byte
}

func (e *APIError) Error() string {
	text := fmt.Sprintf("%s %s: %d", e.Method, safeEndpoint(e.URL), e.StatusCode)
	if e.Message != "" {
		text += " " + e.Message
	}
	if e.RequestID != "" {
		text += " (request_id=" + e.RequestID + ")"
	}
	return text
}

func newAPIError(status int, method, endpoint string, headers http.Header, body []byte) *APIError {
	kind := Other
	switch status {
	case 400:
		kind = BadRequest
	case 401:
		kind = Authentication
	case 403:
		kind = PermissionDenied
	case 404:
		kind = NotFound
	case 422:
		kind = UnprocessableEntity
	case 429:
		kind = RateLimit
	default:
		if status >= 500 {
			kind = InternalServer
		}
	}
	return &APIError{Kind: kind, StatusCode: status, Method: method, URL: safeEndpoint(endpoint),
		RequestID: headers.Get(requestIDHeader), Message: apiMessage(body), Headers: headers.Clone(), Body: bytes.Clone(body)}
}

func safeEndpoint(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil || !u.IsAbs() || u.Host == "" {
		return "<invalid URL>"
	}
	u.User, u.RawQuery, u.Fragment, u.RawFragment, u.ForceQuery = nil, "", "", "", false
	return u.String()
}

// ConnectionError wraps a network cause without exposing its potentially secret
// URL or headers in Error. Inspect the cause with errors.Is or errors.As.
type ConnectionError struct {
	Method, URL string
	Err         error
}

func (e *ConnectionError) Error() string {
	return e.Method + " " + safeEndpoint(e.URL) + ": connection failed"
}
func (e *ConnectionError) Unwrap() error { return e.Err }

// TimeoutError wraps a deadline or timeout cause without printing it.
type TimeoutError struct {
	Method, URL string
	Err         error
}

func (e *TimeoutError) Error() string {
	return e.Method + " " + safeEndpoint(e.URL) + ": operation timed out"
}
func (e *TimeoutError) Unwrap() error { return e.Err }

// ResponseValidationError wraps a response schema failure. The cause remains
// inspectable but is not printed, since it could contain response payload data.
type ResponseValidationError struct {
	Path string
	Err  error
}

func (e *ResponseValidationError) Error() string { return "typesafe: invalid response data" }
func (e *ResponseValidationError) Unwrap() error { return e.Err }

func apiMessage(body []byte) string {
	if len(body) == 0 {
		return "status code (no body)"
	}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return strings.ToValidUTF8(string(body), "\uFFFD")
	}
	if value == nil {
		return "status code (no body)"
	}
	if message, ok := value.(string); ok {
		return message
	}
	if object, ok := value.(map[string]any); ok {
		if message := objectMessage(object); message != "" {
			return message
		}
	}
	compact, _ := json.Marshal(value)
	text := []rune(string(compact))
	if len(text) > 200 {
		return string(text[:200]) + "…"
	}
	return string(text)
}

func objectMessage(object map[string]any) string {
	for _, key := range []string{"error", "message", "detail"} {
		value := object[key]
		if text, ok := value.(string); ok {
			return text
		}
		if nested, ok := value.(map[string]any); ok && key != "message" {
			if text, ok := nested["message"].(string); ok {
				return text
			}
		}
		if list, ok := value.([]any); ok && key == "detail" {
			var messages []string
			for _, item := range list {
				entry, ok := item.(map[string]any)
				if !ok {
					continue
				}
				message, ok := entry["msg"].(string)
				if !ok {
					continue
				}
				var location []string
				if loc, ok := entry["loc"].([]any); ok {
					for _, part := range loc {
						if part != "body" {
							location = append(location, fmt.Sprint(part))
						}
					}
				}
				if len(location) > 0 {
					message = strings.Join(location, ".") + ": " + message
				}
				messages = append(messages, message)
			}
			return strings.Join(messages, "; ")
		}
	}
	return ""
}
