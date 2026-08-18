//go:build linux

package server

import (
	"net"

	"golang.org/x/net/ipv4"
)

// newUDPBatchWriter returns a sendmmsg-based batcher
func newUDPBatchWriter(conn *net.UDPConn) func(addr *net.UDPAddr, packets [][]byte) {
	pc := ipv4.NewPacketConn(conn)
	return func(addr *net.UDPAddr, packets [][]byte) {
		if len(packets) == 0 {
			return
		}
		if len(packets) == 1 {
			_, _ = conn.WriteToUDPAddrPort(packets[0], addr.AddrPort())
			return
		}
		msgs := make([]ipv4.Message, len(packets))
		for i, p := range packets {
			msgs[i].Buffers = [][]byte{p}
			msgs[i].Addr = addr
		}
		n, err := pc.WriteBatch(msgs, 0)
		if err != nil || n < len(packets) {
			// Partial/failed batch: finish remaining with portable writes.
			for _, p := range packets[n:] {
				_, _ = conn.WriteToUDPAddrPort(p, addr.AddrPort())
			}
		}
	}
}
