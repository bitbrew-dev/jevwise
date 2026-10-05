package typesafe

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestResponsePrimitives(t *testing.T) {
	data := []byte(`{"model":"jev-1","usage":{"input_tokens":0,"output_tokens":null},"answers":{
	"n":{"type":"noul","noul":1.2,"ignored":true},
	"c":{"type":"choice","choice":"yes","confidence":-0.1,"probabilities":{"yes":0.6,"no":0.4}},
	"s":{"type":"score","score":1.4,"confidence":0.7,"legend":{"0":"low","1":{"null":null},"2":[null,true]},"probabilities":{"0":0.1,"1":0.4,"2":0.5}},
	"future":{"type":"future","secret":{"ok":true}}}}`)
	var r SystemOneResponse
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	if r.Model != "jev-1" || r.Usage.InputTokens == nil || *r.Usage.InputTokens != 0 || r.Usage.OutputTokens != nil {
		t.Fatalf("model/usage: %+v", r)
	}
	if len(r.Answers) != 3 || r.Nouls()["n"].Noul != 1.2 || r.Choices()["c"].Choice != "yes" || r.Scores()["s"].Score != 1.4 {
		t.Fatalf("typed accessors: %+v", r.Answers)
	}
	if !reflect.DeepEqual(r.Scores()["s"].Legend[2], []any{nil, true}) || r.Scores()["s"].Probabilities[1] != 0.4 {
		t.Fatal("score integer keys or nested null lost")
	}
	if string(r.Raw.Body) != string(data) {
		t.Fatal("original response not preserved")
	}
	data[0] = ' '
	if r.Raw.Body[0] != '{' {
		t.Fatal("raw body aliases input")
	}
	for name, answer := range r.Answers {
		encoded, err := json.Marshal(answer)
		if err != nil || !strings.Contains(string(encoded), `"type":"`+answer.AnswerType()+`"`) {
			t.Errorf("%s wire discriminator: %s, %v", name, encoded, err)
		}
	}
	encoded, err := json.Marshal(r)
	if err != nil || strings.Contains(string(encoded), "future") || strings.Contains(string(encoded), "Body") {
		t.Fatalf("response encoding: %s, %v", encoded, err)
	}
	var roundtrip SystemOneResponse
	if err := json.Unmarshal(encoded, &roundtrip); err != nil || len(roundtrip.Answers) != 3 {
		t.Fatalf("roundtrip: %v", err)
	}
}

func TestResponseDefaults(t *testing.T) {
	for _, body := range []string{`{"model":"m","usage":{}}`, `{"model":"m","usage":{},"answers":{}}`} {
		var r SystemOneResponse
		if err := json.Unmarshal([]byte(body), &r); err != nil || r.Answers == nil || len(r.Answers) != 0 || r.Usage.InputTokens != nil {
			t.Fatalf("defaults: %+v, %v", r, err)
		}
	}
}

func TestMalformedResponsePaths(t *testing.T) {
	cases := []struct{ body, path string }{
		{`{}`, "model"}, {`{"model":null}`, "model"}, {`{"model":false}`, "model"},
		{`{"model":"m"}`, "usage"}, {`{"model":"m","usage":null}`, "usage"},
		{`{"model":"m","usage":[]}`, "usage"}, {`{"model":"m","usage":{"input_tokens":"1"}}`, "usage.input_tokens"},
		{`{"model":"m","usage":{"output_tokens":1.5}}`, "usage.output_tokens"},
		{`{"model":"m","usage":{},"answers":null}`, "answers"},
		{`{"a":null}`, "answers.a.type"}, {`{"a":[]}`, "answers.a.type"}, {`{"a":{}}`, "answers.a.type"},
		{`{"a":{"type":4}}`, "answers.a.type"}, {`{"a":{"type":"noul"}}`, "answers.a.noul"},
		{`{"a":{"type":"noul","noul":"0.5"}}`, "answers.a.noul"}, {`{"a":{"type":"noul","noul":null}}`, "answers.a.noul"},
		{`{"a":{"type":"choice","choice":"x"}}`, "answers.a.confidence"},
		{`{"a":{"type":"choice","confidence":1}}`, "answers.a.choice"},
		{`{"a":{"type":"choice","choice":"x","confidence":1,"probabilities":{"x":null}}}`, "answers.a.probabilities.x"},
		{`{"a":{"type":"choice","choice":"x","confidence":1,"probabilities":[]}}`, "answers.a.probabilities"},
		{`{"a":{"type":"score","score":1,"confidence":1,"legend":[]}}`, "answers.a.legend"},
		{`{"a":{"type":"score","score":1,"confidence":1,"legend":{"x":"bad"}}}`, "answers.a.legend.x"},
		{`{"a":{"type":"score","score":1,"confidence":1,"legend":{"0":null}}}`, "answers.a.legend.0"},
		{`{"a":{"type":"score","score":1,"confidence":1,"legend":{},"probabilities":{"x":1}}}`, "answers.a.probabilities.x"},
	}
	for _, tc := range cases {
		body := tc.body
		if strings.HasPrefix(tc.path, "answers.a") {
			body = `{"model":"m","usage":{},"answers":` + body + `}`
		}
		var r SystemOneResponse
		var err *ResponseDecodeError
		if e := json.Unmarshal([]byte(body), &r); !errors.As(e, &err) || err.Path != tc.path || err.Unwrap() == nil {
			t.Errorf("%s: %v, want %s", body, e, tc.path)
		}
	}
}

