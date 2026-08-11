// Package schedule 提供基于标准库的定时任务触发能力。
//
// 核心能力：
//   - 三种触发类型：At（定时锚点）、Every（固定间隔）、After（延迟单次）
//   - 时间窗口约束：Between（固定日期范围）、BetweenDaily（每日小时范围）
//   - 生命周期控制：Stop / Wait，panic 保护，drift correction
//
// 零外部依赖，仅使用标准库。
//
// 快速开始：
//
//	// 每 30 秒执行一次
//	j := schedule.Run(ctx, schedule.Every(30*time.Second), func(ctx context.Context, t time.Time) error {
//	    fmt.Println("tick", t)
//	    return nil
//	})
//
//	// 每天 3:00 执行
//	j := schedule.Run(ctx, schedule.At(3, 0), func(ctx context.Context, t time.Time) error {
//	    cleanUp()
//	    return nil
//	})
//
//	// 从 3:00 开始，每 2 小时执行一次：3:00、5:00、7:00...
//	j := schedule.Run(ctx, schedule.Every(2*time.Hour).DailyAt(3, 0), doWork)
//
//	// 每 2 小时执行一次（无锚点，从启动时刻开始）
//	j := schedule.Run(ctx, schedule.Every(2*time.Hour), doWork)
//
//	// 每 30 秒，但仅在每天 9:00-18:00 之间执行
//	j := schedule.Run(ctx,
//	    schedule.Every(30*time.Second),
//	    doWork,
//	    schedule.BetweenDaily(9, 18),
//	)
//
//	// 每 30 秒，但仅在每天 22:00-次日 06:00 之间执行
//	j := schedule.Run(ctx,
//	    schedule.Every(30*time.Second),
//	    doWork,
//	    schedule.BetweenDaily(22, 6),
//	)
//
//	// 延迟 10 秒执行一次
//	j := schedule.Run(ctx, schedule.After(10*time.Second), sendNotification)
//
//	// 停止
//	j.Stop()
//	j.Wait()
package schedule
