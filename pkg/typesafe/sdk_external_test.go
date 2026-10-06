package typesafe_test

import (
	"encoding/json"
	"testing"

	"github.com/bitbrew-dev/jevwise/pkg/typesafe"
)

func TestPublicSDKUnderRenamedModule(t *testing.T) {
	request := typesafe.SystemOneRequest{State: "Choose a task", Questions: map[string]typesafe.Question{
		"decision": typesafe.Choice{Criteria: map[string]typesafe.JSONContent{"A": nil, "B": nil}},
	}}
	if _, err := json.Marshal(request); err != nil {
		t.Fatal("public request API failed", err)
	}
	raw := &typesafe.RawResponse{StatusCode: 200, Body: []byte(`{"model":"jev","usage":{},"answers":{"decision":{"type":"choice","choice":"B","confidence":0.8,"probabilities":{"A":0.2,"B":0.8}}}}`)}
	var response typesafe.SystemOneResponse
	if err := raw.Decode(&response); err != nil || response.Raw != raw || response.Choices()["decision"].Probabilities["B"] != 0.8 {
		t.Fatal("public response API failed", err)
	}
}
