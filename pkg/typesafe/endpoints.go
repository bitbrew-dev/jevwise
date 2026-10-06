package typesafe

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"time"
)

// RequestOptions overrides one call without changing the client. A nil Model
// keeps the request/client model; a non-nil pointer preserves even empty text.
// Timeout zero inherits the client timeout. ExtraBody shallowly overwrites the
// validated System One body, including state/model/questions and explicit nulls.
// Models calls accept only Timeout and Headers. At most one options value is allowed.
// Callers must not mutate referenced maps while a call is being prepared.
type RequestOptions struct {
	Model     *string
	Timeout   time.Duration
	Headers   http.Header
	ExtraBody map[string]any
}

func callOptions(options []RequestOptions) (RequestOptions, error) {
	if len(options) > 1 {
		return RequestOptions{}, errors.New("typesafe: at most one request options value is allowed")
	}
	var out RequestOptions
	if len(options) == 1 {
		out = options[0]
	}
	if out.Timeout < 0 {
		return RequestOptions{}, errors.New("typesafe: timeout must be positive")
	}
	out.Headers = out.Headers.Clone()
	return out, nil
}

func (c *Client) systemOneCall(ctx context.Context, request SystemOneRequest, options []RequestOptions, decode func(*RawResponse) error) (*RawResponse, error) {
	opts, err := callOptions(options)
	if err != nil {
		return nil, err
	}
	// Validate the original questions before ExtraBody overrides them. Encoding
	// through RawMessage preserves large integers and never mutates caller maps.
	if request.Model == "" {
		request.Model = c.defaultModel
	}
	if opts.Model != nil {
		request.Model = *opts.Model
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, errors.New("typesafe: invalid System One request")
	}
	if len(opts.ExtraBody) > 0 {
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(body, &fields) // SystemOneRequest encodes a JSON object.
		for key, value := range opts.ExtraBody {
			encoded, err := json.Marshal(value)
			if err != nil {
				return nil, errors.New("typesafe: invalid extra request body")
			}
			fields[key] = encoded
		}
		body, err = json.Marshal(fields)
		if err != nil {
			return nil, errors.New("typesafe: invalid extra request body")
		}
	}
	return c.call(ctx, http.MethodPost, "/v1/systemone", body, opts, decode)
}

func (c *Client) modelsCall(ctx context.Context, options []RequestOptions, decode func(*RawResponse) error) (*RawResponse, error) {
	opts, err := callOptions(options)
	if err != nil {
		return nil, err
	}
	if opts.Model != nil || opts.ExtraBody != nil {
		return nil, errors.New("typesafe: models calls do not accept model or extra body")
	}
	return c.call(ctx, http.MethodGet, "/v1/models", nil, opts, decode)
}

// call keeps transport and optional decoding within one attempt. The retry
// runner can later repeat this entire unit, including custom decoding failures.
func (c *Client) call(ctx context.Context, method, path string, body []byte, opts RequestOptions, decode func(*RawResponse) error) (*RawResponse, error) {
	raw, err := c.sendOnce(ctx, method, path, body, opts.Timeout, opts.Headers, 0)
	if err == nil && decode != nil {
		err = decode(raw)
	}
	return raw, err
}

// SystemOne sends named questions and decodes the standard answer schema.
func (c *Client) SystemOne(ctx context.Context, request SystemOneRequest, options ...RequestOptions) (*SystemOneResponse, error) {
	var response SystemOneResponse
	_, err := c.systemOneCall(ctx, request, options, func(raw *RawResponse) error { return raw.Decode(&response) })
	if err != nil {
		return nil, err
	}
	return &response, nil
}

// SystemOneInto decodes an independent custom schema within the HTTP attempt.
// Unlike standalone RawResponse.Decode, its failures can participate in retries.
// The destination must be a non-nil pointer accepted by json.Unmarshal.
func (c *Client) SystemOneInto(ctx context.Context, request SystemOneRequest, destination any, options ...RequestOptions) (*RawResponse, error) {
	value := reflect.ValueOf(destination)
	if !value.IsValid() || value.Kind() != reflect.Pointer || value.IsNil() {
		return nil, errors.New("typesafe: decode destination must be a non-nil pointer")
	}
	return c.systemOneCall(ctx, request, options, func(raw *RawResponse) error { return raw.Decode(destination) })
}

// ListModels lists the service's available models and aliases.
func (c *Client) ListModels(ctx context.Context, options ...RequestOptions) (*ListModelsResponse, error) {
	var response ListModelsResponse
	_, err := c.modelsCall(ctx, options, func(raw *RawResponse) error { return raw.Decode(&response) })
	if err != nil {
		return nil, err
	}
	return &response, nil
}

// SystemOneRaw returns validated JSON without imposing the default answer schema.
func (c *Client) SystemOneRaw(ctx context.Context, request SystemOneRequest, options ...RequestOptions) (*RawResponse, error) {
	return c.systemOneCall(ctx, request, options, nil)
}

// ListModelsRaw returns JSON and HTTP metadata for independent custom decoding.
func (c *Client) ListModelsRaw(ctx context.Context, options ...RequestOptions) (*RawResponse, error) {
	return c.modelsCall(ctx, options, nil)
}

// Decode unmarshals into a caller-supplied Go value. SDK response types receive
// full HTTP metadata only after successful decoding. Other values are unchanged
// by metadata attachment. As with json.Unmarshal, failed decoding may partially
// modify the destination; error strings never include the payload or field path.
// This standalone helper makes no HTTP requests and performs no retries.
func (r *RawResponse) Decode(destination any) error {
	if r == nil {
		return &ResponseValidationError{Path: "$", Err: errors.New("nil raw response")}
	}
	if err := json.Unmarshal(r.Body, destination); err != nil {
		path := "$"
		var schema *ResponseDecodeError
		var field *json.UnmarshalTypeError
		if errors.As(err, &schema) {
			path = schema.Path
		} else if errors.As(err, &field) && field.Field != "" {
			path = field.Field
		}
		return &ResponseValidationError{Path: path, Err: err}
	}
	switch response := destination.(type) {
	case *SystemOneResponse:
		response.Raw = r
	case *ListModelsResponse:
		response.Raw = r
	}
	return nil
}
