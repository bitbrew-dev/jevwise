package debuglog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestTraceIsOptInPrivateAndConcurrent(t *testing.T) {
	var output bytes.Buffer
	ctx := WithWriter(context.Background(), &output)
	finish := Trace(ctx, "fixture.operation")
	finish(errors.New("private-key-and-error"))
	var group sync.WaitGroup
	for i := 0; i < 16; i++ {
		group.Go(func() { Count(ctx, "fixture.status", 503) })
	}
	group.Wait()
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 18 || strings.Contains(output.String(), "private") {
		t.Fatal("concurrent diagnostics were lost or leaked")
	}
	for _, line := range lines {
		var fields map[string]any
		if json.Unmarshal([]byte(line), &fields) != nil || fields["level"] != "debug" {
			t.Fatal("invalid structured event")
		}
		if fields["message"] == "finished" && fields["success"] != false {
			t.Fatal("failure outcome lost")
		}
	}
	before := output.Len()
	quiet := WithWriter(ctx, nil)
	Event(quiet, "disabled")
	Trace(quiet, "disabled")(nil)
	Event(nil, "disabled")
	Trace(nil, "disabled")(nil)
	if output.Len() != before {
		t.Fatal("disabled context inherited output")
	}
}

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, errors.New("private-writer-error") }

func TestCancellationAndWriterFailure(t *testing.T) {
	var output bytes.Buffer
	Trace(WithWriter(context.Background(), &output), "canceled")(context.DeadlineExceeded)
	if !strings.Contains(output.String(), `"canceled":true`) {
		t.Fatal("deadline outcome lost")
	}
	Trace(WithWriter(context.Background(), failedWriter{}), "fixture")(nil)
}
