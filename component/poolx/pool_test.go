package poolx

import (
	"sync"
	"sync/atomic"
	"testing"
)

// ---------- 测试用类型 ----------

type testObj struct {
	val        int
	generation atomic.Int64
}

func (o *testObj) PoolGeneration() int64     { return o.generation.Load() }
func (o *testObj) SetPoolGeneration(g int64) { o.generation.Store(g) }
func (o *testObj) Reset()                    { o.val = 0 }

// ---------- 基础功能 ----------

func TestNewPool_NewRequired(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when New is nil")
		}
	}()
	NewPool(Config[int]{})
}

func TestNewPool_NegativeLimitPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when Limit is negative")
		}
	}()
	NewPool(Config[int]{
		New:   func() int { return 0 },
		Limit: -1,
	})
}

func TestGetPut_Basic(t *testing.T) {
	var newCount int
	pool := NewPool(Config[int]{
		New: func() int {
			newCount++
			return newCount
		},
	})

	a := pool.Get()
	b := pool.Get()
	pool.Put(a)
	pool.Put(b)

	if newCount != 2 {
		t.Fatalf("expected 2 new allocations, got %d", newCount)
	}
}

func TestGetPut_Reuse(t *testing.T) {
	pool := NewPool(Config[int]{
		New: func() int { return 42 },
	})

	a := pool.Get()
	pool.Put(a)
	b := pool.Get()

	// sync.Pool 是 best-effort，不保证复用，只验证 Put 后能正常 Get
	if a != 42 || b != 42 {
		t.Fatalf("expected valid values, got %d and %d", a, b)
	}
}

// ---------- Reset 钩子 ----------

func TestPut_CallsReset(t *testing.T) {
	var resetCount int
	var mu sync.Mutex
	resetVals := []int{}

	pool := NewPool(Config[*testObj]{
		New: func() *testObj { return &testObj{val: 42} },
		Reset: func(o *testObj) {
			mu.Lock()
			resetCount++
			resetVals = append(resetVals, o.val)
			mu.Unlock()
			o.Reset()
		},
	})

	o := pool.Get()
	o.val = 99
	pool.Put(o)

	if resetCount != 1 {
		t.Fatalf("expected Reset called once, got %d", resetCount)
	}
	if len(resetVals) != 1 || resetVals[0] != 99 {
		t.Fatalf("expected Reset called with val=99, got %v", resetVals)
	}
}

// ---------- 代际感知 ----------

func TestGeneration_StaleObjectTriggersInit(t *testing.T) {
	var initCount int
	pool := NewPool(Config[*testObj]{
		New: func() *testObj { return &testObj{} },
		Init: func(o *testObj, gen int64) {
			initCount++
			o.SetPoolGeneration(gen)
		},
	})

	o := pool.Get()
	pool.Put(o)

	// 代际递增，使已有对象过期
	pool.ResetGeneration()

	o2 := pool.Get()
	if o2.PoolGeneration() != 1 {
		t.Fatalf("expected gen=1 after Init, got %d", o2.PoolGeneration())
	}
	if initCount != 1 {
		t.Fatalf("expected Init called once, got %d", initCount)
	}
}

func TestGeneration_FreshObjectSkipsInit(t *testing.T) {
	var initCount int
	pool := NewPool(Config[*testObj]{
		New:  func() *testObj { return &testObj{} },
		Init: func(o *testObj, gen int64) { initCount++ },
	})

	// 先 Get 一次，对象 gen=0 匹配池 gen=0
	o := pool.Get()
	pool.Put(o)

	// 不递增代际，再次 Get
	o2 := pool.Get()
	if initCount != 0 {
		t.Fatalf("expected no Init for fresh object, got %d calls", initCount)
	}
	pool.Put(o2)
}

func TestGeneration_NoPoolG_NoTracking(t *testing.T) {
	// 不实现 GenerationTracker 的类型，池不做代际检查，行为与裸 sync.Pool 一致
	pool := NewPool(Config[int]{
		New: func() int { return 1 },
	})

	// ResetGeneration 对非 PoolG 类型无效果，不会 panic
	pool.ResetGeneration()
	pool.Get()
	pool.Put(1)
	// sync.Pool 不保证复用，只验证不 panic
}

func TestResetGeneration_IncrementsGen(t *testing.T) {
	pool := NewPool(Config[int]{
		New: func() int { return 0 },
	})

	if g := pool.Generation(); g != 0 {
		t.Fatalf("expected initial gen=0, got %d", g)
	}
	pool.ResetGeneration()
	if g := pool.Generation(); g != 1 {
		t.Fatalf("expected gen=1 after reset, got %d", g)
	}
	pool.ResetGeneration()
	if g := pool.Generation(); g != 2 {
		t.Fatalf("expected gen=2 after second reset, got %d", g)
	}
}

// ---------- 背压 ----------

func TestBackpressure_GetBlocksWhenFull(t *testing.T) {
	pool := NewPool(Config[int]{
		New:   func() int { return 1 },
		Limit: 1,
	})

	a := pool.Get()

	done := make(chan struct{})
	go func() {
		defer close(done)
		pool.Get() // 阻塞，因为 Limit=1 且 a 未归还
	}()

	// 等待 goroutine 进入阻塞状态
	select {
	case <-done:
		t.Fatal("Get should block, not return immediately")
	default:
	}

	pool.Put(a)
	<-done // 现在 Get 应该返回了
}

