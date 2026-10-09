package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bitbrew-dev/jevwise/internal/config"
	"github.com/bitbrew-dev/jevwise/internal/service"
)

func TestDebugStreamsPrivacyAndContextIsolation(t *testing.T) {
	configEnvironment(t)
	original := context.Background()
	factory := func(config.Config) (service.DecisionService, func(), error) {
		return decideFunc(func(context.Context, service.Request) (service.Response, error) {
			return service.Response{Choice: "private-option"}, nil
		}), nil, nil
	}
	cmd := NewRootWithFactory(factory)
	cmd.SetContext(original)
	args := []string{"decide", "--prompt=private-prompt", "--option=private-option", "--option=other", "--api-key=private-key"}
	quiet, stderr, err := execute(cmd, args...)
	if err != nil || stderr != "" {
		t.Fatal("default logs changed command behavior", err)
	}
	verbose, stderr, err := execute(cmd, "decide", "--debug")
	if err != nil || quiet != verbose || !strings.Contains(stderr, `"operation":"config.load"`) || !strings.Contains(stderr, "decision.dispatch") {
		t.Fatal("debug output missing or stdout changed", err)
	}
	child, _, _ := cmd.Find([]string{"decide"})
	if strings.Contains(stderr, "private") || child.Context() != original || cmd.Context() != original {
		t.Fatal("diagnostics leaked input or context")
	}
	_, stderr, err = execute(cmd, "decide", "--debug=false")
	if err != nil || stderr != "" {
		t.Fatal("debug setting leaked across invocations", err)
	}
	for _, args := range [][]string{{"--help", "--debug"}, {"decide", "--help", "--debug"}, {"version"}} {
		if _, stderr, err := execute(NewRoot(), args...); err != nil || stderr != "" {
			t.Fatal("help/version unexpectedly logged", err)
		}
	}
	broken := NewRootWithFactory(func(config.Config) (service.DecisionService, func(), error) {
		return nil, nil, errors.New("private-service-error")
	})
	_, stderr, err = execute(broken, append(args, "--debug")...)
	if err == nil || strings.Contains(stderr, "private") || !strings.Contains(stderr, `"success":false`) {
		t.Fatal("failure logs leaked or lost the outcome")
	}
}
