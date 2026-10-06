package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

var validSkill = []byte("---\nname: typesafe-ai\ndescription: decisions\n---\n# Jev 技能\nExact content.\n")

type skillTransport func(*http.Request) (*http.Response, error)

func (f skillTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type skillBody struct {
	io.Reader
	closed bool
}

func (b *skillBody) Close() error { b.closed = true; return nil }

func skillRoot(fetch skillFetch, install skillInstall) *cobra.Command {
	root := NewRoot()
	for _, command := range root.Commands() {
		if command.Name() == "skill" {
			root.RemoveCommand(command)
		}
	}
	root.AddCommand(newSkill(fetch, install))
	return root
}

func TestSkillFetchContractAndFailures(t *testing.T) {
	cases := []struct {
		name   string
		status int
		reader io.Reader
		good   bool
	}{
		{"valid", 200, bytes.NewReader(validSkill), true},
		{"CRLF", 200, strings.NewReader(strings.ReplaceAll(string(validSkill), "\n", "\r\n")), true},
		{"status", 503, bytes.NewReader(validSkill), false},
		{"created", 201, bytes.NewReader(validSkill), false},
		{"empty YAML", 200, strings.NewReader("---\n---\n# Heading\n"), false},
		{"blank YAML", 200, strings.NewReader("---\n \t\n---\n# Heading\n"), false},
		{"malformed close", 200, strings.NewReader("---\nname: skill\n--\n# Heading\n"), false},
		{"read", 200, secretIO{}, false},
		{"large", 200, strings.NewReader(strings.Repeat("x", maxSkillBytes+1)), false},
		{"empty", 200, strings.NewReader(""), false},
		{"HTML", 200, strings.NewReader("<!doctype html><html>private-content</html>"), false},
		{"NUL", 200, bytes.NewReader(append(bytes.Clone(validSkill), 0)), false},
		{"UTF8", 200, bytes.NewReader(append(bytes.Clone(validSkill), 0xff)), false},
		{"frontmatter", 200, strings.NewReader("# Heading\nNo YAML"), false},
		{"heading", 200, strings.NewReader("---\nname: skill\n---\nNo heading"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := &skillBody{Reader: tc.reader}
			jar, _ := cookiejar.New(nil)
			upstream, _ := url.Parse(skillRawURL)
			jar.SetCookies(upstream, []*http.Cookie{{Name: "session", Value: "private-content"}})
			client := &http.Client{Timeout: time.Minute, Jar: jar, Transport: skillTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method != "GET" || r.URL.String() != skillRawURL || r.Header.Get("Authorization") != "" || r.Header.Get("X-TypeSafe-SDK") != "" || r.Header.Get("Cookie") != "" {
					t.Error("unsafe skill request")
				}
				return &http.Response{StatusCode: tc.status, Body: body}, nil
			})}
			data, err := newSkillFetch(client)(context.Background())
			if (err == nil) != tc.good || !body.closed || client.Timeout != time.Minute || client.CheckRedirect != nil || client.Jar != jar {
				t.Fatal("fetch result/ownership invalid", err)
			}
			if tc.good && (tc.name == "valid" && !bytes.Equal(data, validSkill) || tc.name == "CRLF" && !bytes.Contains(data, []byte("\r\n"))) {
				t.Fatal("document bytes changed")
			}
			if err != nil && strings.Contains(err.Error(), "private-content") {
				t.Fatal("fetch failure leaked", err)
			}
		})
	}
	calls := 0
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { t.Fatal("caller redirect policy used"); return nil }, Transport: skillTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://elsewhere.example"}}, Body: io.NopCloser(strings.NewReader("redirect"))}, nil
	})}
	if _, err := newSkillFetch(client)(context.Background()); err == nil || calls != 1 {
		t.Fatal("redirect followed", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := newSkillFetch(client)(ctx); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal("cancelled fetch dispatched", err)
	}
	cause := errors.New("private-content")
	client.Transport = skillTransport(func(*http.Request) (*http.Response, error) { return nil, cause })
	if _, err := newSkillFetch(client)(context.Background()); !errors.Is(err, cause) || strings.Contains(err.Error(), "private-content") {
		t.Fatal("network error not safe", err)
	}
}

func TestSkillModesHelpAndLocalCallbacks(t *testing.T) {
	fetch := func(ctx context.Context) ([]byte, error) {
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 10*time.Second {
			t.Error("missing local timeout")
		}
		return bytes.Clone(validSkill), nil
	}
	installed := 0
	install := func(_ context.Context, data []byte, _ string, _ bool) error {
		installed++
		if !bytes.Equal(data, validSkill) {
			t.Error("installed bytes changed")
		}
		return nil
	}
	for _, args := range [][]string{{"skill"}, {"skill", "--local"}, {"skill", "--jev"}} {
		if out, stderr, err := execute(skillRoot(fetch, install), args...); err != nil || out != "" || stderr != "" {
			t.Fatal("local mode failed", err)
		}
	}
	if installed != 3 {
		t.Fatal("default is not local")
	}
	out, _, err := execute(skillRoot(nil, nil), "skill", "--online", "--config", "/missing", "--timeout", "invalid")
	if err != nil || out != skillURL+"\n" {
		t.Fatal("online mode used IO/config", err)
	}
	out, _, err = execute(skillRoot(nil, nil), "skill", "--help")
	if err != nil || strings.Contains(out, "--jev") || !strings.Contains(out, "--online") {
		t.Fatal("hidden Jev/help invalid", err)
	}
	for _, args := range [][]string{{"skill", "private-content"}, {"skill", "--online", "--local"}, {"skill", "--online=false"}, {"skill", "--local=false"}, {"skill", "--jev=false"}, {"skill", "--online", "--provider", "claude"}, {"skill", "--timeout", "0s"}} {
		if out, _, err := execute(skillRoot(func(context.Context) ([]byte, error) { t.Fatal("invalid mode fetched"); return nil, nil }, func(context.Context, []byte, string, bool) error { t.Fatal("invalid mode installed"); return nil }), args...); err == nil || out != "" || strings.Contains(err.Error(), "private-content") {
			t.Fatal("invalid mode accepted or leaked", err)
		}
	}
	called := false
	if _, _, err := execute(skillRoot(func(context.Context) ([]byte, error) { called = true; return validSkill, nil }, nil), "skill"); err == nil || called {
		t.Fatal("nil installer fetched", err)
	}
}

