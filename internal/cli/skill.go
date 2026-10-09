package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bitbrew-dev/jevwise/internal/debuglog"
	"github.com/spf13/cobra"
)

const skillURL = "https://github.com/typesafe-ai/skills/blob/main/skills/typesafe-ai/SKILL.md"
const skillRawURL = "https://raw.githubusercontent.com/typesafe-ai/skills/main/skills/typesafe-ai/SKILL.md"
const maxSkillBytes = 1 << 20

type skillFetch func(context.Context) ([]byte, error)
type skillInstall func(context.Context, []byte, string, bool) error

var skillHeading = regexp.MustCompile(`(?m)^#{1,6}[ \t]+\S`)

func validateSkill(data []byte) error {
	invalid := errors.New("invalid Jev skill document")
	if len(data) == 0 || len(data) > maxSkillBytes || !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return invalid
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return invalid
	}
	parts := strings.SplitN(text[4:], "\n---\n", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || !skillHeading.MatchString(parts[1]) ||
		strings.Contains(strings.ToLower(text), "<html") || strings.Contains(strings.ToLower(text), "<!doctype html") {
		return invalid
	}
	return nil
}

// newSkillFetch uses only the fixed public URL. It neither inherits SDK headers
// nor follows redirects, and leaves the injected client's settings untouched.
func newSkillFetch(client *http.Client) skillFetch {
	if client == nil {
		client = &http.Client{}
	}
	return func(ctx context.Context) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, skillRawURL, nil)
		if err != nil {
			return nil, &decisionError{"cannot fetch Jev skill", err}
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		copy := *client
		copy.Jar = nil // The public document must not receive caller cookies.
		copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		response, err := copy.Do(req)
		if response != nil && response.Body != nil {
			defer response.Body.Close()
		}
		if err != nil {
			return nil, &decisionError{"cannot fetch Jev skill", err}
		}
		if response.StatusCode != http.StatusOK {
			return nil, errors.New("cannot fetch Jev skill: response must be HTTP 200")
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, maxSkillBytes+1))
		if err != nil {
			return nil, &decisionError{"cannot fetch Jev skill", err}
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := validateSkill(data); err != nil {
			return nil, err
		}
		return data, nil
	}
}

// newSkill defaults to local installation under .agent; only the installer
// handles filesystem paths. Online mode never fetches or installs a document.
func newSkill(fetch skillFetch, install skillInstall) *cobra.Command {
	var online, local, agent, claude, force bool
	jev := true
	cmd := &cobra.Command{Use: "skill", Short: "Install Jev's skill or print its upstream URL",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 0 {
				return errors.New("skill does not accept positional arguments")
			}
			return nil
		}, RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if err := ctx.Err(); err != nil {
				return err
			}
			if !jev || cmd.Flags().Changed("online") && !online || cmd.Flags().Changed("local") && !local || cmd.Flags().Changed("online") && cmd.Flags().Changed("local") {
				return errors.New("select Jev and at most one enabled skill mode")
			}
			if flag := cmd.Flags().Lookup("provider"); flag != nil && flag.Changed && flag.Value.String() != "jev" {
				return errors.New("skill supports only the Jev provider")
			}
			if cmd.Flags().Changed("agent") && !agent || cmd.Flags().Changed("claude") && !claude || agent && claude {
				return errors.New("select at most one enabled skill target")
			}
			if online {
				if cmd.Flags().Changed("agent") || cmd.Flags().Changed("claude") || cmd.Flags().Changed("force") {
					return errors.New("online skill mode does not accept target or force flags")
				}
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), skillURL); err != nil {
					return &decisionError{"cannot write Jev skill URL", err}
				}
				return nil
			}
			if install == nil || fetch == nil {
				return errors.New("local skill installation is unavailable")
			}
			timeout := "10s"
			if flag := cmd.Flags().Lookup("timeout"); flag != nil {
				timeout = flag.Value.String()
			}
			duration, err := time.ParseDuration(timeout)
			if err != nil || duration <= 0 {
				return errors.New("skill timeout must be a positive duration")
			}
			ctx, cancel := context.WithTimeout(ctx, duration)
			defer cancel()
			if err := ctx.Err(); err != nil {
				return err
			}
			debuglog.Event(ctx, "skill.fetch")
			data, err := fetch(ctx)
			if err != nil {
				return &decisionError{"cannot fetch Jev skill", err}
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := validateSkill(data); err != nil {
				return err
			}
			target := ".agent"
			if claude {
				target = ".claude"
			}
			if err := install(ctx, data, target, force); err != nil {
				if errors.Is(err, errSkillExists) {
					return &decisionError{errSkillExists.Error(), err}
				}
				return &decisionError{"cannot install Jev skill", err}
			}
			return ctx.Err()
		}}
	cmd.Flags().BoolVar(&online, "online", false, "Print the upstream GitHub URL without downloading")
	cmd.Flags().BoolVar(&local, "local", false, "Fetch and install the skill (default)")
	cmd.Flags().BoolVar(&agent, "agent", false, "Install under .agent/skills (default)")
	cmd.Flags().BoolVar(&claude, "claude", false, "Install under .claude/skills")
	cmd.Flags().BoolVar(&force, "force", false, "Replace an existing regular skill file")
	cmd.Flags().BoolVar(&jev, "jev", true, "Use the Jev skill")
	_ = cmd.Flags().MarkHidden("jev")
	return cmd
}
