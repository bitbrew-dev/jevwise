package typesafe

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestAPIErrorStatusMapping(t *testing.T) {
	cases := map[int]APIErrorKind{400: BadRequest, 401: Authentication, 403: PermissionDenied,
		404: NotFound, 422: UnprocessableEntity, 429: RateLimit, 500: InternalServer,
		503: InternalServer, 408: Other, 409: Other, 302: Other}
	for status, kind := range cases {
		err := newAPIError(status, "GET", "https://api.typesafe.ai/v1/models", nil, nil)
		if err.Kind != kind || err.StatusCode != status || err.Message != "status code (no body)" {
			t.Errorf("status %d: %#v", status, err)
		}
	}
}

func TestAPIErrorMessageExtraction(t *testing.T) {
	cases := []struct{ body, want string }{
		{"", "status code (no body)"}, {" \n ", " \n "}, {"null", "status code (no body)"},
		{`"plain JSON"`, "plain JSON"}, {"not JSON", "not JSON"},
		{`{"error":"first","message":"second"}`, "first"},
		{`{"error":{"message":"nested"},"message":"second"}`, "nested"},
		{`{"error":{},"message":"second","detail":"third"}`, "second"},
		{`{"message":4,"detail":"third"}`, "third"},
		{`{"detail":{"message":"nested detail"}}`, "nested detail"},
		{`{"error":"","message":"ignored"}`, `{"error":"","message":"ignored"}`},
		{`{"detail":[{"loc":["body","questions","q","score","criteria",0],"msg":"Invalid"},{"msg":"Missing"}]}`, "questions.q.score.criteria.0: Invalid; Missing"},
		{`{"detail":[null,{}, {"loc":["body"],"msg":"Only body"}]}`, "Only body"},
		{`{"future": true}`, `{"future":true}`},
		{`[1,true]`, `[1,true]`},
		{string([]byte{'x', 0xff}), "x\uFFFD"},
	}
	for _, tc := range cases {
		if got := apiMessage([]byte(tc.body)); got != tc.want {
			t.Errorf("body %q: got %q, want %q", tc.body, got, tc.want)
		}
	}
	body := `{"future":"` + strings.Repeat("x", 250) + `"}`
	if got := apiMessage([]byte(body)); got != body[:200]+"…" {
		t.Fatal("fallback JSON was not bounded")
	}
	longMessage := strings.Repeat("x", 250)
	if got := apiMessage([]byte(`{"error":"` + longMessage + `"}`)); got != longMessage {
		t.Fatal("recognized server message was truncated")
	}
}

func TestAPIErrorSafeEndpointAndMetadata(t *testing.T) {
	headers := http.Header{"X-Typesafe-Request-Id": {"req_123"}}
	body := []byte(`{"error":"explanation"}`)
	err := newAPIError(400, "GET", "https://user:password@api.typesafe.ai/v1/models?key=secret#fragment", headers, body)
	want := "GET https://api.typesafe.ai/v1/models: 400 explanation (request_id=req_123)"
	if err.Error() != want || err.URL != "https://api.typesafe.ai/v1/models" {
		t.Fatalf("unsafe or unexpected error: %s", err)
	}
	headers.Set("X-Typesafe-Request-Id", "changed")
	body[0] = 'x'
	if err.Headers.Get("X-Typesafe-Request-Id") != "req_123" || err.Body[0] != '{' {
		t.Fatal("response metadata was not copied")
	}
	if got := safeEndpoint("https://%secret"); got != "<invalid URL>" {
		t.Fatal("invalid URL leaked")
	}
	if got := safeEndpoint("https:opaque-secret"); got != "<invalid URL>" {
		t.Fatal("opaque URL leaked")
	}
	var classified *APIError
	if !errors.As(err, &classified) || classified.Kind != BadRequest {
		t.Fatal("APIError cannot be inspected")
	}
}

func TestWrappedErrorsHideSecretCauses(t *testing.T) {
	cause := &url.Error{Op: "Get", URL: "https://secret@host?secret=true", Err: context.DeadlineExceeded}
	endpoint := "https://user:password@example.com/path?key=secret#secret"
	connection := &ConnectionError{Method: "GET", URL: endpoint, Err: cause}
	timeout := &TimeoutError{Method: "GET", URL: endpoint, Err: cause}
	validation := &ResponseValidationError{Path: "answers.secret", Err: cause}
	for _, err := range []error{connection, timeout, validation} {
		if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "password") {
			t.Fatalf("secret cause disclosed: %s", err)
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("error cause lost: %s", err)
		}
		var underlying *url.Error
		if !errors.As(err, &underlying) || underlying != cause {
			t.Fatal("wrapped error cannot be inspected")
		}
	}
}
