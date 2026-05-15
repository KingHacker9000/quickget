package main

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

const refreshIntervalMS = 200

func printProgress(downloaded, total int64, start time.Time) {
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
		fmt.Printf("\r[%s] %5.1f%% %.2f MB / %.2f MB %.2f MB/s", bar, percent, downloadedMB, float64(total)/1024/1024, speedMBps)
		return
	}

	fmt.Printf("\r[%-30s]   ??.?%% %.2f MB / ? MB %.2f MB/s", "", downloadedMB, speedMBps)
}

func startProgressLoop(downloaded *int64, totalSize int64) (chan struct{}, chan struct{}) {
	done := make(chan struct{})
	finished := make(chan struct{})
	start := time.Now()
	go func() {
		defer close(finished)
		ticker := time.NewTicker(refreshIntervalMS * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				current := atomic.LoadInt64(downloaded)
				printProgress(current, totalSize, start)
			case <-done:
				current := atomic.LoadInt64(downloaded)
				printProgress(current, totalSize, start)
				fmt.Println()
				return
			}
		}
	}()
	return done, finished
}

func startVerboseProgressLoop(downloaded *int64, totalSize int64, chunkTotals []int64, chunkDownloaded []int64, writeStats *DownloadStats) (chan struct{}, chan struct{}) {
	done := make(chan struct{})
	finished := make(chan struct{})
	start := time.Now()

	go func() {
		defer close(finished)
		ticker := time.NewTicker(refreshIntervalMS * time.Millisecond)
		defer ticker.Stop()
		lines := len(chunkTotals) + 1
		firstRender := true

		render := func(final bool) {
			if !firstRender {
				fmt.Printf("\033[%dA", lines)
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
			fmt.Printf("[TOTAL] %5.1f%% %.2f/%.2f MB %.2f MB/s%s\n", totalPercent, totalMB, float64(totalSize)/1024/1024, speedMBps, writePctText)

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
				fmt.Printf("[C%02d] [%s] %5.1f%% %.2f/%.2f MB\n", i, bar, percent, float64(chunkVal)/1024/1024, float64(chunkTotals[i])/1024/1024)
			}

			firstRender = false
			if final {
				fmt.Println()
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
