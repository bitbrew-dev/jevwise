package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func skillInstalled(t *testing.T, project, target string, want []byte) {
	t.Helper()
	dir := filepath.Join(project, target, "skills", "typesafe-ai")
	data, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	entries, listErr := os.ReadDir(dir)
	if err != nil || listErr != nil || !bytes.Equal(data, want) || len(entries) != 1 {
		t.Fatalf("content or staging cleanup failed: %v/%v/%d", err, listErr, len(entries))
	}
}

func TestSkillInstallTargetsAndForce(t *testing.T) {
	for _, target := range []string{".agent", ".claude"} {
		project := t.TempDir()
		if err := installSkillAt(context.Background(), project, validSkill, target, false, skillInstallHooks{}); err != nil {
			t.Fatal(err)
		}
		replacement := append(bytes.Clone(validSkill), []byte("replacement\n")...)
		if err := installSkillAt(context.Background(), project, replacement, target, false, skillInstallHooks{}); !errors.Is(err, errSkillExists) {
			t.Fatal("existing skill overwritten without force")
		}
		skillInstalled(t, project, target, validSkill)
		if runtime.GOOS == "windows" {
			if err := installSkillAt(context.Background(), project, replacement, target, true, skillInstallHooks{}); err == nil {
				t.Fatal("Windows force accepted")
			}
			continue
		}
		path := filepath.Join(project, target, "skills", "typesafe-ai", "SKILL.md")
		old, _ := os.Stat(path)
		alias := filepath.Join(project, "alias")
		if err := os.Link(path, alias); err != nil {
			t.Fatal(err)
		}
		if err := installSkillAt(context.Background(), project, replacement, target, true, skillInstallHooks{}); err != nil {
			t.Fatal(err)
		}
		skillInstalled(t, project, target, replacement)
		current, _ := os.Stat(path)
		aliased, _ := os.ReadFile(alias)
		if os.SameFile(old, current) || !bytes.Equal(aliased, validSkill) {
			t.Fatal("force modified old hardlinked inode")
		}
	}
}

func TestSkillInstallRejectsUnsafePathsAndInputs(t *testing.T) {
	for _, path := range []string{".agent", ".agent/skills", ".agent/skills/typesafe-ai", ".agent/skills/typesafe-ai/SKILL.md"} {
		for _, link := range []bool{false, true} {
			project := t.TempDir()
			full := filepath.Join(project, filepath.FromSlash(path))
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			outside := t.TempDir()
			if link {
				if err := os.Symlink(outside, full); err != nil {
					t.Skip("symlinks unavailable")
				}
			} else if path == ".agent/skills/typesafe-ai/SKILL.md" {
				if err := os.Mkdir(full, 0o755); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(full, []byte("original"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := installSkillAt(context.Background(), project, validSkill, ".agent", runtime.GOOS != "windows", skillInstallHooks{}); err == nil {
				t.Fatal("unsafe path accepted", path)
			}
			entries, _ := os.ReadDir(outside)
			if len(entries) != 0 {
				t.Fatal("link referent changed")
			}
		}
	}
	project := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		ctx    context.Context
		data   []byte
		target string
	}{{ctx, validSkill, ".agent"}, {nil, validSkill, ".agent"}, {context.Background(), []byte("bad"), ".agent"}, {context.Background(), validSkill, "../outside"}} {
		if err := installSkillAt(tc.ctx, project, tc.data, tc.target, false, skillInstallHooks{}); err == nil {
			t.Fatal("invalid input accepted")
		}
	}
	entries, _ := os.ReadDir(project)
	if len(entries) != 0 {
		t.Fatal("invalid input modified project")
	}
}

func TestSkillInstallFailuresPreserveAndClean(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("force is unsupported")
	}
	boom := errors.New("injected failure")
	replacement := append(bytes.Clone(validSkill), []byte("replacement\n")...)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, tc := range []struct {
		name  string
		ctx   context.Context
		hooks skillInstallHooks
		want  error
	}{
		{"write", context.Background(), skillInstallHooks{write: func(f *os.File, data []byte) (int, error) { n, _ := f.Write(data[:3]); return n, boom }}, boom},
		{"short", context.Background(), skillInstallHooks{write: func(f *os.File, data []byte) (int, error) { return f.Write(data[:3]) }}, io.ErrShortWrite},
		{"sync", context.Background(), skillInstallHooks{sync: func(*os.File) error { return boom }}, boom},
		{"close", context.Background(), skillInstallHooks{close: func(*os.File) error { return boom }}, boom},
		{"publish", context.Background(), skillInstallHooks{publish: func(*os.Root, string, string, bool) error { return boom }}, boom},
		{"cancel", ctx, skillInstallHooks{write: func(f *os.File, data []byte) (int, error) { cancel(); return f.Write(data) }}, context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			project := t.TempDir()
			if err := installSkillAt(context.Background(), project, validSkill, ".agent", false, skillInstallHooks{}); err != nil {
				t.Fatal(err)
			}
			if err := installSkillAt(tc.ctx, project, replacement, ".agent", true, tc.hooks); !errors.Is(err, tc.want) {
				t.Fatalf("wrong failure: %v", err)
			}
			skillInstalled(t, project, ".agent", validSkill)
		})
	}
}

func TestSkillInstallConcurrentNoReplace(t *testing.T) {
	project := t.TempDir()
	ready, release, outcomes := make(chan struct{}, 2), make(chan struct{}), make(chan error, 2)
	hooks := skillInstallHooks{publish: func(root *os.Root, stage, path string, _ bool) error {
		ready <- struct{}{}
		<-release
		return root.Link(stage, path)
	}}
	for i := 0; i < 2; i++ {
		go func() { outcomes <- installSkillAt(context.Background(), project, validSkill, ".agent", false, hooks) }()
	}
	<-ready
	<-ready
	close(release)
	successes := 0
	for i := 0; i < 2; i++ {
		if err := <-outcomes; err == nil {
			successes++
		} else if !errors.Is(err, errSkillExists) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatal("no-force race did not choose exactly one winner")
	}
	skillInstalled(t, project, ".agent", validSkill)
}

func TestSkillProductionWiring(t *testing.T) {
	t.Chdir(t.TempDir())
	cmd := skillRoot(func(context.Context) ([]byte, error) { return validSkill, nil }, installSkill)
	if _, _, err := execute(cmd, "skill"); err != nil {
		t.Fatal(err)
	}
	skillInstalled(t, ".", ".agent", validSkill)
}
