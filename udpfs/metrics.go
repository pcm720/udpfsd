package udpfs

import (
	"sync"
	"time"
)

// Metrics is a point-in-time snapshot of a Connection's lifetime counters.
type Metrics struct {
	// Lifetime counts
	CommandCounts map[MsgType]int64 // Per-command counts
	ErrorCounts   map[MsgType]int64 // Per-command errors
	BytesTx       int64             // Sent to client
	BytesRx       int64             // Received from client

	// Throughput, bytes per second (average per active transfer periods)
	AvgTxThroughput float64
	AvgRxThroughput float64
}

type metricCollector struct {
	// Command and error counts using maps
	commandCounts map[MsgType]int64
	errorCounts   map[MsgType]int64

	// Byte counters
	bytesTx int64
	bytesRx int64

	// For moving average throughput calculation
	lastCheckTimeSecond  int64
	totalSecondsObserved uint64 // Number of active seconds observed
	currentSecondBytesRx uint64 // Bytes in current second
	currentSecondBytesTx uint64 // Bytes in current second

	// Running average values
	avgThroughputTx float64
	avgThroughputRx float64

	sync.Mutex
}

func newMetricCollector() *metricCollector {
	return &metricCollector{
		commandCounts: make(map[MsgType]int64),
		errorCounts:   make(map[MsgType]int64),
	}
}

func (c *metricCollector) logMetric(msg MsgType, statusCode int) {
	c.Lock()
	defer c.Unlock()

	// Count command and errors using maps
	c.commandCounts[msg]++
	if statusCode < 0 {
		c.errorCounts[msg]++
	}

	if statusCode > 0 {
		switch msg {
		case MsgReadReq, MsgBreadReq:
			c.updateThroughput(statusCode, false)
		case MsgWriteReq, MsgWriteData, MsgBwriteReq:
			c.updateThroughput(statusCode, true)
		}
	}
}

func (c *metricCollector) updateThroughput(byteCount int, isWrite bool) {
	curSecond := time.Now().Unix()

	if curSecond > c.lastCheckTimeSecond {
		// Update moving average with current second's rate
		c.totalSecondsObserved++
		if c.currentSecondBytesTx > 0 {
			c.avgThroughputTx += (float64(c.currentSecondBytesTx) - c.avgThroughputTx) / float64(c.totalSecondsObserved)
		}
		if c.currentSecondBytesRx > 0 {
			c.avgThroughputRx += (float64(c.currentSecondBytesRx) - c.avgThroughputRx) / float64(c.totalSecondsObserved)
		}

		c.currentSecondBytesTx = 0
		c.currentSecondBytesRx = 0
		c.lastCheckTimeSecond = curSecond
	}

	if !isWrite {
		c.bytesTx += int64(byteCount)
		c.currentSecondBytesTx += uint64(byteCount)
	} else {
		c.bytesRx += int64(byteCount)
		c.currentSecondBytesRx += uint64(byteCount)
	}
}

// snapshot returns a copy of the collected metrics.
func (c *metricCollector) snapshot() Metrics {
	c.Lock()
	defer c.Unlock()

	// Copy internal maps
	cmdCounts := make(map[MsgType]int64, len(c.commandCounts))
	errCounts := make(map[MsgType]int64, len(c.errorCounts))
	for k, v := range c.commandCounts {
		cmdCounts[k] = v
	}
	for k, v := range c.errorCounts {
		errCounts[k] = v
	}

	return Metrics{
		CommandCounts:   cmdCounts,
		ErrorCounts:     errCounts,
		BytesTx:         c.bytesTx,
		BytesRx:         c.bytesRx,
		AvgTxThroughput: c.avgThroughputTx,
		AvgRxThroughput: c.avgThroughputRx,
	}
}
