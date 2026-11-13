package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecideWithGeminiUsesOnlyGoogleCredentials(t *testing.T) {
	decisionEnvironment(t)
	t.Setenv("JEV_API_KEY", "private-jev-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/v1beta/decisions" || r.Header.Get("x-goog-api-key") != "fixture-google-key" || r.Header.Get("Authorization") != "" || strings.Contains(string(body), "private-jev-key") || !strings.Contains(string(body), `"model":"gemini-decision-1"`) {
			t.Error("unexpected Gemini request")
		}
		_, _ = io.WriteString(w, `{"answers":{"decision":{"type":"choice","choice":"Go","confidence":0.8,"probabilities":{"Go":0.8,"Rust":0.2}}},"usage":{"total_input_tokens":7,"total_output_tokens":0}}`)
	}))
	defer server.Close()
	t.Setenv("GOOGLE_API_KEY", "fixture-google-key")
	t.Setenv("GOOGLE_GEMINI_BASE_URL", server.URL)
	out, stderr, err := execute(NewRoot(), "decide", "--provider", "gemini", "--prompt", "Pick a language", "--option", "Go", "--option", "Rust")
	var response struct {
		Model, Choice string
		Probabilities map[string]float64
	}
	if err != nil || stderr != "" || json.Unmarshal([]byte(out), &response) != nil || response.Model != "gemini-decision-1" || response.Choice != "Go" || response.Probabilities["Rust"] != 0.2 {
		t.Fatal("Gemini decision failed", out, err)
	}
	t.Setenv("GOOGLE_API_KEY", "")
	if _, _, err := execute(NewRoot(), "decide", "--provider", "gemini", "--prompt", "p", "--option", "A", "--option", "B"); err == nil || !strings.Contains(err.Error(), "API key is required") {
		t.Fatal("Gemini used a non-Google key", err)
	}
}
