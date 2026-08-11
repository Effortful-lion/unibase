package schedule

import "time"

// triggerState 封装某种触发类型的时间计算逻辑。
//
// 每个 Trigger 类型对应一个 triggerState 实现，负责：
//   - initialDelay(ctx)：首次执行前的等待时长
//   - nextTriggerTime(lastFireTime)：从上次触发时间算下次触发时间
type triggerState interface {
	initialDelay() time.Duration
	nextTriggerTime(lastFireTime time.Time) time.Duration
}

// atState 实现定时锚点触发。
//
// 使用 drift correction：从首次锚点起，以固定 period 推进，
// 避免任务执行时长导致的漂移累积。
type atState struct {
	anchor time.Time // 首次触发的绝对时间
	period time.Duration
}

func newAtState(at atTrigger) atState {
	now := time.Now()
	period := 24 * time.Hour // At 固定每天一次

	anchor := time.Date(now.Year(), now.Month(), now.Day(), at.hour, at.minute, 0, 0, now.Location())

	if !anchor.After(now) {
		// anchor <= now，往后推至少一个 period，到达下一个锚点
		elapsed := now.Sub(anchor)
		periods := elapsed / period
		anchor = anchor.Add(periods * period)
		if !anchor.After(now) {
			anchor = anchor.Add(period)
		}
	}

	return atState{anchor: anchor, period: period}
}

func (s atState) initialDelay() time.Duration {
	return time.Until(s.anchor)
}

func (s atState) nextTriggerTime(lastFireTime time.Time) time.Duration {
	next := s.anchor.Add(lastFireTime.Sub(s.anchor) / s.period * s.period)
	if !next.After(lastFireTime) {
		next = next.Add(s.period)
	}
	return time.Until(next)
}

// everyState 实现固定间隔触发。
//
// 使用 drift correction：period 从首次触发时间算起，
// 避免任务执行时长导致的漂移累积。
type everyState struct {
	interval time.Duration
	first    time.Time // 首次触发时间
}

func newEveryState(interval time.Duration, first time.Time) everyState {
	return everyState{interval: interval, first: first}
}

func (s everyState) initialDelay() time.Duration {
	return s.interval
}

func (s everyState) nextTriggerTime(lastFireTime time.Time) time.Duration {
	next := s.first.Add(lastFireTime.Sub(s.first) / s.interval * s.interval)
	if !next.After(lastFireTime) {
		next = next.Add(s.interval)
	}
	return time.Until(next)
}

// dailyEveryState 实现带每日锚点的固定间隔触发。
//
// 首次触发在下一个 HH:MM 锚点，之后按 interval 间隔触发，
// 使用 drift correction 从锚点算起。
type dailyEveryState struct {
	anchor   time.Time // 首个锚点时间（当天的 HH:MM 或下一个 HH:MM）
	interval time.Duration
}

func newDailyEveryState(hour, minute int, interval time.Duration) dailyEveryState {
	now := time.Now()
	anchor := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())

	if !anchor.After(now) {
		// 锚点已过，跳到下一个
		anchor = anchor.Add(interval)
	}

	return dailyEveryState{anchor: anchor, interval: interval}
}

func (s dailyEveryState) initialDelay() time.Duration {
	return time.Until(s.anchor)
}

func (s dailyEveryState) nextTriggerTime(lastFireTime time.Time) time.Duration {
	// 从锚点算，lastFireTime 落在第 N 个周期内，下次是第 N+1 个锚点
	next := s.anchor.Add((lastFireTime.Sub(s.anchor)/s.interval + 1) * s.interval)
	return time.Until(next)
}

// afterState 实现延迟单次触发。
type afterState struct {
	delay time.Duration
}

func newAfterState(delay time.Duration) afterState {
	return afterState{delay: delay}
}

func (s afterState) initialDelay() time.Duration {
	return s.delay
}

func (s afterState) nextTriggerTime(lastFireTime time.Time) time.Duration {
	// 没有下一次触发
	return 0
}

// resolveTriggerState 根据 Trigger 类型创建对应的 triggerState。
func resolveTriggerState(trigger Trigger) triggerState {
	switch trigger.kind {
	case triggerKindAt:
		return newAtState(trigger.at)
	case triggerKindEvery:
		if trigger.every.dailyHour != 0 || trigger.every.dailyMin != 0 {
			return newDailyEveryState(trigger.every.dailyHour, trigger.every.dailyMin, trigger.every.interval)
		}
		return newEveryState(trigger.every.interval, time.Now().Add(trigger.every.interval))
	case triggerKindAfter:
		return newAfterState(trigger.after.delay)
	default:
		return newAfterState(0)
	}
}

// nextDelay 从上次触发时间计算下一次等待时长。
// 返回 0 表示没有下一次触发。
func nextDelay(state triggerState, lastFireTime time.Time) time.Duration {
	d := state.nextTriggerTime(lastFireTime)
	if d <= 0 {
		return 0
	}
	return d
}

// isAllowed 检查当前时间是否满足所有约束。
func isAllowed(now time.Time, constraints []Constraint) bool {
	for _, c := range constraints {
		if !c.check(now) {
			return false
		}
	}
	return true
}

func (c Constraint) check(now time.Time) bool {
	switch c.kind {
	case constraintKindBetween:
		b := c.between
		// 比较日期+时间，支持跨日/跨月约束
		return !now.Before(b.start) && !now.After(b.end)
	case constraintKindDailyHour:
		h := c.dailyHour
		hour := now.Hour()
		if h.startHour < h.endHour {
			// 同日窗口：[startHour, endHour)
			return hour >= h.startHour && hour < h.endHour
		}
		// 跨天窗口：从 startHour 到 23，再从 0 到 endHour
		return hour >= h.startHour || hour < h.endHour
	}
	return true
}
