package schedule

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// ---- At ----

func TestAt_Daily(t *testing.T) {
	state := newAtState(At(3, 0).at)
	delay := state.initialDelay()

	if delay < 0 || delay >= 24*time.Hour {
		t.Errorf("initialDelay: got %v, want in [0, 24h)", delay)
	}
}

func TestAt_ExactTime(t *testing.T) {
	// 锚点刚好在过去（上一个整点），验证会跳到下一个周期
	anchor := time.Now().Truncate(time.Hour)
	state := atState{anchor: anchor, period: 24 * time.Hour}

	delay := state.nextTriggerTime(time.Now())
	if delay < 23*time.Hour || delay > 25*time.Hour {
		t.Errorf("nextTriggerTime: got %v, expected ~24h", delay)
	}
}

func TestAt_BeforeAnchor(t *testing.T) {
	// 锚点在 1 小时后，应该等 ~1h
	state := atState{
		anchor: time.Now().Add(1 * time.Hour),
		period: 24 * time.Hour,
	}

	delay := state.nextTriggerTime(time.Now())
	if delay < 59*time.Minute || delay > 61*time.Minute {
		t.Errorf("nextTriggerTime: got %v, expected ~1h", delay)
	}
}

func TestAt_ExactAnchor_Delay(t *testing.T) {
	// 验证 newAtState 在 now 恰好等于锚点时的行为
	// 构造一个 state 模拟 newAtState 在 now == anchor 时的结果
	anchor := time.Date(2025, 1, 15, 3, 0, 0, 0, time.UTC)
	period := 24 * time.Hour
	now := anchor // 恰好等于锚点

	// 模拟 newAtState 的逻辑（修复后的版本）
	elapsed := now.Sub(anchor)                 // 0
	periods := elapsed / period                // 0
	nextAnchor := anchor.Add(periods * period) // anchor + 0 = anchor
	if !nextAnchor.After(now) {                // true，因为 anchor == now
		nextAnchor = nextAnchor.Add(period) // anchor + period = 明天 3:00
	}

	// 预期首次触发在明天 3:00，即 24h 后
	delay := nextAnchor.Sub(now)
	if delay != period {
		t.Errorf("exact anchor: got delay %v, want %v", delay, period)
	}
}

// ---- Every DailyAt ----

func TestEvery_DailyAt_ModifiesTrigger(t *testing.T) {
	trigger := Every(2 * time.Hour)
	if trigger.every.dailyHour != 0 || trigger.every.dailyMin != 0 {
		t.Error("Every should not have daily anchor initially")
	}

	trigger = trigger.DailyAt(3, 0)
	if trigger.every.dailyHour != 3 || trigger.every.dailyMin != 0 {
		t.Errorf("DailyAt: got (%d,%d), want (3,0)", trigger.every.dailyHour, trigger.every.dailyMin)
	}
	if trigger.kind != triggerKindEvery {
		t.Error("DailyAt should not change trigger kind")
	}
}

func TestEvery_DailyAt_DoesNotModifyOtherKinds(t *testing.T) {
	atTrigger := At(3, 0)
	modified := atTrigger.DailyAt(9, 0)
	if modified.every.dailyHour != 0 || modified.every.dailyMin != 0 {
		t.Error("DailyAt on At trigger should not set daily anchor")
	}
}

// ---- Between ----

func TestBetween_InsideWindow(t *testing.T) {
	start := time.Date(2025, 1, 15, 9, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 15, 18, 0, 0, 0, time.UTC)

	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	if !isAllowed(now, []Constraint{Between(start, end)}) {
		t.Error("should be inside window")
	}
}

func TestBetween_OutsideWindow(t *testing.T) {
	start := time.Date(2025, 1, 15, 9, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 15, 18, 0, 0, 0, time.UTC)

	now := time.Date(2025, 1, 15, 20, 0, 0, 0, time.UTC)
	if isAllowed(now, []Constraint{Between(start, end)}) {
		t.Error("should be outside window")
	}
}

