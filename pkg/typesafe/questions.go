// Package typesafe provides typed questions and a client for the TypeSafe API.
package typesafe

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// JSONContent is text, a JSON object, or a JSON array. Nested JSON scalars and
// nulls are allowed. Values are checked when the question or request is encoded.
type JSONContent = any

// Question supports typed primitives, raw questions, and custom wire encoders.
type Question interface{ json.Marshaler }

// NoulCriteria describes the true and false outcomes. Missing keys are omitted;
// explicitly supplied nil values are preserved as JSON null.
type NoulCriteria map[string]JSONContent

// Noul asks a yes/no question. Its zero value is a valid question.
type Noul struct {
	Instructions JSONContent
	Criteria     NoulCriteria
}

// Choice selects one of the labels in Criteria. A nil description uses the label
// alone; an empty (but non-nil) criteria map is accepted by the upstream schema.
type Choice struct {
	Instructions JSONContent
	Criteria     map[string]JSONContent
}

// Score rates content on the ordered, nonempty rubric, starting at zero.
type Score struct {
	Instructions JSONContent
	Criteria     []JSONContent
}

// RawQuestion preserves all supplied fields, including future question types.
type RawQuestion map[string]any

func isNil(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return r.IsNil()
	}
	return false
}

// ValidateJSONContent verifies the outer content kind and JSON encodability.
func ValidateJSONContent(v JSONContent) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(data) == 0 || (data[0] != '"' && data[0] != '{' && data[0] != '[') {
		return fmt.Errorf("content must be a string, object, or array")
	}
	return nil
}

func optionalContent(v any) error {
	if isNil(v) {
		return nil
	}
	return ValidateJSONContent(v)
}

func (q Noul) MarshalJSON() ([]byte, error) {
	for key, value := range q.Criteria {
		if key != "true" && key != "false" {
			return nil, fmt.Errorf("noul criteria key must be true or false")
		}
		if err := optionalContent(value); err != nil {
			return nil, fmt.Errorf("noul criteria %s: %w", key, err)
		}
	}
	return marshalQuestion("noul", q.Instructions, q.Criteria)
}

func (q Choice) MarshalJSON() ([]byte, error) {
	if q.Criteria == nil {
		return nil, fmt.Errorf("choice criteria are required")
	}
	for key, value := range q.Criteria {
		if err := optionalContent(value); err != nil {
			return nil, fmt.Errorf("choice criteria %s: %w", key, err)
		}
	}
	return marshalQuestion("choice", q.Instructions, q.Criteria)
}

func (q Score) MarshalJSON() ([]byte, error) {
	if len(q.Criteria) == 0 {
		return nil, fmt.Errorf("score criteria must not be empty")
	}
	for i, value := range q.Criteria {
		if err := ValidateJSONContent(value); err != nil {
			return nil, fmt.Errorf("score criteria %d: %w", i, err)
		}
	}
	return marshalQuestion("score", q.Instructions, q.Criteria)
}

func marshalQuestion(kind string, instructions, criteria any) ([]byte, error) {
	if err := optionalContent(instructions); err != nil {
		return nil, fmt.Errorf("instructions: %w", err)
	}
	wire := map[string]any{"type": kind}
	if !isNil(instructions) {
		wire["instructions"] = instructions
	}
	if !isNil(criteria) {
		wire["criteria"] = criteria
	}
	return json.Marshal(wire)
}

func (q RawQuestion) MarshalJSON() ([]byte, error) {
	kind, ok := q["type"].(string)
	if !ok || kind == "" {
		return nil, fmt.Errorf("raw question requires a nonempty string type")
	}
	if kind == "choice" || kind == "score" {
		criteria, present := q["criteria"]
		if !present {
			return nil, fmt.Errorf("%s criteria are required", kind)
		}
		if kind == "score" {
			v := reflect.ValueOf(criteria)
			if isNil(criteria) || ((v.Kind() == reflect.Bool || (v.Kind() >= reflect.Int && v.Kind() <= reflect.Float64)) && v.IsZero()) || ((v.Kind() == reflect.Slice || v.Kind() == reflect.Array || v.Kind() == reflect.Map || v.Kind() == reflect.String) && v.Len() == 0) {
				return nil, fmt.Errorf("score criteria must not be empty")
			}
		}
	}
	// Unlike typed primitives, raw fields are never omitted or rewritten.
	return json.Marshal(map[string]any(q))
}

// SystemOneRequest asks named questions about shared State. Model may be left
// empty for a client to fill from its configuration before sending the request.
type SystemOneRequest struct {
	State     JSONContent         `json:"state"`
	Model     string              `json:"model"`
	Questions map[string]Question `json:"questions"`
}

// Validate checks state, question presence, and each question's wire encoding.
func (r SystemOneRequest) Validate() error {
	if err := ValidateJSONContent(r.State); err != nil {
		return fmt.Errorf("state: %w", err)
	}
	if len(r.Questions) == 0 {
		return fmt.Errorf("questions must not be empty")
	}
	for name, q := range r.Questions {
		if isNil(q) {
			return fmt.Errorf("question %s must not be nil", name)
		}
		data, err := json.Marshal(q)
		var raw RawQuestion
		if err == nil {
			err = json.Unmarshal(data, &raw)
		}
		if err == nil {
			_, err = raw.MarshalJSON()
		}
		if err != nil {
			return fmt.Errorf("question %s: %w", name, err)
		}
	}
	return nil
}

func (r SystemOneRequest) MarshalJSON() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	type wire SystemOneRequest
	return json.Marshal(wire(r))
}
