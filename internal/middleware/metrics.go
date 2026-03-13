package middleware

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	RequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "antigateway_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)

	RequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "antigateway_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)

	TokensInput = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "antigateway_tokens_input_total",
		Help: "Total input tokens processed",
	})

	TokensOutput = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "antigateway_tokens_output_total",
		Help: "Total output tokens generated",
	})

	ToolCallsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "antigateway_tool_calls_total",
		Help: "Total tool calls made",
	})

	ErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "antigateway_errors_total",
			Help: "Total errors by type",
		},
		[]string{"type"},
	)

	AutoContinueTriggered = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "antigateway_auto_continue_triggered_total",
		Help: "Total auto-continuation triggers",
	})

	ProviderRetries = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "antigateway_provider_retries_total",
			Help: "Total provider retry attempts",
		},
		[]string{"provider"},
	)
)

func init() {
	prometheus.MustRegister(
		RequestsTotal,
		RequestDuration,
		TokensInput,
		TokensOutput,
		ToolCallsTotal,
		ErrorsTotal,
		AutoContinueTriggered,
		ProviderRetries,
	)
}

// Metrics returns a Gin middleware that records request metrics.
func Metrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		status := strconv.Itoa(c.Writer.Status())
		duration := time.Since(start).Seconds()

		RequestsTotal.WithLabelValues(c.Request.Method, c.FullPath(), status).Inc()
		RequestDuration.WithLabelValues(c.Request.Method, c.FullPath()).Observe(duration)
	}
}