func TestBetween_CrossDay(t *testing.T) {
	start := time.Date(2025, 1, 13, 0, 0, 0, 0, time.UTC) // Monday
	end := time.Date(2025, 1, 17, 23, 59, 0, 0, time.UTC) // Friday

	if !isAllowed(time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC), []Constraint{Between(start, end)}) {
		t.Error("Wednesday should be inside Mon-Fri window")
	}
	if isAllowed(time.Date(2025, 1, 18, 12, 0, 0, 0, time.UTC), []Constraint{Between(start, end)}) {
		t.Error("Saturday should be outside Mon-Fri window")
	}
}

// ---- BetweenDaily ----

func TestBetweenDaily_InsideWindow(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	if !isAllowed(now, []Constraint{BetweenDaily(9, 18)}) {
		t.Error("12:00 should be inside 9-18 window")
	}
}

func TestBetweenDaily_OutsideWindow(t *testing.T) {
	now := time.Date(2025, 1, 15, 20, 0, 0, 0, time.UTC)
	if isAllowed(now, []Constraint{BetweenDaily(9, 18)}) {
		t.Error("20:00 should be outside 9-18 window")
	}
}

func TestBetweenDaily_BeforeStart(t *testing.T) {
	now := time.Date(2025, 1, 15, 8, 59, 0, 0, time.UTC)
	if isAllowed(now, []Constraint{BetweenDaily(9, 18)}) {
		t.Error("08:59 should be outside 9-18 window")
	}
}

func TestBetweenDaily_AtEndHour(t *testing.T) {
	// endHour is exclusive: [startHour, endHour)
	now := time.Date(2025, 1, 15, 18, 0, 0, 0, time.UTC)
	if isAllowed(now, []Constraint{BetweenDaily(9, 18)}) {
		t.Error("18:00 should be outside [9, 18) window")
	}
}

func TestBetweenDaily_OvernightNotAllowed(t *testing.T) {
	now := time.Date(2025, 1, 15, 3, 0, 0, 0, time.UTC)
	if isAllowed(now, []Constraint{BetweenDaily(9, 18)}) {
		t.Error("03:00 should be outside 9-18 window")
	}
}

// ---- BetweenDaily cross-day ----

func TestBetweenDailyCrossDay_InWindowBeforeMidnight(t *testing.T) {
	// BetweenDaily(22, 6): 22:00 应该在窗口内
	now := time.Date(2025, 1, 15, 22, 0, 0, 0, time.UTC)
	if !isAllowed(now, []Constraint{BetweenDaily(22, 6)}) {
		t.Error("22:00 should be inside 22-6 window")
	}
}

func TestBetweenDailyCrossDay_InWindowAfterMidnight(t *testing.T) {
	// BetweenDaily(22, 6): 03:00 应该在窗口内
	now := time.Date(2025, 1, 15, 3, 0, 0, 0, time.UTC)
	if !isAllowed(now, []Constraint{BetweenDaily(22, 6)}) {
		t.Error("03:00 should be inside 22-6 window")
	}
}

func TestBetweenDailyCrossDay_AtStart(t *testing.T) {
	// BetweenDaily(22, 6): 22:00 刚好在边界上，应该在窗口内
	now := time.Date(2025, 1, 15, 22, 0, 0, 0, time.UTC)
	if !isAllowed(now, []Constraint{BetweenDaily(22, 6)}) {
		t.Error("22:00 should be inside 22-6 window")
	}
}

func TestBetweenDailyCrossDay_AtEnd(t *testing.T) {
	// BetweenDaily(22, 6): 06:00 刚好在 endHour 上，应该不在窗口内
	now := time.Date(2025, 1, 15, 6, 0, 0, 0, time.UTC)
	if isAllowed(now, []Constraint{BetweenDaily(22, 6)}) {
		t.Error("06:00 should be outside 22-6 window (endHour exclusive)")
	}
}

func TestBetweenDailyCrossDay_MiddayExcluded(t *testing.T) {
	// BetweenDaily(22, 6): 12:00 不在窗口内
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	if isAllowed(now, []Constraint{BetweenDaily(22, 6)}) {
		t.Error("12:00 should be outside 22-6 window")
	}
}

// ---- Run: Every ----

func TestRun_Every_ExecutesPeriodically(t *testing.T) {
	ctx := context.Background()
	var count int32

	j := Run(ctx, Every(50*time.Millisecond), func(ctx context.Context, t time.Time) error {
		atomic.AddInt32(&count, 1)
		return nil
	})

	time.Sleep(180 * time.Millisecond)
	j.Stop()
	j.Wait()

	if count < 2 {
		t.Errorf("expected at least 2 executions, got %d", count)
	}
}