func TestListModels(t *testing.T) {
	var r ListModelsResponse
	body := `{"models":[{"name":"m","description":"model","release_date":"2026-09-15","extra":1}]}`
	if err := json.Unmarshal([]byte(body), &r); err != nil || len(r.Models) != 1 || r.Models[0].Name != "m" || string(r.Raw.Body) != body {
		t.Fatalf("models: %+v, %v", r, err)
	}
	if err := json.Unmarshal([]byte(`{"models":[]}`), &r); err != nil || r.Models == nil {
		t.Fatalf("empty models: %+v, %v", r, err)
	}
	for _, tc := range []struct{ body, path string }{
		{`{}`, "models"}, {`{"models":null}`, "models"},
		{`{"models":[{}]}`, "models[0].name"},
		{`{"models":[{"name":"m","description":"d","release_date":1}]}`, "models[0].release_date"},
		{`{"models":[{"name":"m","release_date":"d"}]}`, "models[0].description"},
	} {
		var err *ResponseDecodeError
		if e := json.Unmarshal([]byte(tc.body), &r); !errors.As(e, &err) || err.Path != tc.path {
			t.Errorf("models: %v, want %s", e, tc.path)
		}
	}
}

func TestDecodeStablePathsAndSafeErrors(t *testing.T) {
	for _, tc := range []struct{ answers, path string }{
		{`{"z":{"type":"noul"},"a":{"type":"noul"}}`, "answers.a.noul"},
		{`{"a":{"type":"choice","choice":"x","confidence":1,"probabilities":{"z":null,"a":null}}}`, "answers.a.probabilities.a"},
		{`{"a":{"type":"score","score":1,"confidence":1,"legend":{"z":"bad","a":"bad"}}}`, "answers.a.legend.a"},
		{`{"private-secret":{"type":"noul"}}`, "answers.private-secret.noul"},
	} {
		for i := 0; i < 100; i++ {
			var r SystemOneResponse
			var err *ResponseDecodeError
			body := `{"model":"m","usage":{},"answers":` + tc.answers + `}`
			if e := json.Unmarshal([]byte(body), &r); !errors.As(e, &err) || err.Path != tc.path || strings.Contains(err.Error(), "private-secret") {
				t.Fatalf("unstable or unsafe error: %v, want %s", e, tc.path)
			}
		}
	}
	for i := 0; i < 100; i++ {
		var r SystemOneResponse
		body := `{"model":"m","usage":{},"answers":{"s":{"type":"score","score":1,"confidence":1,"legend":{"1":"one","01":"zero-one"},"probabilities":{"1":0.7,"01":0.3}}}}`
		if err := json.Unmarshal([]byte(body), &r); err != nil || r.Scores()["s"].Legend[1] != "one" || r.Scores()["s"].Probabilities[1] != 0.7 {
			t.Fatalf("unstable integer-key collision: %v", err)
		}
	}
}
