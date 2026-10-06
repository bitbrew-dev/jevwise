//go:build darwin

package daemon

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func addACL(t *testing.T, path, permissions string) {
	t.Helper()
	// Only task-owned temporary fixtures are changed. Production never invokes
	// chmod or modifies existing ACLs.
	command := exec.Command("/bin/chmod", "+a", "everyone allow "+permissions, path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("cannot create ACL fixture: %v: %s", err, output)
	}
}

func openACLFixture(t *testing.T, path string) *os.File {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}

func TestNoExtendedACLRejectsGrantsDespitePrivateModes(t *testing.T) {
	for _, directory := range []bool{false, true} {
		name := "file"
		if directory {
			name = "directory"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			mode := os.FileMode(0o600)
			if directory {
				mode = 0o700
				if err := os.Mkdir(path, mode); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte("fixture"), mode); err != nil {
				t.Fatal(err)
			}
			file := openACLFixture(t, path)
			if !noExtendedACL(file) {
				t.Fatal("ACL-free private fixture rejected")
			}
			addACL(t, path, "read,readattr,readextattr,readsecurity")
			if err := os.Chmod(path, mode); err != nil {
				t.Fatal(err)
			}
			before, err := file.Stat()
			if err != nil || before.Mode().Perm() != mode {
				t.Fatal("fixture lost private mode", err)
			}
			if noExtendedACL(file) {
				t.Fatal("independent ACL grant accepted despite private mode")
			}
			after, err := file.Stat()
			if err != nil || !os.SameFile(before, after) || after.Mode() != before.Mode() {
				t.Fatal("ACL validation changed fixture", err)
			}
		})
	}
}

func TestNoExtendedACLRejectsInheritedACL(t *testing.T) {
	parent := t.TempDir()
	addACL(t, parent, "read,readattr,readextattr,readsecurity,file_inherit,directory_inherit")
	directory := filepath.Join(parent, "runtime")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "control.key")
	if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{directory, path} {
		if noExtendedACL(openACLFixture(t, path)) {
			t.Fatal("inherited ACL accepted")
		}
	}
}

func TestNoExtendedACLUsesPinnedHandle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	file := openACLFixture(t, path)
	if err := os.Rename(path, path+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	addACL(t, path, "read,readattr,readextattr,readsecurity")
	if !noExtendedACL(file) || noExtendedACL(openACLFixture(t, path)) {
		t.Fatal("ACL check followed exchanged path instead of original handle")
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if noExtendedACL(file) || noExtendedACL(nil) {
		t.Fatal("unavailable handle accepted")
	}
}

func TestEmptySecurityReferenceFailsClosed(t *testing.T) {
	valid := [12]byte{}
	binary.LittleEndian.PutUint32(valid[0:4], 12)
	binary.LittleEndian.PutUint32(valid[4:8], 8)
	if !emptySecurityReference(valid) {
		t.Fatal("valid absence reference rejected")
	}
	for _, offset := range []int{0, 4, 8} {
		invalid := valid
		invalid[offset]++
		if emptySecurityReference(invalid) {
			t.Fatal("malformed or nonempty security reference accepted", offset)
		}
	}
	if emptySecurityReference([12]byte{}) {
		t.Fatal("uninitialized output accepted")
	}
}
