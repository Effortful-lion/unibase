package adapter

import (
	"github.com/Effortful-lion/unibase/logx"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// PrometheusConfig Prometheus 配置。
type PrometheusConfig struct {
	// Namespace 指标命名空间。
	Namespace string

	// Subsystem 指标子系统。
	Subsystem string

	// ListenAddr Prometheus HTTP 监听地址，留空则不启动抓取端点。
	// 例如 ":9090"
	ListenAddr string
}

// Prometheus 是 Prometheus 客户端的薄封装。
// 提供指标注册器和常用指标类型的快捷创建方法。
type Prometheus struct {
	registry *prometheus.Registry
	logger   *logx.Logger
}

// NewPrometheus 创建 Prometheus 适配器。
func NewPrometheus(cfg PrometheusConfig) *Prometheus {
	return &Prometheus{
		registry: prometheus.NewRegistry(),
		logger:   logx.Module("adapter.prometheus"),
	}
}

// Registry 返回底层的 prometheus.Registry。
func (p *Prometheus) Registry() *prometheus.Registry { return p.registry }

// Register 注册指标采集器。
func (p *Prometheus) Register(c prometheus.Collector) error {
	return p.registry.Register(c)
}

// MustRegister 注册指标采集器，失败时 panic。
func (p *Prometheus) MustRegister(c prometheus.Collector) {
	p.registry.MustRegister(c)
}

// Unregister 注销指标采集器。
func (p *Prometheus) Unregister(c prometheus.Collector) bool {
	return p.registry.Unregister(c)
}

// NewCounter 创建并注册 Counter 指标。
func (p *Prometheus) NewCounter(opt prometheus.CounterOpts) prometheus.Counter {
	c := prometheus.NewCounter(opt)
	p.MustRegister(c)
	return c
}

// NewGauge 创建并注册 Gauge 指标。
func (p *Prometheus) NewGauge(opt prometheus.GaugeOpts) prometheus.Gauge {
	g := prometheus.NewGauge(opt)
	p.MustRegister(g)
	return g
}

// NewHistogram 创建并注册 Histogram 指标。
func (p *Prometheus) NewHistogram(opt prometheus.HistogramOpts) prometheus.Histogram {
	h := prometheus.NewHistogram(opt)
	p.MustRegister(h)
	return h
}

// NewSummary 创建并注册 Summary 指标。
func (p *Prometheus) NewSummary(opt prometheus.SummaryOpts) prometheus.Summary {
	s := prometheus.NewSummary(opt)
	p.MustRegister(s)
	return s
}

// Gather 收集所有注册的指标样本，返回 metric families。
func (p *Prometheus) Gather() ([]*dto.MetricFamily, error) {
	gatherer := prometheus.Gatherer(p.registry)
	return gatherer.Gather()
}

// Close Prometheus 客户端无需显式关闭。
// 保留此方法以统一接口。
func (p *Prometheus) Close() error {
	return nil
}