func TestSkillSafeCallbacksAndCancellation(t *testing.T) {
	cause := errors.New("private-content")
	for _, failFetch := range []bool{true, false} {
		fetch := func(context.Context) ([]byte, error) {
			if failFetch {
				return nil, cause
			}
			return validSkill, nil
		}
		install := func(context.Context, []byte, string, bool) error { return cause }
		if _, _, err := execute(skillRoot(fetch, install), "skill"); !errors.Is(err, cause) || strings.Contains(err.Error(), "private-content") {
			t.Fatal("callback failure unsafe", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := skillRoot(func(context.Context) ([]byte, error) { t.Fatal("cancelled command fetched"); return nil, nil }, nil)
	cancel()
	cmd.SetArgs([]string{"skill"})
	if err := cmd.ExecuteContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("pre-cancel lost", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	cmd = skillRoot(func(context.Context) ([]byte, error) { cancel(); return validSkill, nil }, func(context.Context, []byte, string, bool) error { t.Fatal("cancelled fetch installed"); return nil })
	cmd.SetArgs([]string{"skill"})
	if err := cmd.ExecuteContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("fetch cancellation lost", err)
	}
	cmd = skillRoot(nil, nil)
	cmd.SetArgs([]string{"skill", "--online"})
	cmd.SetOut(secretIO{})
	if err := cmd.Execute(); err == nil || strings.Contains(err.Error(), "private-content") {
		t.Fatal("online writer unsafe", err)
	}
}

func TestSkillInjectedCancellationAndInvalidContent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	body := &skillBody{Reader: cancelInput{cancel}}
	client := &http.Client{Transport: skillTransport(func(*http.Request) (*http.Response, error) { return &http.Response{StatusCode: 200, Body: body}, nil })}
	if _, err := newSkillFetch(client)(ctx); !errors.Is(err, context.Canceled) || !body.closed {
		t.Fatal("body cancellation lost", err)
	}
	if _, err := newSkillFetch(nil)(nil); err == nil {
		t.Fatal("nil context accepted")
	}
	install := func(context.Context, []byte, string, bool) error { t.Fatal("invalid/nil fetch installed"); return nil }
	for _, fetch := range []skillFetch{nil, func(context.Context) ([]byte, error) { return []byte("invalid"), nil }} {
		if _, _, err := execute(skillRoot(fetch, install), "skill"); err == nil {
			t.Fatal("invalid injected fetch accepted")
		}
	}
}

func TestSkillPostInstallCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := skillRoot(func(context.Context) ([]byte, error) { return validSkill, nil }, func(context.Context, []byte, string, bool) error { cancel(); return nil })
	cmd.SetArgs([]string{"skill"})
	if err := cmd.ExecuteContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("post-install cancellation lost", err)
	}
}

func TestSkillTargetFlagsAndRegistration(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		target string
		force  bool
	}{
		{nil, ".agent", false}, {[]string{"--agent"}, ".agent", false}, {[]string{"--claude"}, ".claude", false},
		{[]string{"--claude", "--force"}, ".claude", true}, {[]string{"--force=false"}, ".agent", false},
	} {
		called := false
		cmd := skillRoot(func(context.Context) ([]byte, error) { return validSkill, nil }, func(_ context.Context, _ []byte, target string, force bool) error {
			called = true
			if target != tc.target || force != tc.force {
				t.Error("target/force mapping changed")
			}
			return nil
		})
		if _, _, err := execute(cmd, append([]string{"skill"}, tc.args...)...); err != nil || !called {
			t.Fatal("local target failed", err)
		}
	}
	for _, args := range [][]string{
		{"--agent=false"}, {"--claude=false"}, {"--agent", "--claude"},
		{"--online", "--agent"}, {"--online", "--claude"}, {"--online", "--force"}, {"--online", "--force=false"},
	} {
		cmd := skillRoot(func(context.Context) ([]byte, error) { t.Fatal("invalid target fetched"); return nil, nil }, func(context.Context, []byte, string, bool) error { t.Fatal("invalid target installed"); return nil })
		if out, _, err := execute(cmd, append([]string{"skill"}, args...)...); err == nil || out != "" {
			t.Fatal("invalid target had effects", err)
		}
	}
	out, _, err := execute(NewRoot(), "skill", "--online", "--config", "/missing", "--api-key", "", "--timeout", "invalid")
	if err != nil || out != skillURL+"\n" {
		t.Fatal("registered online skill loaded config or credentials", err)
	}
}

func TestSkillExistingFileForceHint(t *testing.T) {
	cmd := skillRoot(func(context.Context) ([]byte, error) { return validSkill, nil }, func(context.Context, []byte, string, bool) error {
		return &decisionError{"private-content", errSkillExists}
	})
	out, _, err := execute(cmd, "skill")
	if out != "" || !errors.Is(err, errSkillExists) || !strings.Contains(err.Error(), "--force") || strings.Contains(err.Error(), "private-content") {
		t.Fatal("existing file hint/cause unsafe", err)
	}
}
