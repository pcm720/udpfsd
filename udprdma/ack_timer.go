package udprdma

type ackWaitMode uint8

const (
	ackWaitNone ackWaitMode = iota
	ackWaitWindow
	ackWaitFin
)

// armAckTimer arms the ACK timeout with FIN or window interval.
// Assumes the lock is acquired
func (s *Session) armAckTimer() {
	d := WindowAckTimeout
	mode := ackWaitWindow
	if s.finPending {
		d = FinAckTimeout
		mode = ackWaitFin
	}
	// Avoid Stop/Reset when already waiting in the same mode.
	if s.ackWaitMode == mode {
		return
	}

	s.ackTimer.Stop()
	s.ackTimerSeq++
	s.ackTimerExpectSeq = s.ackTimerSeq
	s.ackWaitMode = mode
	s.ackTimer.Reset(d)
}

// stopAckTimer invalidates and stops the ACK timeout.
// Assumes the lock is acquired
func (s *Session) stopAckTimer() {
	s.ackTimerSeq++
	s.ackWaitMode = ackWaitNone
	if s.ackTimer != nil {
		s.ackTimer.Stop()
	}
}

// updateAckTimer arms or stops the ACK timer based on wait state.
// Assumes the lock is acquired
func (s *Session) updateAckTimer() {
	if s.InFlight() == 0 {
		s.stopAckTimer()
		return
	}
	if s.finPending || (s.transfer.data != nil && s.InFlight() >= SendWindow) {
		s.armAckTimer()
		return
	}
	s.stopAckTimer()
}

// Timer handler
func (s *Session) ackTimerFunc() {
	s.Lock()
	defer s.Unlock()
	if s.closed || s.ackWaitMode == ackWaitNone || s.ackTimerExpectSeq != s.ackTimerSeq {
		return
	}
	s.ackWaitMode = ackWaitNone
	s.handleAckTimeout()
}
