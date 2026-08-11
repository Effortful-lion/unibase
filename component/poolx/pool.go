package poolx

import (
	"sync"
	"sync/atomic"
)

// GenerationTracker 是可选的生命周期标记接口。
// 需要代际感知的类型嵌入 atomic.Int64 并实现此接口。
// 未实现该接口的类型，池行为与裸 sync.Pool 一致。
type GenerationTracker interface {
	PoolGeneration() int64
	SetPoolGeneration(int64)
}

// Config 配置泛型对象池。
type Config[T any] struct {
	New   func() T       // 必填：创建新实例
	Reset func(T)        // 可选：Put 前调用，用于重置对象状态
	Init  func(T, int64) // 可选：对象代际过期时调用，用于重新初始化；必须是幂等的，且调用成功后对象必须处于完全可用状态
	Limit int64          // 背压上限，同时在外的对象数量不能超过此值；非正数表示无限制
}

// Pool 是线程安全的泛型对象池，底层委托给 sync.Pool。
type Pool[T any] struct {
	inner      *sync.Pool
	reset      func(T)
	init       func(T, int64)
	sem        chan struct{} // nil 表示无背压
	generation atomic.Int64  // 当前代际，ResetGeneration 时递增
}

// NewPool 创建对象池。cfg.New 为必填项，其余均为可选。
func NewPool[T any](cfg Config[T]) *Pool[T] {
	if cfg.New == nil {
		panic("poolx: New is required")
	}
	if cfg.Limit < 0 {
		panic("poolx: Limit must be >= 0")
	}
	p := &Pool[T]{
		inner: &sync.Pool{
			New: func() interface{} { return cfg.New() },
		},
		reset: cfg.Reset,
		init:  cfg.Init,
	}
	if cfg.Limit > 0 {
		p.sem = make(chan struct{}, cfg.Limit)
	}
	return p
}

// Get 从池中取出一个对象。
// 若配置了 Limit 且池满，阻塞直到有对象被归还。
// Init panic 时许可照常释放，避免永久背压泄漏。
func (p *Pool[T]) Get() T {
	if p.sem != nil {
		p.sem <- struct{}{}
		defer func() {
			if r := recover(); r != nil {
				<-p.sem
				panic(r)
			}
		}()
	}
	return p.checkGeneration(p.inner.Get().(T))
}

// TryGet 非阻塞尝试从池中取出对象。
// 背压满时返回零值和 false；池空时通过 New 创建新对象并返回 true。
// Init panic 时许可照常释放，避免永久背压泄漏。
func (p *Pool[T]) TryGet() (T, bool) {
	if p.sem != nil {
		select {
		case p.sem <- struct{}{}:
		default:
			var zero T
			return zero, false
		}
		defer func() {
			if r := recover(); r != nil {
				<-p.sem
				panic(r)
			}
		}()
	}
	return p.checkGeneration(p.inner.Get().(T)), true
}

// Put 将对象归还池中。归还前会调用 Reset（如果配置了）。
// Reset panic 时对象不回池，但许可照常释放，避免死锁。
// 对于 Pool[*T] 类型，传入 nil 指针会按普通对象处理——Reset 和放回池均正常执行，
// 调用方应确保 Reset 能处理 nil 值，或避免传入 nil。
func (p *Pool[T]) Put(v T) {
	if p.sem != nil {
		defer func() { <-p.sem }()
	}
	if p.reset != nil {
		p.reset(v)
	}
	p.inner.Put(v)
}

// Generation 返回当前代际编号。
func (p *Pool[T]) Generation() int64 {
	return p.generation.Load()
}

// ResetGeneration 递增代际，使所有已在池中或流落在外的对象标记为过期。
// 这些对象下次进入 Get 时会触发 Init 重新初始化。
func (p *Pool[T]) ResetGeneration() {
	p.generation.Add(1)
}

// checkGeneration 检查对象的代际标签，过期则重新初始化。
// generation 更新在 Init 成功之后，Init panic 时对象保持过期状态，
// 调用方修复 Init 后会再次尝试。
func (p *Pool[T]) checkGeneration(v T) T {
	if tracker, ok := any(v).(GenerationTracker); ok {
		currentGeneration := p.generation.Load()
		if tracker.PoolGeneration() != currentGeneration {
			if p.init != nil {
				p.init(v, currentGeneration)
			}
			tracker.SetPoolGeneration(currentGeneration)
		}
	}
	return v
}
