package daemon

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func validateTestBootstrap(b Bootstrap) error {
	if b.AgentToken != "agent-secret" || b.Instance != "instance" {
		return errors.New("private validation details")
	}
	return nil
}

func testBootstrap() Bootstrap {
	return Bootstrap{Address: "127.0.0.1:8080", AgentToken: "agent-secret", Instance: "instance", ControlToken: "management-secret"}
}

func testFrame(payload string) []byte {
	frame := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(frame[:4], uint32(len(payload)))
	copy(frame[4:], payload)
	return frame
}

func TestBootstrapAcknowledgedLifetime(t *testing.T) {
	var data bytes.Buffer
	if err := WriteBootstrap(&data, testBootstrap(), validateTestBootstrap); err != nil {
		t.Fatal(err)
	}
	if err := WriteAcknowledgement(&data); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot, startup, err := ReadBootstrap(ctx, io.NopCloser(bytes.NewReader(data.Bytes())), time.Second, validateTestBootstrap)
	if err != nil || boot != testBootstrap() {
		t.Fatalf("bootstrap round trip failed: %v", err)
	}
	defer startup.Close()
	if err := startup.AwaitAcknowledgement(); err != nil {
		t.Fatal(err)
	}
	startup.mu.Lock()
	startup.deadline = time.Now().Add(-time.Second)
	startup.mu.Unlock()
	startup.expire()
	if startup.Context().Err() != nil {
		t.Fatal("acknowledged lifetime was cancelled")
	}
	cancel()
	<-startup.Context().Done()
}

func TestBootstrapRejectsMalformedFrames(t *testing.T) {
	for _, payload := range []string{
		`{"AgentToken":"agent-secret","Instance":"instance","unknown":"secret"}`,
		`{"AgentToken":"agent-secret","Instance":"instance","agenttoken":"secret"}`,
		`{"AgentToken":"agent-secret","Instance":"instance","Instance":"instance"}`,
		`{"AgentToken":"agent-secret","Instance":"instance","Config":{"Model":"x","Model":"y"}}`,
		`{"AgentToken":"agent-secret","Instance":"instance","Config":{"apikey":"secret"}}`,
		`{"AgentToken":"agent-secret","Instance":"instance"} {}`,
		`{"AgentToken":"wrong-secret","Instance":"instance"}`,
		`{"AgentToken":"agent-secret","Instance":"instance","Config":[]}`,
		"{\xff}", `null`, `[]`, `{`,
	} {
		t.Run(payload, func(t *testing.T) {
			_, startup, err := ReadBootstrap(context.Background(), io.NopCloser(bytes.NewReader(testFrame(payload))), time.Second, validateTestBootstrap)
			if err == nil || startup != nil || err.Error() != "invalid or expired background bootstrap" {
				t.Fatalf("unsafe acceptance or error: %v", err)
			}
		})
	}
	for _, frame := range [][]byte{{}, {0}, {0, 0, 0, 0}, {0, 1, 0, 1}, {0, 0, 0, 9, '{'}} {
		if _, _, err := ReadBootstrap(context.Background(), io.NopCloser(bytes.NewReader(frame)), time.Second, validateTestBootstrap); err == nil {
			t.Fatal("invalid frame accepted")
		}
	}
}

func TestBootstrapReadCancelledAndLeaseExpired(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		input, output := io.Pipe()
		ctx, cancel := context.WithCancel(context.Background())
		lease := 20 * time.Millisecond
		if cancelled {
			lease = time.Second
			cancel()
		}
		_, _, err := ReadBootstrap(ctx, input, lease, validateTestBootstrap)
		cancel()
		_ = output.Close()
		if err == nil {
			t.Fatal("blocked bootstrap did not expire")
		}
	}
}

func TestBootstrapUnacknowledgedAndExpired(t *testing.T) {
	for _, ack := range []byte{0, acknowledgement, 99} {
		var data bytes.Buffer
		_ = WriteBootstrap(&data, testBootstrap(), validateTestBootstrap)
		if ack != 0 {
			_ = data.WriteByte(ack)
		}
		_, startup, err := ReadBootstrap(context.Background(), io.NopCloser(bytes.NewReader(data.Bytes())), time.Second, validateTestBootstrap)
		if err != nil {
			t.Fatal(err)
		}
		if ack == acknowledgement {
			startup.mu.Lock()
			startup.deadline = time.Now().Add(-time.Second)
			startup.mu.Unlock()
		}
		if err := startup.AwaitAcknowledgement(); err == nil || startup.Context().Err() == nil {
			t.Fatal("unacknowledged startup remained live")
		}
	}
}

func TestBootstrapACKReadLeaseClosesPipe(t *testing.T) {
	input, output := io.Pipe()
	written := make(chan error, 1)
	go func() { written <- WriteBootstrap(output, testBootstrap(), validateTestBootstrap) }()
	_, startup, err := ReadBootstrap(context.Background(), input, 20*time.Millisecond, validateTestBootstrap)
	if err != nil {
		t.Fatal(err)
	}
	defer startup.Close()
	defer output.Close()
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	if err := startup.AwaitAcknowledgement(); err == nil || startup.Context().Err() == nil {
		t.Fatal("blocked ACK did not expire")
	}
}

type shortWriter struct{}

func (shortWriter) Write([]byte) (int, error) { return 0, nil }

func TestBootstrapWriteBoundsAndErrors(t *testing.T) {
	boot := testBootstrap()
	boot.Config.APIKey = strings.Repeat("secret", bootstrapLimit)
	if err := WriteBootstrap(io.Discard, boot, validateTestBootstrap); err == nil {
		t.Fatal("oversized bootstrap accepted")
	}
	boot = testBootstrap()
	boot.Config.APIKey = "\xff"
	if err := WriteBootstrap(io.Discard, boot, validateTestBootstrap); err == nil {
		t.Fatal("invalid UTF-8 normalized")
	}
	if err := WriteBootstrap(shortWriter{}, testBootstrap(), validateTestBootstrap); err == nil {
		t.Fatal("short write accepted")
	}
	if err := WriteAcknowledgement(shortWriter{}); err == nil {
		t.Fatal("short ACK write accepted")
	}
	if _, _, err := ReadBootstrap(nil, nil, 0, nil); err == nil {
		t.Fatal("invalid arguments accepted")
	}
}
