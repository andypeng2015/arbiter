package grpcserver

import "github.com/prometheus/client_golang/prometheus"

var (
	evalTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "arbiter_eval_total",
		Help: "Total number of evaluations",
	}, []string{"bundle_name", "mode", "status"})

	evalDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "arbiter_eval_duration_seconds",
		Help:    "Evaluation latency in seconds",
		Buckets: prometheus.DefBuckets,
	}, []string{"bundle_name", "mode", "status"})

	ruleMatchesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "arbiter_rule_matches_total",
		Help: "Total rule matches",
	}, []string{"bundle_name"})

	expertRoundsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "arbiter_expert_rounds_total",
		Help: "Total expert inference rounds",
	}, []string{"bundle_name"})

	expertMutationsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "arbiter_expert_mutations_total",
		Help: "Total expert mutations",
	}, []string{"bundle_name"})

	flagResolvesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "arbiter_flag_resolves_total",
		Help: "Total flag resolves",
	}, []string{"bundle_name", "flag"})

	bundlePublishTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "arbiter_bundle_publish_total",
		Help: "Total bundles published",
	})

	activeSessions = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "arbiter_active_sessions",
		Help: "Currently active expert sessions",
	}, []string{"bundle_name"})
)

// RegisterMetrics registers all Arbiter metrics with the given registerer.
// Use prometheus.DefaultRegisterer for the global registry, or a custom
// *prometheus.Registry for isolated test registries.
func RegisterMetrics(reg prometheus.Registerer) {
	reg.MustRegister(
		evalTotal,
		evalDuration,
		ruleMatchesTotal,
		expertRoundsTotal,
		expertMutationsTotal,
		flagResolvesTotal,
		bundlePublishTotal,
		activeSessions,
	)
}
