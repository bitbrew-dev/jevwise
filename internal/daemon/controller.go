package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"time"
)

const controlResponseLimit = 4 << 10
const controlTimeout = 3 * time.Second

// Controller authenticates an exact local instance without using its PID.
type Controller struct {
	address  string
	instance string
	token    string
	client   *http.Client
}

type controlError struct{ cause error }

func (e *controlError) Error() string { return "management request failed" }
func (e *controlError) Unwrap() error { return e.cause }

// NewController does not contact a server or read upstream service configuration.
func NewController(address, instance, controlToken string) (*Controller, error) {
	if !controlAddress(address) || !controlID(instance, 16, 128) || !controlID(controlToken, 32, 256) {
		return nil, errors.New("invalid management configuration")
	}
	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            (&net.Dialer{Timeout: controlTimeout, KeepAlive: -1}).DialContext,
		DisableKeepAlives:      true,
		ResponseHeaderTimeout:  controlTimeout,
		MaxResponseHeaderBytes: 16 << 10,
	}
	return &Controller{address: address, instance: instance, token: controlToken, client: &http.Client{
		Transport: transport,
		Timeout:   controlTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}, nil
}

// Status verifies readiness and identity through the protected endpoint.
func (c *Controller) Status(ctx context.Context) (ManagementStatus, error) {
	return c.request(ctx, http.MethodGet, "/_jev/status")
}

// Stop requests cancellation only after the server authenticates this instance.
func (c *Controller) Stop(ctx context.Context) (ManagementStatus, error) {
	return c.request(ctx, http.MethodPost, "/_jev/stop")
}

func (c *Controller) request(ctx context.Context, method, path string) (ManagementStatus, error) {
	fail := func(cause error) (ManagementStatus, error) {
		return ManagementStatus{}, &controlError{cause}
	}
	r, err := http.NewRequestWithContext(ctx, method, "http://"+c.address+path, nil)
	if err != nil {
		return fail(err)
	}
	r.Header.Set("Authorization", "Bearer "+c.token)
	r.Header.Set(instanceHeader, c.instance)
	r.Header.Set("Accept", "application/json")
	response, err := c.client.Do(r)
	if err != nil {
		return fail(err)
	}
	defer response.Body.Close()
	contentType, _, typeErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode != http.StatusOK || typeErr != nil || contentType != "application/json" || len(response.Header.Values("Content-Type")) != 1 {
		return fail(errors.New("unexpected management response"))
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, controlResponseLimit+1))
	if err != nil {
		return fail(err)
	}
	if len(body) > controlResponseLimit {
		return fail(errors.New("management response exceeds limit"))
	}
	status, err := decodeManagementStatus(body)
	if err != nil {
		return fail(err)
	}
	if status.Instance != c.instance || (status.State != "prepared" && status.State != "running" && status.State != "stopping") || (method == http.MethodPost && status.State != "stopping") {
		return fail(errors.New("unexpected management identity or state"))
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	return status, nil
}

func decodeManagementStatus(body []byte) (ManagementStatus, error) {
	invalid := errors.New("invalid management response")
	decoder := json.NewDecoder(bytes.NewReader(body))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return ManagementStatus{}, invalid
	}
	fields := make(map[string]string, 2)
	for decoder.More() {
		token, err := decoder.Token()
		name, ok := token.(string)
		if err != nil || !ok || (name != "instance" && name != "state") {
			return ManagementStatus{}, invalid
		}
		if _, duplicate := fields[name]; duplicate {
			return ManagementStatus{}, invalid
		}
		var value string
		if err := decoder.Decode(&value); err != nil || value == "" {
			return ManagementStatus{}, invalid
		}
		fields[name] = value
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') || len(fields) != 2 {
		return ManagementStatus{}, invalid
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return ManagementStatus{}, invalid
	}
	return ManagementStatus{Instance: fields["instance"], State: fields["state"]}, nil
}
