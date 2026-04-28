//go:build windows

package holepunch

import "syscall"

// controlSocket sets SO_REUSEADDR on the raw socket before it is bound.
// On Windows, SO_REUSEADDR permits binding a second socket to a port that
// already has a listener, which is required for TCP hole-punch dialling.
func controlSocket(_ string, _ string, c syscall.RawConn) error {
	var sErr error
	if err := c.Control(func(fd uintptr) {
		sErr = syscall.SetsockoptInt(syscall.Handle(fd),
			syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
	}); err != nil {
		return err
	}
	return sErr
}
