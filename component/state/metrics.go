package state

// Metrics 管理器执行指标。
type Metrics struct {
	ProcessCount int64
	ElapsedNanos int64
	ActiveTasks  int
}
