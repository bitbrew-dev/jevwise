package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/bitbrew-dev/jevwise/internal/config"
	"github.com/spf13/cobra"
)

func editorRoot(t *testing.T, run configEditor) *cobra.Command {
	root := configRoot(t)
	group, _, _ := root.Find([]string{"config"})
	edit, _, _ := root.Find([]string{"config", "edit"})
	group.RemoveCommand(edit)
	group.AddCommand(newConfigEdit(run))
	return root
}

func TestConfigEditSelectionStreamsAndRepair(t *testing.T) {
	for _, mode := range []string{"default", "local", "global", "explicit", "malformed", "override"} {
		t.Run(mode, func(t *testing.T) {
			configEnvironment(t)
			path, _ := filepath.Abs(config.LocalName)
			content, want := "", "vim"
			args := []string{"config", "edit"}
			if mode == "global" {
				path, _ = config.GlobalPath()
			}
			if mode == "explicit" {
				path = filepath.Join(t.TempDir(), "selected file.toml")
				args = append(args, "--config", path)
				_ = os.WriteFile(config.LocalName, []byte("editor='wrong-local'"), 0600)
			}
			if mode == "local" || mode == "global" || mode == "explicit" {
				content, want = "editor='configured editor'", "configured editor"
			}
			if mode == "malformed" {
				content = "api_key='private-secret'\n["
			}
			if mode == "override" {
				content, want = "editor=42", "override-editor"
				args = append(args, "--editor", want)
			}
			_ = os.MkdirAll(filepath.Dir(path), 0700)
			_ = os.WriteFile(path, []byte(content), 0600)
			calls := 0
			root := editorRoot(t, func(ctx context.Context, editor, selected string, in io.Reader, out, stderr io.Writer) error {
				calls++
				input, _ := io.ReadAll(in)
				if editor != want || selected != path || string(input) != "typed" || ctx.Err() != nil {
					t.Fatal("wrong editor/path/streams/context")
				}
				_, _ = io.WriteString(out, "editor-output")
				_, _ = io.WriteString(stderr, "editor-diagnostic")
				return nil
			})
			root.SetIn(strings.NewReader("typed"))
			out, stderr, err := execute(root, args...)
			if err != nil || calls != 1 || out != "editor-output" || stderr != "editor-diagnostic" {
				t.Fatal("editor not invoked correctly", err)
			}
		})
	}
}

func TestConfigEditInvalidRequestsHelpAndSafeFailure(t *testing.T) {
	configEnvironment(t)
	for _, args := range [][]string{{"config", "edit", "--help", "--config", "/missing"}, {"config", "edit"}, {"config", "edit", "private-secret"}, {"config", "edit", "--config="}} {
		out, _, err := execute(editorRoot(t, func(context.Context, string, string, io.Reader, io.Writer, io.Writer) error {
			t.Fatal("invalid/help started editor")
			return nil
		}), args...)
		if strings.Contains(strings.Join(args, " "), "--help") {
			if err != nil || !strings.Contains(out, "--editor") {
				t.Fatal("edit help failed", err)
			}
		} else if err == nil || out != "" || strings.Contains(err.Error(), "private-secret") {
			t.Fatal("invalid edit accepted or leaked", err)
		}
	}
	_ = os.WriteFile(config.LocalName, []byte("editor='vim'"), 0600)
	if _, _, err := execute(editorRoot(t, func(context.Context, string, string, io.Reader, io.Writer, io.Writer) error {
		return errors.New("private-secret")
	}), "config", "edit"); err == nil || strings.Contains(err.Error(), "private-secret") {
		t.Fatal("editor error leaked", err)
	}
	if runtime.GOOS != "windows" {
		_ = os.Symlink(config.LocalName, "linked.toml")
		if _, _, err := execute(editorRoot(t, func(context.Context, string, string, io.Reader, io.Writer, io.Writer) error {
			t.Fatal("linked editor target accepted")
			return nil
		}), "config", "edit", "--config", "linked.toml"); err == nil {
			t.Fatal("symlink accepted")
		}
	}
}

func TestConfigEditorProcessNoShellAndCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("installed editor behavior supported/tested on Unix")
	}
	directory := t.TempDir()
	editor := filepath.Join(directory, "vim")
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	_ = os.WriteFile(editor, []byte("#!/bin/sh\nprintf '%s\\n' \"$#\" \"$1\"\ncat\nprintf 'diagnostic' >&2\n"), 0700)
	path := filepath.Join(directory, "config ; not a command.toml")
	var out, stderr bytes.Buffer
	if err := runConfigEditor(context.Background(), "vim", path, strings.NewReader("typed"), &out, &stderr); err != nil || out.String() != "1\n"+path+"\ntyped" || stderr.String() != "diagnostic" {
		t.Fatal("process arguments/streams changed", err)
	}
	marker := filepath.Join(directory, "not-created")
	if err := runConfigEditor(context.Background(), "sh -c 'touch "+marker+"'", path, nil, io.Discard, io.Discard); err == nil {
		t.Fatal("shell fragment executed")
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("shell side effect")
	}
	_ = os.WriteFile(editor, []byte("#!/bin/sh\nexit 7\n"), 0700)
	if err := runConfigEditor(context.Background(), editor, path, nil, io.Discard, io.Discard); err == nil {
		t.Fatal("nonzero editor exit accepted")
	}
	_ = os.WriteFile(editor, []byte("#!/bin/sh\nexec sleep 30\n"), 0700)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := runConfigEditor(ctx, editor, path, nil, io.Discard, io.Discard); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("editor cancellation ignored", err)
	}
}

func TestConfigEditRejectsBadEditorAndCancelledRequests(t *testing.T) {
	configEnvironment(t)
	_ = os.WriteFile(config.LocalName, []byte("editor=42"), 0600)
	for _, args := range [][]string{{"config", "edit"}, {"config", "edit", "--editor="}, {"config", "edit", "--config", "."}} {
		if _, _, err := execute(editorRoot(t, func(context.Context, string, string, io.Reader, io.Writer, io.Writer) error {
			t.Fatal("invalid editor request launched")
			return nil
		}), args...); err == nil {
			t.Fatal("invalid editor accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root := editorRoot(t, func(context.Context, string, string, io.Reader, io.Writer, io.Writer) error {
		t.Fatal("cancelled edit launched")
		return nil
	})
	root.SetArgs([]string{"config", "edit", "--editor", "vim"})
	if err := root.ExecuteContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled edit accepted", err)
	}
}

func TestConfigEditorBoundsInheritedPipeWait(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix synthetic editor helper")
	}
	dir := t.TempDir()
	path, editor := filepath.Join(dir, "config.toml"), filepath.Join(dir, "helper")
	_ = os.WriteFile(editor, []byte("#!/bin/sh\n(sleep 1; printf done > \"$1.finished\") &\nexit 0\n"), 0700)
	var out, stderr bytes.Buffer
	if err := runConfigEditor(context.Background(), editor, path, nil, &out, &stderr); !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatal("inherited output pipe was not bounded", err)
	}
	// Wait for the owned, bounded helper to finish before deleting its files.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path + ".finished"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("synthetic helper did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
