package typesafe

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestQuestionWireForms(t *testing.T) {
	cases := []struct {
		name string
		q    Question
		want string
	}{
		{"noul default", Noul{}, `{"type":"noul"}`},
		{"noul empty criteria", Noul{Criteria: NoulCriteria{}}, `{"criteria":{},"type":"noul"}`},
		{"noul null criteria", Noul{Criteria: NoulCriteria{"true": nil}}, `{"criteria":{"true":null},"type":"noul"}`},
		{"noul structured", Noul{Instructions: map[string]any{"task": []any{nil, true, 4}}}, `{"instructions":{"task":[null,true,4]},"type":"noul"}`},
		{"choice null", Choice{Criteria: map[string]any{"a": nil, "b": "B"}}, `{"criteria":{"a":null,"b":"B"},"type":"choice"}`},
		{"choice empty", Choice{Criteria: map[string]any{}}, `{"criteria":{},"type":"choice"}`},
		{"score", Score{Instructions: "rate", Criteria: []any{"low", []any{nil, 1}}}, `{"criteria":["low",[null,1]],"instructions":"rate","type":"score"}`},
		{"raw future", RawQuestion{"type": "future", "instructions": nil, "extra": true}, `{"extra":true,"instructions":null,"type":"future"}`},
		{"raw known", RawQuestion{"type": "choice", "criteria": nil}, `{"criteria":null,"type":"choice"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.q)
			if err != nil || string(got) != tc.want {
				t.Fatalf("got %s, %v; want %s", got, err, tc.want)
			}
		})
	}
}

func TestInvalidQuestions(t *testing.T) {
	for _, q := range []Question{
		Noul{Instructions: 1}, Noul{Criteria: NoulCriteria{"yes": "yes"}},
		Noul{Criteria: NoulCriteria{"true": false}}, Choice{},
		Choice{Criteria: map[string]any{"bad": 1}}, Score{},
		Score{Criteria: []any{nil}}, Score{Criteria: []any{false}},
		RawQuestion{}, RawQuestion{"type": ""}, RawQuestion{"type": 1},
		RawQuestion{"type": "choice"}, RawQuestion{"type": "score"},
		RawQuestion{"type": "score", "criteria": []any{}},
		RawQuestion{"type": "score", "criteria": nil},
		RawQuestion{"type": "score", "criteria": false},
		RawQuestion{"type": "score", "criteria": 0},
		RawQuestion{"type": "future", "bad": make(chan int)},
	} {
		if _, err := json.Marshal(q); err == nil {
			t.Errorf("accepted invalid question %#v", q)
		}
	}
}

func TestJSONContent(t *testing.T) {
	cycle := map[string]any{}
	cycle["self"] = cycle
	for _, bad := range []any{nil, true, 42, 1.5, []any{math.Inf(1)}, map[string]any{"f": func() {}}, cycle, json.RawMessage(`null`)} {
		if err := ValidateJSONContent(bad); err == nil {
			t.Errorf("accepted invalid content %T", bad)
		}
	}
	for _, good := range []any{"", []any{}, map[string]any{}, map[string]any{"nil": nil, "bool": true, "num": 2}, json.RawMessage(`{"a":null}`)} {
		if err := ValidateJSONContent(good); err != nil {
			t.Errorf("rejected valid content %T: %v", good, err)
		}
	}
}

func TestRequest(t *testing.T) {
	r := SystemOneRequest{State: []any{nil, "state"}, Model: "jev-latest", Questions: map[string]Question{"yes": Noul{}}}
	got, err := json.Marshal(r)
	want := `{"state":[null,"state"],"model":"jev-latest","questions":{"yes":{"type":"noul"}}}`
	if err != nil || string(got) != want {
		t.Fatalf("got %s, %v; want %s", got, err, want)
	}
	for _, state := range []any{nil, false, 1, map[string]any{"bad": make(chan int)}} {
		r.State = state
		if _, err := json.Marshal(r); err == nil || !strings.Contains(err.Error(), "state") {
			t.Errorf("state %T: %v", state, err)
		}
	}
	r.State = "text"
	r.Questions = nil
	if r.Validate() == nil {
		t.Fatal("accepted empty questions")
	}
	var typedNil *Noul
	for _, question := range []Question{nil, typedNil, RawQuestion{"type": ""}} {
		r.Questions = map[string]Question{"bad": question}
		if err := r.Validate(); err == nil || !strings.Contains(err.Error(), "bad") {
			t.Errorf("question %T: %v", question, err)
		}
	}
}
