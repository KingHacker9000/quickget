package progress

import (
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"time"
)

const (
	RefreshIntervalMS    = 200
	MinRefreshIntervalMS = 50
)

type DownloadStats struct {
	startTime       time.Time
	totalWriteNanos int64
}

type Snapshot struct {
	Downloaded   int64
	Total        int64
	SpeedMBps    float64
	Percent      float64
	Elapsed      time.Duration
	WritePercent float64
}

type Reporter func(s Snapshot)

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

func sanitizeIntervalMS(intervalMs int) int {
	if intervalMs <= 0 {
		return RefreshIntervalMS
	}
	if intervalMs < MinRefreshIntervalMS {
		return MinRefreshIntervalMS
	}
	return intervalMs
}

func StartProgressLoop(w io.Writer, downloaded *int64, totalSize int64, reporter Reporter, writeStats *DownloadStats, intervalMs int) (chan struct{}, chan struct{}) {
	done := make(chan struct{})
	finished := make(chan struct{})
	start := time.Now()
	intervalMs = sanitizeIntervalMS(intervalMs)
	go func() {
		defer close(finished)
		ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				current := atomic.LoadInt64(downloaded)
				renderProgress(w, current, totalSize, start)
				reportSnapshot(reporter, current, totalSize, start, writeStats)
			case <-done:
				current := atomic.LoadInt64(downloaded)
				renderProgress(w, current, totalSize, start)
				reportSnapshot(reporter, current, totalSize, start, writeStats)
				fmt.Fprintln(w)
				return
			}
		}
	}()
	return done, finished
}

func StartVerboseProgressLoop(w io.Writer, downloaded *int64, totalSize int64, chunkTotals []int64, chunkDownloaded []int64, reporter Reporter, writeStats *DownloadStats, intervalMs int) (chan struct{}, chan struct{}) {
	done := make(chan struct{})
	finished := make(chan struct{})
	start := time.Now()
	intervalMs = sanitizeIntervalMS(intervalMs)

	go func() {
		defer close(finished)
		ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
		defer ticker.Stop()
		lines := len(chunkTotals) + 1
		firstRender := true

		render := func(final bool) {
			if !firstRender {
				fmt.Fprintf(w, "\033[%dA", lines)
			}

			currentTotal := atomic.LoadInt64(downloaded)
			elapsed := time.Since(start).Seconds()
			if elapsed <= 0 {
				elapsed = 1
			}
			totalPercent := float64(currentTotal) / float64(totalSize) * 100
			if totalPercent > 100 {
				totalPercent = 100
			}
			totalMB := float64(currentTotal) / 1024 / 1024
			speedMBps := totalMB / elapsed
			writePctText := ""
			if writeStats != nil {
				writePctText = fmt.Sprintf(" write~%.1f%%", writeStats.WritePercentApprox())
			}
			fmt.Fprintf(w, "[TOTAL] %5.1f%% %.2f/%.2f MB %.2f MB/s%s\n", totalPercent, totalMB, float64(totalSize)/1024/1024, speedMBps, writePctText)

			for i := range chunkTotals {
				chunkVal := atomic.LoadInt64(&chunkDownloaded[i])
				percent := 0.0
				if chunkTotals[i] > 0 {
					percent = float64(chunkVal) / float64(chunkTotals[i]) * 100
				}
				if percent > 100 {
					percent = 100
				}
				barWidth := 24
				filled := int(percent / 100 * float64(barWidth))
				if filled < 0 {
					filled = 0
				}
				if filled > barWidth {
					filled = barWidth
				}
				bar := strings.Repeat("=", filled) + strings.Repeat(" ", barWidth-filled)
				fmt.Fprintf(w, "[C%02d] [%s] %5.1f%% %.2f/%.2f MB\n", i, bar, percent, float64(chunkVal)/1024/1024, float64(chunkTotals[i])/1024/1024)
			}

			reportSnapshot(reporter, currentTotal, totalSize, start, writeStats)
			firstRender = false
			if final {
				fmt.Fprintln(w)
			}
		}

		for {
			select {
			case <-ticker.C:
				render(false)
			case <-done:
				render(true)
				return
			}
		}
	}()

	return done, finished
}

func reportSnapshot(reporter Reporter, downloaded, total int64, start time.Time, writeStats *DownloadStats) {
	if reporter == nil {
		return
	}
	elapsed := time.Since(start)
	elapsedSec := elapsed.Seconds()
	if elapsedSec <= 0 {
		elapsedSec = 1
	}
	percent := 0.0
	if total > 0 {
		percent = float64(downloaded) / float64(total) * 100
		if percent > 100 {
			percent = 100
		}
	}
	s := Snapshot{
		Downloaded: downloaded,
		Total:      total,
		SpeedMBps:  float64(downloaded) / 1024 / 1024 / elapsedSec,
		Percent:    percent,
		Elapsed:    elapsed,
	}
	if writeStats != nil {
		s.WritePercent = writeStats.WritePercentApprox()
	}
	reporter(s)
}

func renderProgress(w io.Writer, downloaded, total int64, start time.Time) {
	elapsed := time.Since(start).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}

	downloadedMB := float64(downloaded) / 1024 / 1024
	speedMBps := downloadedMB / elapsed

	if total > 0 {
		percent := float64(downloaded) / float64(total) * 100
		if percent > 100 {
			percent = 100
		}
		barWidth := 30
		filled := int(percent / 100 * float64(barWidth))
		if filled < 0 {
			filled = 0
		}
		if filled > barWidth {
			filled = barWidth
		}
		bar := strings.Repeat("=", filled) + strings.Repeat(" ", barWidth-filled)
		fmt.Fprintf(w, "\r[%s] %5.1f%% %.2f MB / %.2f MB %.2f MB/s", bar, percent, downloadedMB, float64(total)/1024/1024, speedMBps)
		return
	}

	fmt.Fprintf(w, "\r[%-30s]   ??.?%% %.2f MB / ? MB %.2f MB/s", "", downloadedMB, speedMBps)
}
