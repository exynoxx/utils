//go:build linux || darwin || freebsd || netbsd || openbsd

package holepunch

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// controlSocket sets SO_REUSEPORT on the raw socket before it is bound.
// SO_REUSEPORT allows a second socket to bind to a port that already has a
// listening socket, which is required for TCP hole-punch dialling from the
// same local port as the existing TCP listener.
func controlSocket(_ string, _ string, c syscall.RawConn) error {
	var sErr error
	if err := c.Control(func(fd uintptr) {
		sErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
	}); err != nil {
		return err
	}
	return sErr
}
