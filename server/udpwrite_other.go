//go:build !linux

package server

import "net"

// newUDPBatchWriter is unsupported on non-Linux systems
func newUDPBatchWriter(conn *net.UDPConn) func(addr *net.UDPAddr, packets [][]byte) {
	return nil
}
