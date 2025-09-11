package slackbot

import (
	"sync/atomic"
	"time"
)

type Metrics struct {
	Requests       atomic.Int64
	Responses      atomic.Int64
	Errors         atomic.Int64
	TotalLatencyNs atomic.Int64
}

func (m *Metrics) RecordRequest() { m.Requests.Add(1) }
func (m *Metrics) RecordResponse(d time.Duration) {
	m.Responses.Add(1)
	m.TotalLatencyNs.Add(d.Nanoseconds())
}
func (m *Metrics) RecordError() { m.Errors.Add(1) }
