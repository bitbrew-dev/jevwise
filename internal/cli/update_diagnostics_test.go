package cli

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bitbrew-dev/jevwise/internal/update"
)

func TestUpdateReportsRateLimitWithAndWithoutDebug(t *testing.T) {
	client := update.NewClient(&http.Client{Transport: skillTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Header: http.Header{"X-Ratelimit-Remaining": {"0"}}, Body: io.NopCloser(strings.NewReader("private-body"))}, nil
	})})
	for _, debug := range []bool{false, true} {
		root := updateRoot(client.Latest, "dev")
		cmd, _, _ := root.Find([]string{"update"})
		instrumentCommands(cmd) // The injected test command was added after NewRoot.
		args := []string{"update", "--check"}
		if debug {
			args = append(args, "--debug")
		}
		out, stderr, err := execute(root, args...)
		if err == nil || out != "" || !strings.Contains(err.Error(), "rate limited") || strings.Contains(stderr+err.Error(), "private") {
			t.Fatal("rate-limit failure unhelpful or unsafe")
		}
		if debug != strings.Contains(stderr, "update.lookup.failed.rate_limited") {
			t.Fatal("debug flag was not honored")
		}
	}
}
