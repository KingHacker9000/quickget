package tune

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

var DiskTestBufferSizes = []int{64 * 1024, 128 * 1024, 256 * 1024, 512 * 1024, 1024 * 1024, 2 * 1024 * 1024}

const (
	DiskTestFileSizeBytes = int64(256 * 1024 * 1024)
	DiskTestRepeats       = 3
)

type DiskTestResult struct {
	BufferSize      int
	ThroughputMBps  float64
	AvgWriteLatency time.Duration
}

func RecommendBufferSizeForPath(outputPath string) (DiskTestResult, error) {
	_, recommended, err := RunDiskBufferTest(outputPath)
	return recommended, err
}

func RunDiskBufferTest(outputPath string) ([]DiskTestResult, DiskTestResult, error) {
	results := make([]DiskTestResult, 0, len(DiskTestBufferSizes))
	bestThroughput := 0.0
	for _, sz := range DiskTestBufferSizes {
		r, err := measureDiskBufferRobust(outputPath, sz)
		if err != nil {
			return nil, DiskTestResult{}, err
		}
		results = append(results, r)
		if r.ThroughputMBps > bestThroughput {
			bestThroughput = r.ThroughputMBps
		}
	}
	if len(results) == 0 {
		return nil, DiskTestResult{}, errors.New("no disk test results")
	}
	threshold := bestThroughput * 0.95
	recommended := results[len(results)-1]
	for _, r := range results {
		if r.ThroughputMBps >= threshold {
			recommended = r
			break
		}
	}
	return results, recommended, nil
}

func measureDiskBufferRobust(outputPath string, bufferSize int) (DiskTestResult, error) {
	throughputs := make([]float64, 0, DiskTestRepeats)
	latencies := make([]time.Duration, 0, DiskTestRepeats)
	for i := 0; i < DiskTestRepeats; i++ {
		runPath := fmt.Sprintf("%s.%d.tmp", outputPath, i)
		r, err := measureDiskBuffer(runPath, bufferSize)
		if err != nil {
			return DiskTestResult{}, err
		}
		throughputs = append(throughputs, r.ThroughputMBps)
		latencies = append(latencies, r.AvgWriteLatency)
	}
	return DiskTestResult{
		BufferSize:      bufferSize,
		ThroughputMBps:  medianFloat64(throughputs),
		AvgWriteLatency: medianDuration(latencies),
	}, nil
}

func measureDiskBuffer(outputPath string, bufferSize int) (DiskTestResult, error) {
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return DiskTestResult{}, err
	}
	f, err := os.Create(outputPath)
	if err != nil {
		return DiskTestResult{}, err
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(outputPath)
	}()

	totalBytes := DiskTestFileSizeBytes
	buf := make([]byte, bufferSize)
	remaining := totalBytes
	var writeCount int64
	var totalWriteLatency time.Duration
	start := time.Now()
	for remaining > 0 {
		toWrite := len(buf)
		if int64(toWrite) > remaining {
			toWrite = int(remaining)
		}
		wStart := time.Now()
		n, wErr := f.Write(buf[:toWrite])
		totalWriteLatency += time.Since(wStart)
		writeCount++
		if wErr != nil {
			return DiskTestResult{}, wErr
		}
		remaining -= int64(n)
	}
	if err := f.Sync(); err != nil {
		return DiskTestResult{}, err
	}
	elapsed := time.Since(start)
	if elapsed <= 0 {
		elapsed = time.Nanosecond
	}
	throughput := (float64(totalBytes) / 1024.0 / 1024.0) / elapsed.Seconds()
	avgLatency := time.Duration(int64(totalWriteLatency) / writeCount)
	return DiskTestResult{
		BufferSize:      bufferSize,
		ThroughputMBps:  throughput,
		AvgWriteLatency: avgLatency,
	}, nil
}

func medianFloat64(v []float64) float64 {
	cp := append([]float64(nil), v...)
	sort.Float64s(cp)
	mid := len(cp) / 2
	if len(cp)%2 == 1 {
		return cp[mid]
	}
	return (cp[mid-1] + cp[mid]) / 2
}

func medianDuration(v []time.Duration) time.Duration {
	cp := append([]time.Duration(nil), v...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	mid := len(cp) / 2
	if len(cp)%2 == 1 {
		return cp[mid]
	}
	return (cp[mid-1] + cp[mid]) / 2
}

func FormatBytesBinary(n int) string {
	if n%(1024*1024) == 0 {
		return fmt.Sprintf("%dMB", n/(1024*1024))
	}
	return fmt.Sprintf("%dKB", n/1024)
}
