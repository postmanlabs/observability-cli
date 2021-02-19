package pcap

import (
	"time"
)

type clockWrapper interface {
	Now() time.Time
}

type realClock struct{}

func (*realClock) Now() time.Time {
	return time.Now()
}

type fakeClock struct {
	currTime time.Time
}

func (f *fakeClock) Now() time.Time {
	return f.currTime
}
