package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const controllerStart = `# TYPE controller_runtime_reconcile_total counter
controller_runtime_reconcile_total{controller="component",result="success"} 10
controller_runtime_reconcile_total{controller="component",result="error"} 1
# TYPE ocm_system_ocm_k8s_toolkit_cache_miss counter
ocm_system_ocm_k8s_toolkit_cache_miss{component="a",version="1.0.0",verification_state="unverified"} 1
ocm_system_ocm_k8s_toolkit_cache_miss{component="b",version="1.0.0",verification_state="unverified"} 2
# TYPE controller_runtime_reconcile_time_seconds histogram
controller_runtime_reconcile_time_seconds_bucket{controller="component",le="1"} 4
controller_runtime_reconcile_time_seconds_bucket{controller="component",le="+Inf"} 5
controller_runtime_reconcile_time_seconds_sum{controller="component"} 2.5
controller_runtime_reconcile_time_seconds_count{controller="component"} 5
# TYPE workqueue_depth gauge
workqueue_depth{name="component"} 3
workqueue_depth{name="resource"} 0
# TYPE go_goroutines gauge
go_goroutines 42
`

const controllerEnd = `# TYPE controller_runtime_reconcile_total counter
controller_runtime_reconcile_total{controller="component",result="success"} 110
controller_runtime_reconcile_total{controller="component",result="error"} 1
# TYPE ocm_system_ocm_k8s_toolkit_cache_miss counter
ocm_system_ocm_k8s_toolkit_cache_miss{component="a",version="1.0.0",verification_state="unverified"} 4
ocm_system_ocm_k8s_toolkit_cache_miss{component="b",version="1.0.0",verification_state="unverified"} 5
ocm_system_ocm_k8s_toolkit_cache_miss{component="c",version="1.0.0",verification_state="unverified"} 7
# TYPE controller_runtime_reconcile_time_seconds histogram
controller_runtime_reconcile_time_seconds_bucket{controller="component",le="1"} 100
controller_runtime_reconcile_time_seconds_bucket{controller="component",le="+Inf"} 105
controller_runtime_reconcile_time_seconds_sum{controller="component"} 52.5
controller_runtime_reconcile_time_seconds_count{controller="component"} 105
# TYPE workqueue_depth gauge
workqueue_depth{name="component"} 0
workqueue_depth{name="resource"} 0
# TYPE go_goroutines gauge
go_goroutines 42
`

const kubeletMetrics = `# TYPE container_cpu_usage_seconds_total counter
container_cpu_usage_seconds_total{container="manager",namespace="ocm-k8s-toolkit-system",pod="ctl-abc"} 12.5 1700000000000
container_cpu_usage_seconds_total{container="manager",namespace="ocm-k8s-toolkit-system",pod="ctl-old"} 99 1700000000000
# TYPE container_memory_working_set_bytes gauge
container_memory_working_set_bytes{container="manager",namespace="ocm-k8s-toolkit-system",pod="ctl-abc"} 1.048576e+08 1700000000000
`

func TestCounterDeltas(t *testing.T) {
	r := require.New(t)
	start, err := parseScrape([]byte(controllerStart))
	r.NoError(err)
	end, err := parseScrape([]byte(controllerEnd))
	r.NoError(err)

	deltas := counterDeltas(start, end, []string{"controller_runtime_", "ocm_system_"})

	r.InDelta(100, deltas[`controller_runtime_reconcile_total{controller="component",result="success"}`], 0)
	r.InDelta(0, deltas[`controller_runtime_reconcile_total{controller="component",result="error"}`], 0)
	r.InDelta(100, deltas[`controller_runtime_reconcile_time_seconds_count{controller="component"}`], 0)
	r.InDelta(50, deltas[`controller_runtime_reconcile_time_seconds_sum{controller="component"}`], 0)
	// Per-component series fold into one, including a series new in the end scrape.
	r.InDelta(13, deltas[`ocm_system_ocm_k8s_toolkit_cache_miss{verification_state="unverified"}`], 0)
	r.NotContains(deltas, "workqueue_depth{name=\"component\"}", "gauges are not deltas")
	r.InDelta(100, sumSeries(deltas, "controller_runtime_reconcile_total"), 0)
}

func TestGaugeValues(t *testing.T) {
	r := require.New(t)
	s, err := parseScrape([]byte(controllerStart))
	r.NoError(err)

	gauges := gaugeValues(s, []string{"workqueue_depth", "go_goroutines"})

	r.Equal(map[string]float64{
		`workqueue_depth{name="component"}`: 3,
		`workqueue_depth{name="resource"}`:  0,
		"go_goroutines":                     42,
	}, gauges)
}

func TestContainerValue(t *testing.T) {
	tests := []struct {
		name   string
		family string
		pod    string
		want   float64
		found  bool
	}{
		{name: "counter for the current pod", family: "container_cpu_usage_seconds_total", pod: "ctl-abc", want: 12.5, found: true},
		{name: "gauge for the current pod", family: "container_memory_working_set_bytes", pod: "ctl-abc", want: 104857600, found: true},
		{name: "other pod is ignored", family: "container_memory_working_set_bytes", pod: "ctl-old", found: false},
		{name: "missing family", family: "container_start_time_seconds", pod: "ctl-abc", found: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			s, err := parseScrape([]byte(kubeletMetrics))
			r.NoError(err)

			got, ok := containerValue(s, tt.family, tt.pod, managerContainer)
			r.Equal(tt.found, ok)
			r.InDelta(tt.want, got, 0)
		})
	}
}

func TestPeaks(t *testing.T) {
	r := require.New(t)
	samples := []sample{
		{WorkingSetBytes: 10, Gauges: map[string]float64{"a": 1, "b": 5}},
		{WorkingSetBytes: 30, Gauges: map[string]float64{"a": 4}},
		{WorkingSetBytes: 20, Gauges: map[string]float64{"a": 2, "b": 3}},
	}

	ws, gauges := peaks(samples)

	r.InDelta(30, ws, 0)
	r.Equal(map[string]float64{"a": 4, "b": 5}, gauges)
}
