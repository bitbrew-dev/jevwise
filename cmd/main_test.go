package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunExitStatusAndStreams(t *testing.T) {
	for _, tt := range []struct {
		args []string
		code int
	}{{[]string{"--help"}, 0}, {[]string{"--unknown=private-secret"}, 1}} {
		var out, stderr bytes.Buffer
		code := run(context.Background(), tt.args, strings.NewReader(""), &out, &stderr)
		if code != tt.code {
			t.Fatalf("exit code = %d, want %d", code, tt.code)
		}
		if code == 0 && (out.Len() == 0 || stderr.Len() != 0) {
			t.Fatal("help streams incorrect")
		}
		if code != 0 && (out.Len() != 0 || stderr.Len() == 0 || strings.Contains(stderr.String(), "private-secret")) {
			t.Fatal("error streams incorrect or secret exposed")
		}
	}
}
