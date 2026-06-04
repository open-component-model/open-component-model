package workerpool

import (
	kmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	"ocm.software/open-component-model/kubernetes/controller/internal/metrics"
)

func init() {
	kmetrics.Registry.MustRegister(
		TransferQueueSizeGauge,
		TransferInProgressGauge,
		TransferDurationHistogram,
		TransferEventChannelDropsTotal,
		TransferTotal,
	)
}

const (
	// MetricsNamespace defines the namespace of all transfer metrics.
	MetricsNamespace = "ocm_system"
	// OcmComponent is the name of the component registering for these metrics.
	OcmComponent = "ocm_k8s_toolkit"
)

const (
	// TransferQueueSizeLabel tracks the current size of the transfer queue.
	TransferQueueSizeLabel = "transfer_queue_size"
	// TransferInProgressLabel tracks the number of transfers currently in progress.
	TransferInProgressLabel = "transfer_in_progress"
	// TransferDurationLabel tracks the duration of transfers.
	TransferDurationLabel = "transfer_duration_seconds"
	// TransferEventChannelDropsLabel tracks dropped completion events.
	TransferEventChannelDropsLabel = "transfer_event_channel_drops"
	// TransferTotalLabel tracks the number of completed transfers by result.
	TransferTotalLabel = "transfer_total"
)

const (
	// ResultLabel is the name of the label for the terminal result of a transfer.
	ResultLabel = "result"
)

const (
	resultSuccess  = "success"
	resultError    = "error"
	resultCanceled = "canceled"
)

// TransferQueueSizeGauge tracks the current size of the transfer queue.
var TransferQueueSizeGauge = metrics.MustRegisterGauge(
	MetricsNamespace,
	OcmComponent,
	TransferQueueSizeLabel,
	"Current size of the component version transfer queue.",
)

// TransferInProgressGauge tracks the number of transfers currently in progress.
var TransferInProgressGauge = metrics.MustRegisterGauge(
	MetricsNamespace,
	OcmComponent,
	TransferInProgressLabel,
	"Number of component version transfers currently in progress.",
)

// TransferDurationHistogram tracks the duration of transfers.
// [result].
var TransferDurationHistogram = metrics.MustRegisterHistogramVec(
	MetricsNamespace,
	OcmComponent,
	TransferDurationLabel,
	"Duration of component version transfers in seconds.",
	[]float64{.05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60, 120, 300},
	ResultLabel,
)

// TransferEventChannelDropsTotal counts dropped completion events due to channel overflow.
var TransferEventChannelDropsTotal = metrics.MustRegisterCounterVec(
	MetricsNamespace,
	OcmComponent,
	TransferEventChannelDropsLabel,
	"Number of times transfer completion events could not be emitted due to channel overflow.",
)

// TransferTotal counts completed transfers by result.
// [result].
var TransferTotal = metrics.MustRegisterCounterVec(
	MetricsNamespace,
	OcmComponent,
	TransferTotalLabel,
	"Number of completed component version transfers by terminal result.",
	ResultLabel,
)