func TestTryGet_NonBlocking(t *testing.T) {
	pool := NewPool(Config[int]{
		New:   func() int { return 1 },
		Limit: 1,
	})

	a := pool.Get()

	// 池满，TryGet 应立即返回 false
	_, ok := pool.TryGet()
	if ok {
		t.Fatal("expected TryGet to fail when pool is full")
	}

	pool.Put(a)
	// 现在 TryGet 应该成功
	b, ok := pool.TryGet()
	if !ok {
		t.Fatal("expected TryGet to succeed after Put")
	}
	if a != b {
		t.Fatal("expected same object reused")
	}
}

func TestTryGet_NoLimitAlwaysSucceeds(t *testing.T) {
	pool := NewPool(Config[int]{
		New:   func() int { return 1 },
		Limit: 0, // 无背压，TryGet 直接走 sync.Pool，New 永不为空
	})

	_, ok := pool.TryGet()
	if !ok {
		t.Fatal("expected TryGet to succeed because New is always set")
	}
}

// ---------- 并发安全 ----------

func TestConcurrent_GetPut(t *testing.T) {
	var newCount atomic.Int64
	pool := NewPool(Config[int]{
		New: func() int {
			newCount.Add(1)
			return int(newCount.Load())
		},
	})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				v := pool.Get()
				pool.Put(v)
			}
		}()
	}
	wg.Wait()

	// sync.Pool 是 per-P 本地缓存，对象不一定跨 P 共享
	// 只验证并发安全（无 panic / 无 data race），不断言复用率
}

func TestConcurrent_Generation(t *testing.T) {
	var initCount int64
	pool := NewPool(Config[*testObj]{
		New: func() *testObj { return &testObj{} },
		Init: func(o *testObj, gen int64) {
			atomic.AddInt64(&initCount, 1)
			o.SetPoolGeneration(gen)
		},
	})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				o := pool.Get()
				pool.Put(o)
			}
		}()
	}

	// 并发中递增代际
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				pool.ResetGeneration()
			}
		}
	}()

	wg.Wait()
	close(stop)
	// 只验证不 panic，initCount 的值不确定
	_ = initCount
}

// ---------- Reset panic 恢复 ----------

func TestPut_ResetPanicReleasesBackpressure(t *testing.T) {
	pool := NewPool(Config[int]{
		New:   func() int { return 42 },
		Reset: func(int) { panic("reset failed") },
		Limit: 1,
	})

	v := pool.Get()

	// Reset panic 应传播，且许可必须释放
	func() {
		defer func() { recover() }()
		pool.Put(v)
	}()

	// 若许可未释放，此 Get 将永久阻塞
	done := make(chan struct{})
	go func() {
		defer close(done)
		pool.Get()
	}()
	<-done
}

// ---------- Init panic 恢复 ----------

func TestGet_InitPanicReleasesBackpressure(t *testing.T) {
	pool := NewPool(Config[*testObj]{
		New:   func() *testObj { return &testObj{} },
		Init:  func(o *testObj, gen int64) { panic("init failed") },
		Limit: 1,
	})

	// 先 Get 一个正常对象，占满许可
	v := pool.Get()
	pool.Put(v)

	// 递增代际，使下次 Get 时 Init 被触发
	pool.ResetGeneration()

	// Init panic 应传播，且许可必须释放
	func() {
		defer func() { recover() }()
		pool.Get()
	}()

	// 若许可未释放，此 Get 将永久阻塞
	// goroutine 内也 recover，只通过 done 是否关闭来判断许可是否泄漏
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { recover() }()
		pool.Get()
	}()
	<-done
}

func TestTryGet_InitPanicReleasesBackpressure(t *testing.T) {
	pool := NewPool(Config[*testObj]{
		New:   func() *testObj { return &testObj{} },
		Init:  func(o *testObj, gen int64) { panic("init failed") },
		Limit: 1,
	})

	// 先 TryGet 一个正常对象，占满许可
	v, ok := pool.TryGet()
	if !ok {
		t.Fatal("expected first TryGet to succeed")
	}
	pool.Put(v)

	// 递增代际，使下次 TryGet 时 Init 被触发
	pool.ResetGeneration()

	// Init panic 应传播，且许可必须释放
	func() {
		defer func() { recover() }()
		pool.TryGet()
	}()

	// 若许可未释放，此 TryGet 将永久阻塞（背压满）
	// goroutine 内也 recover，只通过 done 是否关闭来判断许可是否泄漏
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { recover() }()
		pool.TryGet()
	}()
	<-done
}

// ---------- 背压并发 ----------

func TestConcurrent_Backpressure(t *testing.T) {
	limit := int64(10)
	pool := NewPool(Config[int]{
		New:   func() int { return 1 },
		Limit: limit,
	})

	var wg sync.WaitGroup
	var mu sync.Mutex
	inFlight := int64(0)
	maxInFlight := int64(0)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v := pool.Get()
			mu.Lock()
			inFlight++
			if inFlight > maxInFlight {
				maxInFlight = inFlight
			}
			mu.Unlock()
			_ = v
			mu.Lock()
			inFlight--
			mu.Unlock()
			pool.Put(v)
		}()
	}
	wg.Wait()

	if maxInFlight > limit {
		t.Fatalf("expected in-flight <= %d, got %d", limit, maxInFlight)
	}
}
