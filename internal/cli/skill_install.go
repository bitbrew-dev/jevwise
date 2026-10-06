package cli

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"runtime"
)

var errSkillExists = errors.New("skill already exists; use --force to replace it")

type skillInstallHooks struct {
	write   func(*os.File, []byte) (int, error)
	sync    func(*os.File) error
	close   func(*os.File) error
	publish func(*os.Root, string, string, bool) error
}

func installSkill(ctx context.Context, data []byte, target string, force bool) error {
	err := installSkillAt(ctx, ".", data, target, force, skillInstallHooks{})
	if err != nil {
		return &decisionError{"cannot install Jev skill", err}
	}
	return nil
}

// installSkillAt confines operations to one project root. Link publishes without
// replacement; Unix rename replaces the directory entry, never its old inode.
// Directory and final-link checks are static checks, not hostile-writer CAS.
func installSkillAt(ctx context.Context, project string, data []byte, target string, force bool, hooks skillInstallHooks) (err error) {
	if ctx == nil {
		return errors.New("skill context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateSkill(data); err != nil {
		return err
	}
	if target != ".agent" && target != ".claude" {
		return errors.New("skill target must be .agent or .claude")
	}
	switch runtime.GOOS {
	case "js", "wasip1", "plan9":
		return errors.New("skill installation is unsupported on this platform")
	case "windows":
		if force {
			return errors.New("forced skill replacement is unsupported on Windows")
		}
	}
	root, err := os.OpenRoot(project)
	if err != nil {
		return err
	}
	defer root.Close()
	dir := target + "/skills/typesafe-ai"
	for _, component := range []string{target, target + "/skills", dir} {
		if err := root.Mkdir(component, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		info, err := root.Lstat(component)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return errors.New("skill parent must be a directory, not a link")
		}
	}
	path := dir + "/SKILL.md"
	if err := checkSkillTarget(root, path, force); err != nil {
		return err
	}
	stage := dir + "/.skill-" + rand.Text() + ".tmp"
	file, err := root.OpenFile(stage, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	published := false
	defer func() {
		_ = file.Close() // Also closes handles left open by a failed injected close.
		if cleanup := root.Remove(stage); cleanup != nil && !errors.Is(cleanup, os.ErrNotExist) {
			message := "cannot clean skill staging file"
			if published {
				message = "skill installed but staging cleanup failed"
			}
			err = errors.Join(err, &decisionError{message, cleanup})
		}
	}()
	if hooks.write == nil {
		hooks.write = (*os.File).Write
	}
	if hooks.sync == nil {
		hooks.sync = (*os.File).Sync
	}
	if hooks.close == nil {
		hooks.close = (*os.File).Close
	}
	if hooks.publish == nil {
		hooks.publish = func(root *os.Root, stage, path string, force bool) error {
			if force {
				return root.Rename(stage, path)
			}
			return root.Link(stage, path)
		}
	}
	if n, err := hooks.write(file, data); err != nil {
		return err
	} else if n != len(data) {
		return io.ErrShortWrite
	}
	if err := hooks.sync(file); err != nil {
		return err
	}
	if err := hooks.close(file); err != nil {
		return err
	}
	if err := checkSkillTarget(root, path, force); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := hooks.publish(root, stage, path, force); err != nil {
		if !force && errors.Is(err, os.ErrExist) {
			return errSkillExists
		}
		return err
	}
	published = true
	return nil
}

func checkSkillTarget(root *os.Root, path string, force bool) error {
	info, err := root.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("skill destination must be a regular file, not a link")
	}
	if !force {
		return errSkillExists
	}
	return nil
}
