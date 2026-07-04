package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	// ReviewDuration tracks end-to-end review latency, labelled by final verdict.
	ReviewDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "review_duration_seconds",
			Help:    "End-to-end review pipeline latency in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"status"}, // APPROVE | REQUEST_CHANGES | COMMENT | error
	)

	// LLMTokensTotal counts tokens consumed by LLM calls, labelled by model and direction.
	LLMTokensTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "llm_tokens_total",
			Help: "Total LLM tokens consumed.",
		},
		[]string{"model", "direction"}, // direction: input | output
	)

	// ReviewQueueDepth is the number of review jobs currently available in the queue.
	ReviewQueueDepth = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "review_queue_depth",
			Help: "Number of review jobs pending in the River queue.",
		},
	)

	// WebhookRequestsTotal counts incoming webhook events by action.
	WebhookRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "webhook_requests_total",
			Help: "Total webhook events received.",
		},
		[]string{"action"}, // opened | synchronize | skipped | invalid_sig | error
	)

	// SuggestionsEmittedTotal counts agent-proposed fix suggestions that passed
	// validation and were posted as GitHub ```suggestion blocks.
	SuggestionsEmittedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "suggestions_emitted_total",
			Help: "Total auto-fix suggestions posted to GitHub.",
		},
	)

	// SuggestionsDroppedTotal counts agent-proposed suggestions rejected by
	// validation before posting, labelled by rejection reason.
	SuggestionsDroppedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "suggestions_dropped_total",
			Help: "Total auto-fix suggestions dropped by validation, labelled by reason.",
		},
		[]string{"reason"},
	)
)

func init() {
	prometheus.MustRegister(
		ReviewDuration,
		LLMTokensTotal,
		ReviewQueueDepth,
		WebhookRequestsTotal,
		SuggestionsEmittedTotal,
		SuggestionsDroppedTotal,
	)
}

// RecordLLMTokens records token usage for a single LLM call, labelled by model.
// An empty model is recorded as "unknown"; zero counts are skipped so empty
// directions don't create idle series.
func RecordLLMTokens(model string, inputTokens, outputTokens int) {
	if model == "" {
		model = "unknown"
	}
	if inputTokens > 0 {
		LLMTokensTotal.WithLabelValues(model, "input").Add(float64(inputTokens))
	}
	if outputTokens > 0 {
		LLMTokensTotal.WithLabelValues(model, "output").Add(float64(outputTokens))
	}
}
