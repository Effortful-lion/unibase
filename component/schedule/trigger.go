package schedule

import (
	"context"
	"time"
)

// JobFunc 是定时任务执行函数。
//
// triggerTime 是本次触发的实际时间点（即 time.Now() 的采样值）。
// 返回的 error 仅用于日志记录，不影响后续调度。
type JobFunc func(ctx context.Context, triggerTime time.Time) error

// Trigger 定义任务在何时触发。
//
// 三种触发类型语义正交，由构造函数创建，不是接口。
type Trigger struct {
	kind  triggerKind
	at    atTrigger
	every everyTrigger
	after afterTrigger
}

type triggerKind int

const (
	triggerKindAt    triggerKind = iota // 定时锚点：在指定的 wall-clock 时间点触发
	triggerKindEvery                    // 固定间隔：每隔固定时长触发一次
	triggerKindAfter                    // 延迟单次：等固定时长后触发一次
)

type atTrigger struct {
	hour, minute int
}

type everyTrigger struct {
	interval  time.Duration // 两次触发之间的固定间隔
	dailyHour int           // 每日锚点小时，0 表示未设置
	dailyMin  int           // 每日锚点分钟
}

type afterTrigger struct {
	delay time.Duration // 首次触发前的等待时长
}

// At 创建一个定时锚点触发。
//
// hour 和 minute 指定 wall-clock 时间点（如 3, 0 表示凌晨 3:00）。
// 每天在该时间点触发一次。
//
//	At(3, 0) → 每天 3:00
//	At(0, 0) → 每天 0:00
func At(hour, minute int) Trigger {
	return Trigger{kind: triggerKindAt, at: atTrigger{hour: hour, minute: minute}}
}

// Every 创建一个固定间隔触发。
//
// interval 是两次触发之间的固定时长，支持任意 time.Duration 单位。
//
//	Every(30*time.Second) → 每 30 秒
//	Every(5*time.Minute)  → 每 5 分钟
//	Every(2*time.Hour)    → 每 2 小时
//
// 可选搭配 DailyAt 设置每日锚点，从指定时间开始按间隔触发：
//
//	Every(2*time.Hour).DailyAt(3, 0) → 每天 3:00、5:00、7:00...
func Every(interval time.Duration) Trigger {
	return Trigger{kind: triggerKindEvery, every: everyTrigger{interval: interval}}
}

// DailyAt 设置 Every 的每日锚点。
//
// hour 和 minute 指定每天开始触发的时间点。
// 首次触发在当天的 HH:MM（或下一个 HH:MM），之后按 interval 间隔触发。
// 返回修改后的 Trigger，可以链式调用。
//
//	Every(2*time.Hour).DailyAt(3, 0)
//	Every(30*time.Minute).DailyAt(9, 0)
func (t Trigger) DailyAt(hour, minute int) Trigger {
	if t.kind != triggerKindEvery {
		return t
	}
	t.every.dailyHour = hour
	t.every.dailyMin = minute
	return t
}

// After 创建一个延迟单次触发。
//
// delay 是首次触发前的等待时长，任务只执行一次。
//
//	After(10*time.Second) → 10 秒后执行一次
func After(delay time.Duration) Trigger {
	return Trigger{kind: triggerKindAfter, after: afterTrigger{delay: delay}}
}

// Constraint 定义任务执行的时间窗口约束。
//
// 约束附加在 Trigger 上，仅在窗口内才实际执行任务。
// 约束不满足时跳过本次，按正常周期等待下一触发时机。
type Constraint struct {
	kind      constraintKind
	between   betweenConstraint
	dailyHour dailyHourConstraint
}

type constraintKind int

const (
	constraintKindBetween   constraintKind = iota // 仅在 [start, end] 时间窗口内执行
	constraintKindDailyHour                       // 仅在每天的 [startHour, endHour) 时段内执行
)

type betweenConstraint struct {
	start, end time.Time
}

type dailyHourConstraint struct {
	startHour, endHour int // 0-23，闭区间 [startHour, endHour)
}

// Between 创建一个时间窗口约束。
//
// start 和 end 定义了任务允许执行的时间范围（闭区间）。
// 每次触发时判断当前时间是否在 [start, end] 内：
//   - 在窗口内：正常执行
//   - 不在窗口内：跳过，按正常周期等待
//
// start 和 end 的日期部分也参与比较，因此可以实现：
//   - 日内窗口：Between(当天 9:00, 当天 18:00)
//   - 跨日窗口：Between(周一 00:00, 周五 23:59)
//   - 月内窗口：Between(1号 00:00, 1号 23:59)
func Between(start, end time.Time) Constraint {
	return Constraint{
		kind:    constraintKindBetween,
		between: betweenConstraint{start: start, end: end},
	}
}

// BetweenDaily 创建一个按小时范围每日重复的约束。
//
// startHour 和 endHour 定义每天允许执行的小时范围。
//
// 两种语义模式：
//
//   - startHour < endHour：同日窗口 [startHour, endHour)
//     BetweenDaily(9, 18) → 每天 9:00-17:59
//     BetweenDaily(0, 24) → 全天允许
//
//   - startHour >= endHour：跨天窗口，从 startHour 到午夜，再从午夜到 endHour
//     BetweenDaily(22, 6) → 每天 22:00-23:59 和 00:00-06:00
//     BetweenDaily(23, 1) → 每天 23:00-00:59
//
// 每次触发时判断当前时间的小时数是否在范围内：
//   - 在窗口内：正常执行
//   - 不在窗口内：跳过，按正常周期等待
func BetweenDaily(startHour, endHour int) Constraint {
	return Constraint{
		kind:      constraintKindDailyHour,
		dailyHour: dailyHourConstraint{startHour: startHour, endHour: endHour},
	}
}
