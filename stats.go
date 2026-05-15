package main

import (
	"sync/atomic"
	"time"
)

type DownloadStats struct {
	startTime       time.Time
	totalWriteNanos int64
}

func NewDownloadStats() *DownloadStats {
	return &DownloadStats{startTime: time.Now()}
}

func (s *DownloadStats) ObserveWrite(d time.Duration) {
	if s == nil {
		return
	}
	atomic.AddInt64(&s.totalWriteNanos, d.Nanoseconds())
}

func (s *DownloadStats) WriteNanos() int64 {
	if s == nil {
		return 0
	}
	return atomic.LoadInt64(&s.totalWriteNanos)
}

func (s *DownloadStats) WritePercentApprox() float64 {
	if s == nil {
		return 0
	}
	elapsed := time.Since(s.startTime).Nanoseconds()
	if elapsed <= 0 {
		return 0
	}
	pct := float64(s.WriteNanos()) / float64(elapsed) * 100
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}
