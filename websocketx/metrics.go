package websocketx

// ── 监控埋点 ──────────────────────────────────────────────────

// MetricsHook 是监控埋点回调。
// event 为事件名称（connect / disconnect / message / broadcast / broadcast_room）。
// labels 为标准标签集合（通过 StandardMetricLabels 生成）。
type MetricsHook func(event string, labels map[string]string)

type hubMetrics struct {
	onConnect    MetricsHook
	onDisconnect MetricsHook
	onMessage    MetricsHook
	onBroadcast  MetricsHook
}

// triggerOnConnect 在锁外触发 onConnect 回调。
func (h *Hub) triggerOnConnect(session *Session) {
	if h.metrics.onConnect != nil {
		h.metrics.onConnect(string(MetricEventConnect), StandardMetricLabels(MetricEventConnect, map[string]string{
			"session_id": session.ID(),
			"user_id":    session.userID,
		}))
	}
	if h.onConnect != nil {
		h.onConnect(session)
	}
}

// triggerOnDisconnect 在锁外触发 onDisconnect 回调。
func (h *Hub) triggerOnDisconnect(session *Session) {
	if h.metrics.onDisconnect != nil {
		h.metrics.onDisconnect(string(MetricEventDisconnect), StandardMetricLabels(MetricEventDisconnect, map[string]string{
			"session_id": session.ID(),
			"user_id":    session.userID,
		}))
	}
	if h.onDisconnect != nil {
		h.onDisconnect(session)
	}
}
