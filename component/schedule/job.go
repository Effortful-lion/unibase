package schedule

import (
	"context"
	"sync"
	"time"
)

// Job 是 Run 返回的定时任务句柄，用于控制任务生命周期。
type Job struct {
	ctx      context.Context
	stop     context.CancelFunc
	finished sync.WaitGroup
}

// Stop 停止调度。
//
// 调用后 ctx 被取消，当前 timer 到期后不再安排下一轮。
// 如果任务正在执行，会等待其完成后 goroutine 才退出。
// 多次调用幂等。
func (j *Job) Stop() {
	j.stop()
}

// Wait 阻塞直到当前轮次的任务执行完成。
//
// 通常在 Stop 之后调用，用于等待清理。
// 如果任务已自然结束，立即返回。
func (j *Job) Wait() {
	j.finished.Wait()
}

// Run 启动一个定时任务。
//
// trigger 定义触发时机（At / Every / After）。
// jobFunc 是任务函数，每次触发时执行。
// constraints 是可选的执行约束，默认不限制。
//
// 返回 *Job 用于后续 Stop / Wait。
//
// 行为说明：
//   - jobFunc 的 error 仅记录，不影响后续调度
//   - jobFunc panic 会被 recover，记录后继续调度
//   - After 类型只执行一次，执行后 goroutine 退出
//   - ctx 取消与 Stop 等效，两者任一触发都会停止调度
//   - At 和 Every 类型使用 drift correction：period 从锚点时间算起，避免执行时长导致的漂移累积
func Run(ctx context.Context, trigger Trigger, jobFunc JobFunc, constraints ...Constraint) *Job {
	childCtx, cancel := context.WithCancel(ctx)
	j := &Job{
		ctx:  childCtx,
		stop: cancel,
	}

	j.finished.Add(1)
	go j.run(trigger, jobFunc, constraints)

	return j
}

func (j *Job) run(trigger Trigger, jobFunc JobFunc, constraints []Constraint) {
	defer j.finished.Done()

	state := resolveTriggerState(trigger)
	timer := time.NewTimer(state.initialDelay())
	defer timer.Stop()

	var lastFireTime time.Time

	for {
		select {
		case <-j.ctx.Done():
			return
		case <-timer.C:
		}

		triggerTime := time.Now()
		lastFireTime = triggerTime

		if !isAllowed(triggerTime, constraints) {
			d := nextDelay(state, lastFireTime)
			if d == 0 {
				return
			}
			timer.Reset(d)
			continue
		}

		j.finished.Add(1)
		func() {
			defer j.finished.Done()
			defer func() {
				recover() // panic 被 recover，不影响后续调度
			}()
			_ = jobFunc(j.ctx, triggerTime)
		}()

		if trigger.kind == triggerKindAfter {
			return
		}

		d := nextDelay(state, lastFireTime)
		if d == 0 {
			return
		}
		timer.Reset(d)
	}
}
