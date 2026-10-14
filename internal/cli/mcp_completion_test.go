package cli

import (
	"context"
	"strings"
	"testing"
)

func TestMCPInstallScopeCompletionIsClientSpecificAndLazy(t *testing.T) {
	decisionEnvironment(t)
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"mcp", "install", "claude", "--scope", ""}, "user\nlocal\nproject\n:4\n"},
		{[]string{"mcp", "install", "codex", "--scope", ""}, "user\n:4\n"},
		{[]string{"mcp", "install", "--scope", ""}, "user\nlocal\nproject\n:4\n"},
		{[]string{"mcp", "install", "claude", "--scope", "l"}, "local\n:4\n"},
		{[]string{"mcp", "install", "claude", "--scope=pr"}, "project\n:4\n"},
		{[]string{"mcp", "install", "codex", "--scope=u"}, "user\n:4\n"},
		{[]string{"mcp", "install", "codex", "--scope", "l"}, ":4\n"},
		{[]string{"mcp", "install", "claude", "--scope", "private-prefix"}, ":4\n"},
	} {
		for _, hidden := range []string{"__complete", "__completeNoDesc"} {
			root := installerRoot(t, func(context.Context, string, ...string) ([]byte, error) {
				t.Fatal("completion invoked a client")
				return nil, nil
			})
			// Completion must remain available with unusable runtime settings.
			args := []string{hidden, "--debug", "--config", "/missing", "--timeout", "invalid", "--api-key", "private-key"}
			out, stderr, err := execute(root, append(args, tt.args...)...)
			if err != nil || out != tt.want || !strings.Contains(stderr, "ShellCompDirectiveNoFileComp") {
				t.Fatal("scope completion candidates or no-file directive changed", tt.args, err)
			}
			if strings.Contains(stderr, "mcp.install.register") || strings.Contains(out+stderr, "private") {
				t.Fatal("completion ran installation or exposed input")
			}
		}
	}
}
