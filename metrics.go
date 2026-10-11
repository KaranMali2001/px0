package main

import (
	"math"
	"runtime"
	"sync"
	"time"
)

type ProcessMetrics struct {
	RSSBytes     uint64  `json:"rssBytes"`
	RSSAvailable bool    `json:"rssAvailable"`
	CPUUsage     float64 `json:"cpuUsage"`
	CPUAvailable bool    `json:"cpuAvailable"`
	Goroutine    int     `json:"goroutines"`
}

type metricsCollector struct {
	mu          sync.Mutex
	lastSample  time.Time
	lastCPUTime time.Duration
	lastUsage   float64
	hasUsage    bool
}

var globalMetrics = &metricsCollector{}

func getProcessMetrics() ProcessMetrics {
	rss, err := readProcessRSS()
	cpu, available := globalMetrics.sampleCPU()
	return ProcessMetrics{RSSBytes: rss, RSSAvailable: err == nil,
		CPUUsage: cpu, CPUAvailable: available, Goroutine: runtime.NumGoroutine()}
}

func (c *metricsCollector) sampleCPU() (float64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if !c.lastSample.IsZero() && now.Sub(c.lastSample) < 200*time.Millisecond {
		return c.lastUsage, c.hasUsage
	}
	cpuTime, err := readProcessCPUTime()
	if err != nil {
		return 0, false
	}
	if c.lastSample.IsZero() {
		c.lastSample, c.lastCPUTime = now, cpuTime
		return 0, false
	}
	c.lastUsage = math.Max(0, float64(cpuTime-c.lastCPUTime)/float64(now.Sub(c.lastSample))*100)
	c.lastSample, c.lastCPUTime = now, cpuTime
	c.hasUsage = true
	return c.lastUsage, true
}
