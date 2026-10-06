// Package daemon owns independent background process startup and runtime state.
package daemon

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/bitbrew-dev/jevwise/internal/config"
)

const bootstrapLimit = 64 << 10
const acknowledgement = 1

// Bootstrap travels only over anonymous stdin, never argv, state or logs.
// The caller must validate configuration, endpoints and protected runtime paths.
type Bootstrap struct {
	Config       config.Config
	Address      string
	AgentToken   string
	RuntimeDir   string
	Instance     string
	ControlToken string
}

// Startup owns stdin and a lifetime that survives successful acknowledgement.
// Input.Close must unblock reads, as it does for an os.File anonymous pipe.
type Startup struct {
	ctx      context.Context
	cancel   context.CancelFunc
	input    io.ReadCloser
	mu       sync.Mutex
	promoted bool
	closed   bool
	deadline time.Time
	timer    *time.Timer
	stop     func() bool
}

func (s *Startup) Context() context.Context { return s.ctx }

// Close cancels the lifetime and releases startup resources, also after ACK.
func (s *Startup) Close() {
	s.mu.Lock()
	s.closed = true
	if s.timer != nil {
		s.timer.Stop()
	}
	if s.stop != nil {
		s.stop()
	}
	s.cancel()
	s.mu.Unlock()
	_ = s.input.Close()
}

// Expiration and ACK promotion share the gate: a delayed timer cannot cancel
// a promoted lifetime, and a delayed ACK cannot extend an expired lease.
func (s *Startup) expire() {
	s.mu.Lock()
	if s.promoted || s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	_ = s.input.Close()
}

// ReadBootstrap starts the lease before reading any input. Errors are fixed and
// never wrap parser, validator or pipe errors, which could contain credentials.
func ReadBootstrap(ctx context.Context, input io.ReadCloser, lease time.Duration, validate func(Bootstrap) error) (Bootstrap, *Startup, error) {
	if ctx == nil || input == nil || lease <= 0 || validate == nil {
		return Bootstrap{}, nil, errors.New("invalid background startup parameters")
	}
	life, cancel := context.WithCancel(ctx)
	s := &Startup{ctx: life, cancel: cancel, input: input, deadline: time.Now().Add(lease)}
	s.mu.Lock()
	s.timer = time.AfterFunc(lease, s.expire)
	s.stop = context.AfterFunc(ctx, s.Close)
	s.mu.Unlock()
	fail := func() (Bootstrap, *Startup, error) {
		s.Close()
		return Bootstrap{}, nil, errors.New("invalid or expired background bootstrap")
	}
	var header [4]byte
	if _, err := io.ReadFull(input, header[:]); err != nil {
		return fail()
	}
	size := binary.BigEndian.Uint32(header[:])
	if size == 0 || size > bootstrapLimit {
		return fail()
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(input, payload); err != nil || !utf8.Valid(payload) {
		return fail()
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := uniqueJSON(decoder); err != nil {
		return fail()
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fail()
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(payload, &fields) != nil || !knownFields(fields, "Config", "Address", "AgentToken", "RuntimeDir", "Instance", "ControlToken") {
		return fail()
	}
	if raw, ok := fields["Config"]; ok {
		fields = nil
		if json.Unmarshal(raw, &fields) != nil || !knownFields(fields, "APIKey", "BaseURL", "Model", "Provider", "Timeout") {
			return fail()
		}
	}
	decoder = json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var boot Bootstrap
	if err := decoder.Decode(&boot); err != nil || validate(boot) != nil || life.Err() != nil || !time.Now().Before(s.deadline) {
		return fail()
	}
	return boot, s, nil
}

// AwaitAcknowledgement must run after the authenticated prepared listener is
// available. EOF, malformed ACK and lease expiry cancel the prepared lifetime.
func (s *Startup) AwaitAcknowledgement() error {
	var ack [1]byte
	if _, err := io.ReadFull(s.input, ack[:]); err != nil || ack[0] != acknowledgement {
		s.Close()
		return errors.New("background startup was not acknowledged")
	}
	s.mu.Lock()
	if s.closed || s.promoted || s.ctx.Err() != nil || !time.Now().Before(s.deadline) {
		s.mu.Unlock()
		s.Close()
		return errors.New("background startup was not acknowledged")
	}
	s.promoted = true
	s.timer.Stop()
	s.mu.Unlock()
	_ = s.input.Close()
	return nil
}

func WriteBootstrap(w io.Writer, boot Bootstrap, validate func(Bootstrap) error) error {
	if w == nil || validate == nil || validate(boot) != nil {
		return errors.New("invalid background bootstrap")
	}
	for _, value := range []string{boot.Address, boot.AgentToken, boot.RuntimeDir, boot.Instance, boot.ControlToken, boot.Config.APIKey, boot.Config.BaseURL, boot.Config.Model, boot.Config.Provider} {
		if !utf8.ValidString(value) {
			return errors.New("invalid background bootstrap")
		}
	}
	payload, err := json.Marshal(boot)
	if err != nil || len(payload) > bootstrapLimit {
		return errors.New("invalid background bootstrap")
	}
	frame := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(frame[:4], uint32(len(payload)))
	copy(frame[4:], payload)
	if n, err := w.Write(frame); err != nil || n != len(frame) {
		return errors.New("cannot transfer background bootstrap")
	}
	return nil
}

func WriteAcknowledgement(w io.Writer) error {
	if w == nil {
		return errors.New("cannot acknowledge background startup")
	}
	if n, err := w.Write([]byte{acknowledgement}); err != nil || n != 1 {
		return errors.New("cannot acknowledge background startup")
	}
	return nil
}

// uniqueJSON rejects duplicate keys at every depth, before typed decoding.
func uniqueJSON(d *json.Decoder) error {
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	seen := make(map[string]bool)
	for d.More() {
		if delim == '{' {
			key, err := d.Token()
			name, ok := key.(string)
			if err != nil || !ok || seen[name] {
				return errors.New("invalid object keys")
			}
			seen[name] = true
		}
		if err := uniqueJSON(d); err != nil {
			return err
		}
	}
	_, err = d.Token()
	return err
}

func knownFields(fields map[string]json.RawMessage, names ...string) bool {
	allowed := make(map[string]bool)
	for _, name := range names {
		allowed[name] = true
	}
	for name := range fields {
		if !allowed[name] {
			return false
		}
	}
	return true
}
