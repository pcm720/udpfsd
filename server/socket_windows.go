//go:build windows

package server

import "syscall"

func setSocketOptionFunc() func(network, addr string, c syscall.RawConn) error {
	return func(network, addr string, c syscall.RawConn) error {
		var err error
		c.Control(func(fd uintptr) {
			err = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
		})
		return err
	}
}
