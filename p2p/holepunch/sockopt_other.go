//go:build !linux && !darwin && !freebsd && !netbsd && !openbsd && !windows

package holepunch

import "syscall"

// controlSocket is a no-op on unsupported platforms.
// TCP hole-punch dialling from a specific local port may not work on these
// platforms without SO_REUSEPORT, but regular dialling will still be tried.
func controlSocket(_ string, _ string, _ syscall.RawConn) error { return nil }
