package middleware

import (
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// MetricsOption 指标中间件配置。
type MetricsOption func(*metricsConfig)

type metricsConfig struct {
	namespace string
	subsystem string
}

func defaultMetricsConfig() *metricsConfig {
	return &metricsConfig{
		namespace: "http",
		subsystem: "server",
	}
}

// WithNamespace 设置指标命名空间。
func WithNamespace(ns string) MetricsOption {
	return func(c *metricsConfig) {
		c.namespace = ns
	}
}

// WithSubsystem 设置指标子系统。
func WithSubsystem(ss string) MetricsOption {
	return func(c *metricsConfig) {
		c.subsystem = ss
	}
}

var (
	metricsRequestsTotal   *prometheus.CounterVec
	metricsRequestDuration *prometheus.HistogramVec
	metricsMu              sync.Mutex
)

// Metrics 返回 Prometheus 指标中间件。
// 幂等：多次调用只注册一次 metric 实例并复用。
func Metrics(opts ...MetricsOption) gin.HandlerFunc {
	cfg := defaultMetricsConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	metricsMu.Lock()
	defer metricsMu.Unlock()

	if metricsRequestsTotal == nil {
		metricsRequestsTotal = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: cfg.namespace,
				Subsystem: cfg.subsystem,
				Name:      "requests_total",
				Help:      "Total number of HTTP requests",
			},
			[]string{"method", "path", "status"},
		)
		metricsRequestDuration = prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: cfg.namespace,
				Subsystem: cfg.subsystem,
				Name:      "request_duration_seconds",
				Help:      "HTTP request duration in seconds",
				Buckets:   []float64{0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
			},
			[]string{"method", "path"},
		)
		prometheus.MustRegister(metricsRequestsTotal, metricsRequestDuration)
	}

	requestsTotal := metricsRequestsTotal
	requestDuration := metricsRequestDuration

	return func(c *gin.Context) {
		start := time.Now()
		path := c.FullPath()
		if path == "" {
			path = "unknown"
		}

		c.Next()

		duration := time.Since(start).Seconds()
		status := strconv.Itoa(c.Writer.Status())
		method := c.Request.Method

		requestsTotal.WithLabelValues(method, path, status).Inc()
		requestDuration.WithLabelValues(method, path).Observe(duration)
	}
}

// MetricsHandler 返回 Prometheus metrics 端点 handler。
func MetricsHandler() gin.HandlerFunc {
	return gin.WrapH(promhttp.Handler())
}
