package main

import (
	"bytes"
	"fmt"
	"maps"
	"slices"
	"strings"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

// scrape is one parsed Prometheus text exposition.
type scrape map[string]*dto.MetricFamily

func parseScrape(raw []byte) (scrape, error) {
	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("parsing metrics: %w", err)
	}
	return families, nil
}

// foldedLabels are dropped before aggregating series. The resolver labels its
// metrics per component and version, which would yield one series per object.
var foldedLabels = map[string]bool{
	"component": true,
	"version":   true,
	"pod":       true,
	"namespace": true,
	"le":        true,
	"quantile":  true,
}

// counterDeltas returns end minus start for every counter, histogram and
// summary whose family matches one of prefixes. Histograms and summaries
// contribute their _count and _sum series.
func counterDeltas(start, end scrape, prefixes []string) map[string]float64 {
	before := cumulativeValues(start, prefixes)
	out := map[string]float64{}
	for key, v := range cumulativeValues(end, prefixes) {
		out[key] = v - before[key]
	}
	return out
}

func cumulativeValues(s scrape, prefixes []string) map[string]float64 {
	out := map[string]float64{}
	for name, mf := range s {
		if !hasAnyPrefix(name, prefixes) {
			continue
		}
		for _, m := range mf.GetMetric() {
			switch mf.GetType() {
			case dto.MetricType_COUNTER:
				out[seriesKey(name, m)] += m.GetCounter().GetValue()
			case dto.MetricType_HISTOGRAM:
				out[seriesKey(name+"_count", m)] += float64(m.GetHistogram().GetSampleCount())
				out[seriesKey(name+"_sum", m)] += m.GetHistogram().GetSampleSum()
			case dto.MetricType_SUMMARY:
				out[seriesKey(name+"_count", m)] += float64(m.GetSummary().GetSampleCount())
				out[seriesKey(name+"_sum", m)] += m.GetSummary().GetSampleSum()
			default:
			}
		}
	}
	return out
}

// gaugeValues sums every gauge series matching prefixes, after folding labels.
func gaugeValues(s scrape, prefixes []string) map[string]float64 {
	out := map[string]float64{}
	for name, mf := range s {
		if mf.GetType() != dto.MetricType_GAUGE || !hasAnyPrefix(name, prefixes) {
			continue
		}
		for _, m := range mf.GetMetric() {
			out[seriesKey(name, m)] += m.GetGauge().GetValue()
		}
	}
	return out
}

// containerValue reads a kubelet resource metric for one container.
func containerValue(s scrape, family, pod, container string) (float64, bool) {
	mf, ok := s[family]
	if !ok {
		return 0, false
	}
	for _, m := range mf.GetMetric() {
		labels := map[string]string{}
		for _, l := range m.GetLabel() {
			labels[l.GetName()] = l.GetValue()
		}
		if labels["pod"] != pod || labels["container"] != container {
			continue
		}
		switch mf.GetType() {
		case dto.MetricType_COUNTER:
			return m.GetCounter().GetValue(), true
		case dto.MetricType_GAUGE:
			return m.GetGauge().GetValue(), true
		case dto.MetricType_UNTYPED:
			return m.GetUntyped().GetValue(), true
		default:
		}
	}
	return 0, false
}

// sumSeries adds up every series of one metric name in a seriesKey map.
func sumSeries(values map[string]float64, name string) float64 {
	var total float64
	for key, v := range values {
		if key == name || strings.HasPrefix(key, name+"{") {
			total += v
		}
	}
	return total
}

func seriesKey(name string, m *dto.Metric) string {
	labels := map[string]string{}
	for _, l := range m.GetLabel() {
		if !foldedLabels[l.GetName()] {
			labels[l.GetName()] = l.GetValue()
		}
	}
	if len(labels) == 0 {
		return name
	}
	parts := make([]string, 0, len(labels))
	for _, k := range slices.Sorted(maps.Keys(labels)) {
		parts = append(parts, fmt.Sprintf("%s=%q", k, labels[k]))
	}
	return name + "{" + strings.Join(parts, ",") + "}"
}

func hasAnyPrefix(name string, prefixes []string) bool {
	return slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(name, p) })
}