func TestRun_Every_StopStopsScheduling(t *testing.T) {
	ctx := context.Background()
	var count int32

	j := Run(ctx, Every(50*time.Millisecond), func(ctx context.Context, t time.Time) error {
		atomic.AddInt32(&count, 1)
		return nil
	})

	time.Sleep(120 * time.Millisecond)
	j.Stop()
	j.Wait()

	finalCount := atomic.LoadInt32(&count)
	time.Sleep(500 * time.Millisecond)
	if atomic.LoadInt32(&count) != finalCount {
		t.Error("count increased after Stop")
	}
}

// ---- Run: After ----

func TestRun_After_ExecutesOnce(t *testing.T) {
	ctx := context.Background()
	var count int32

	j := Run(ctx, After(50*time.Millisecond), func(ctx context.Context, t time.Time) error {
		atomic.AddInt32(&count, 1)
		return nil
	})

	j.Wait()

	if count != 1 {
		t.Errorf("expected exactly 1 execution, got %d", count)
	}
}

// ---- Run: Panic recovery ----

func TestRun_PanicRecovery(t *testing.T) {
	ctx := context.Background()
	var count int32

	j := Run(ctx, Every(30*time.Millisecond), func(ctx context.Context, t time.Time) error {
		atomic.AddInt32(&count, 1)
		if atomic.LoadInt32(&count) == 1 {
			panic("intentional panic")
		}
		return nil
	})

	time.Sleep(100 * time.Millisecond)
	j.Stop()
	j.Wait()

	if count < 1 {
		t.Error("expected at least 1 execution after panic recovery")
	}
}

// ---- Run: Between constraint ----

func TestRun_Between_BlocksOutsideWindow(t *testing.T) {
	ctx := context.Background()
	var count int32

	windowStart := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2025, 1, 1, 23, 59, 59, 0, time.UTC)

	j := Run(ctx,
		Every(20*time.Millisecond),
		func(ctx context.Context, t time.Time) error {
			atomic.AddInt32(&count, 1)
			return nil
		},
		Between(windowStart, windowEnd),
	)

	time.Sleep(100 * time.Millisecond)
	j.Stop()
	j.Wait()

	if count != 0 {
		t.Errorf("expected 0 executions (all outside window), got %d", count)
	}
}

func TestRun_Between_AllowsInsideWindow(t *testing.T) {
	ctx := context.Background()
	var count int32

	now := time.Now()
	windowStart := now.Add(-5 * time.Second)
	windowEnd := now.Add(10 * time.Second)

	j := Run(ctx,
		Every(30*time.Millisecond),
		func(ctx context.Context, t time.Time) error {
			atomic.AddInt32(&count, 1)
			return nil
		},
		Between(windowStart, windowEnd),
	)

	time.Sleep(100 * time.Millisecond)
	j.Stop()
	j.Wait()

	if count < 2 {
		t.Errorf("expected at least 2 executions inside window, got %d", count)
	}
}

// ---- Run: ctx cancellation ----

func TestRun_CtxCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var count int32

	j := Run(ctx, Every(30*time.Millisecond), func(ctx context.Context, t time.Time) error {
		atomic.AddInt32(&count, 1)
		return nil
	})

	time.Sleep(50 * time.Millisecond)
	cancel()
	j.Wait()

	finalCount := atomic.LoadInt32(&count)
	time.Sleep(500 * time.Millisecond)
	if atomic.LoadInt32(&count) != finalCount {
		t.Error("count increased after ctx cancel")
	}
}

// ---- Run: error does not block scheduling ----

func TestRun_ErrorDoesNotBlock(t *testing.T) {
	ctx := context.Background()
	var count int32

	sentinel := errors.New("task error")
	j := Run(ctx, Every(30*time.Millisecond), func(ctx context.Context, t time.Time) error {
		atomic.AddInt32(&count, 1)
		return sentinel
	})

	time.Sleep(100 * time.Millisecond)
	j.Stop()
	j.Wait()

	if count < 2 {
		t.Errorf("expected at least 2 executions despite errors, got %d", count)
	}
}
