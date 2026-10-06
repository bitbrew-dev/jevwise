//go:build linux

package daemon

import "os"

// On Linux the POSIX ACL mask matches the group mode bits. privateInfo's exact
// 0700/0600 requirement therefore masks named-user and group ACL grants too.
func noExtendedACL(file *os.File) bool { return file != nil }
