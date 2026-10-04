package typesafe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
)

// Answer identifies one of the supported answer primitives.
type Answer interface{ AnswerType() string }

type NoulAnswer struct {
	Noul float64 `json:"noul"`
}
type ChoiceAnswer struct {
	Choice        string             `json:"choice"`
	Confidence    float64            `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
}
type ScoreAnswer struct {
	Score         float64             `json:"score"`
	Confidence    float64             `json:"confidence"`
	Legend        map[int]JSONContent `json:"legend"`
	Probabilities map[int]float64     `json:"probabilities"`
}

func (NoulAnswer) AnswerType() string   { return "noul" }
func (ChoiceAnswer) AnswerType() string { return "choice" }
func (ScoreAnswer) AnswerType() string  { return "score" }

func (a NoulAnswer) MarshalJSON() ([]byte, error) {
	type fields NoulAnswer
	return json.Marshal(struct {
		Type string `json:"type"`
		fields
	}{a.AnswerType(), fields(a)})
}
func (a ChoiceAnswer) MarshalJSON() ([]byte, error) {
	type fields ChoiceAnswer
	return json.Marshal(struct {
		Type string `json:"type"`
		fields
	}{a.AnswerType(), fields(a)})
}
func (a ScoreAnswer) MarshalJSON() ([]byte, error) {
	type fields ScoreAnswer
	return json.Marshal(struct {
		Type string `json:"type"`
		fields
	}{a.AnswerType(), fields(a)})
}

// Usage permits absent or null token counts.
type Usage struct {
	InputTokens  *int `json:"input_tokens"`
	OutputTokens *int `json:"output_tokens"`
}

// RawResponse captures HTTP metadata without contributing to wire JSON.
type RawResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
	RequestID  string
}

// ResponseDecodeError identifies a malformed field without exposing its value.
type ResponseDecodeError struct {
	Path string
	Err  error
}

func (e *ResponseDecodeError) Error() string { return "invalid response field" }
func (e *ResponseDecodeError) Unwrap() error { return e.Err }

func required[T any](fields map[string]json.RawMessage, key, path string) (v T, err error) {
	data, ok := fields[key]
	if !ok || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return v, &ResponseDecodeError{path, fmt.Errorf("field is required")}
	}
	if err = json.Unmarshal(data, &v); err != nil {
		return v, &ResponseDecodeError{path, err}
	}
	return v, nil
}

func sortedNames[T any](fields map[string]T) []string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func probabilities[K comparable](fields map[string]json.RawMessage, path string, key func(string) (K, error)) (map[K]float64, error) {
	values, err := required[map[string]json.RawMessage](fields, "probabilities", path+".probabilities")
	if err != nil {
		return nil, err
	}
	out := make(map[K]float64, len(values))
	for _, name := range sortedNames(values) {
		k, err := key(name)
		if err != nil {
			return nil, &ResponseDecodeError{path + ".probabilities." + name, err}
		}
		n, err := required[float64](values, name, path+".probabilities."+name)
		if err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, nil
}

func decodeAnswer(fields map[string]json.RawMessage, path string) (Answer, error) {
	kind, err := required[string](fields, "type", path+".type")
	if err != nil {
		return nil, err
	}
	switch kind {
	case "noul":
		n, err := required[float64](fields, "noul", path+".noul")
		return NoulAnswer{n}, err
	case "choice":
		var a ChoiceAnswer
		if a.Choice, err = required[string](fields, "choice", path+".choice"); err != nil {
			return nil, err
		}
		if a.Confidence, err = required[float64](fields, "confidence", path+".confidence"); err != nil {
			return nil, err
		}
		a.Probabilities, err = probabilities(fields, path, func(s string) (string, error) { return s, nil })
		return a, err
	case "score":
		var a ScoreAnswer
		if a.Score, err = required[float64](fields, "score", path+".score"); err != nil {
			return nil, err
		}
		if a.Confidence, err = required[float64](fields, "confidence", path+".confidence"); err != nil {
			return nil, err
		}
		legend, err := required[map[string]json.RawMessage](fields, "legend", path+".legend")
		if err != nil {
			return nil, err
		}
		a.Legend = make(map[int]JSONContent, len(legend))
		for _, name := range sortedNames(legend) {
			raw := legend[name]
			k, err := strconv.Atoi(name)
			if err == nil {
				err = ValidateJSONContent(raw)
			}
			if err != nil {
				return nil, &ResponseDecodeError{path + ".legend." + name, err}
			}
			var value any
			if err := json.Unmarshal(raw, &value); err != nil {
				return nil, &ResponseDecodeError{path + ".legend." + name, err}
			}
			a.Legend[k] = value
		}
		a.Probabilities, err = probabilities(fields, path, strconv.Atoi)
		return a, err
	}
	return nil, nil // Future types are retained in Raw, but not typed Answers.
}

// SystemOneResponse groups typed answers by their caller-supplied names.
type SystemOneResponse struct {
	Model   string            `json:"model"`
	Usage   Usage             `json:"usage"`
	Answers map[string]Answer `json:"answers"`
	Raw     *RawResponse      `json:"-"`
}

func (r *SystemOneResponse) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return &ResponseDecodeError{"$", err}
	}
	var out SystemOneResponse
	var err error
	if out.Model, err = required[string](fields, "model", "model"); err != nil {
		return err
	}
	usage, err := required[map[string]json.RawMessage](fields, "usage", "usage")
	if err != nil {
		return err
	}
	tokenDestinations := []**int{&out.Usage.InputTokens, &out.Usage.OutputTokens}
	for i, name := range []string{"input_tokens", "output_tokens"} {
		if raw, ok := usage[name]; ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			n, err := required[int](usage, name, "usage."+name)
			if err != nil {
				return err
			}
			*tokenDestinations[i] = &n
		}
	}
	out.Answers = map[string]Answer{}
	if _, ok := fields["answers"]; ok {
		answers, err := required[map[string]json.RawMessage](fields, "answers", "answers")
		if err != nil {
			return err
		}
		for _, name := range sortedNames(answers) {
			path := "answers." + name
			answer, err := required[map[string]json.RawMessage](answers, name, path+".type")
			if err != nil {
				return err
			}
			a, err := decodeAnswer(answer, path)
			if err != nil {
				return err
			}
			if a != nil {
				out.Answers[name] = a
			}
		}
	}
	out.Raw = &RawResponse{Body: append([]byte(nil), data...)}
	*r = out
	return nil
}

func (r SystemOneResponse) Nouls() map[string]NoulAnswer {
	return answersOf[NoulAnswer](r.Answers)
}
func (r SystemOneResponse) Choices() map[string]ChoiceAnswer {
	return answersOf[ChoiceAnswer](r.Answers)
}
func (r SystemOneResponse) Scores() map[string]ScoreAnswer {
	return answersOf[ScoreAnswer](r.Answers)
}
func answersOf[T Answer](answers map[string]Answer) map[string]T {
	out := map[string]T{}
	for name, answer := range answers {
		if a, ok := answer.(T); ok {
			out[name] = a
		}
	}
	return out
}

// ModelMetadata describes a model name or alias accepted by System One.
type ModelMetadata struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ReleaseDate string `json:"release_date"`
}
type ListModelsResponse struct {
	Models []ModelMetadata `json:"models"`
	Raw    *RawResponse    `json:"-"`
}

func (r *ListModelsResponse) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return &ResponseDecodeError{"$", err}
	}
	models, err := required[[]map[string]json.RawMessage](fields, "models", "models")
	if err != nil {
		return err
	}
	out := ListModelsResponse{Models: make([]ModelMetadata, len(models))}
	for i, model := range models {
		destinations := []*string{&out.Models[i].Name, &out.Models[i].Description, &out.Models[i].ReleaseDate}
		for j, name := range []string{"name", "description", "release_date"} {
			if *destinations[j], err = required[string](model, name, fmt.Sprintf("models[%d].%s", i, name)); err != nil {
				return err
			}
		}
	}
	out.Raw = &RawResponse{Body: append([]byte(nil), data...)}
	*r = out
	return nil
}
