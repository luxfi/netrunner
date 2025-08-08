// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package orchestrator

import (
	"sync"
	"time"
)

// MetricsCollector collects metrics from engines
type MetricsCollector struct {
	mu      sync.RWMutex
	metrics map[string]*EngineMetrics
}

// EngineMetrics contains metrics for an engine
type EngineMetrics struct {
	Name        string
	LastUpdated time.Time
	Values      map[string]interface{}
	History     []MetricSnapshot
}

// MetricSnapshot is a point-in-time metric snapshot
type MetricSnapshot struct {
	Timestamp time.Time
	Values    map[string]interface{}
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		metrics: make(map[string]*EngineMetrics),
	}
}

// Record records metrics for an engine
func (mc *MetricsCollector) Record(engine string, values map[string]interface{}) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	
	em, exists := mc.metrics[engine]
	if !exists {
		em = &EngineMetrics{
			Name:    engine,
			History: make([]MetricSnapshot, 0, 100),
		}
		mc.metrics[engine] = em
	}
	
	em.LastUpdated = time.Now()
	em.Values = values
	
	// Add to history (keep last 100 snapshots)
	snapshot := MetricSnapshot{
		Timestamp: em.LastUpdated,
		Values:    values,
	}
	em.History = append(em.History, snapshot)
	if len(em.History) > 100 {
		em.History = em.History[1:]
	}
}

// Get returns current metrics for an engine
func (mc *MetricsCollector) Get(engine string) *EngineMetrics {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	
	return mc.metrics[engine]
}

// GetAll returns all metrics
func (mc *MetricsCollector) GetAll() map[string]*EngineMetrics {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	
	result := make(map[string]*EngineMetrics)
	for k, v := range mc.metrics {
		result[k] = v
	}
	return result
}

// Clear clears metrics for an engine
func (mc *MetricsCollector) Clear(engine string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	
	delete(mc.metrics, engine)
}

// Summary returns a summary of all metrics
func (mc *MetricsCollector) Summary() map[string]interface{} {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	
	summary := make(map[string]interface{})
	summary["engines"] = len(mc.metrics)
	
	var totalUptime float64
	var healthyCount int
	
	for _, em := range mc.metrics {
		if uptime, ok := em.Values["uptime_seconds"].(float64); ok {
			totalUptime += uptime
		}
		if running, ok := em.Values["running"].(bool); ok && running {
			healthyCount++
		}
	}
	
	summary["total_uptime_seconds"] = totalUptime
	summary["healthy_engines"] = healthyCount
	
	return summary
}