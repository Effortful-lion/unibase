// Package state 提供单进程状态机引擎，适合订单、工单、票务等流水线场景。
//
// 核心能力：
//   - 声明式 DSL 定义状态流转路径
//   - 主任务 + 子任务两级流程
//   - 快照恢复（断点续跑）
//   - 内置内存存储，零配置即可使用
//   - 可选分布式锁（lockdog），多进程部署时防止任务并发执行
//
// # 执行模型（使用前必读）
//
// 本库是“单进程执行 + 锁防重”模型，不是分布式工作流调度器：
//
//   - 任务由调用 Execute / Resume 的当前 goroutine 同步、顺序执行到底，
//     库内部不启动后台调度器、不跨进程派发任务。
//   - 进程内通过每 taskID 一把互斥锁保证同一任务串行执行；多实例部署时
//     通过 WithLocker（lockdog）防止同一任务被多个进程并发执行，但任务仍
//     会“随机落到某个持有锁的进程”上完整跑完。
//   - 子任务在同一调用内同步串行执行（非 worker 池并发），数量很大时
//     会阻塞当前 goroutine。
//   - 状态只能向前推进，失败仅记录终态，不支持补偿 / 回滚（Saga）。
//   - 快照恢复依赖调用方在合适时机重新调用 Resume 来唤醒（例如等待外部
//     回调后），库不内置“暂停后自动唤醒”的调度能力。
//
// # 适用场景
//
//   - 单服务内的订单 / 工单 / 审批流水线，步骤明确、可声明
//   - 需要断点续跑 + 子任务扇出，且子任务规模不大、可同步执行
//   - 多实例部署但能接受“锁防重 + 单实例执行”模型
//   - 内部工具 / 个人项目 / 中小业务后台
//
// # 不适用场景（请改用专业工作流引擎）
//
//   - 需要跨服务补偿 / Saga / 事务回滚（资金、库存等）
//   - 海量子任务需要并发 worker 池消费
//   - 长周期、含人工介入（human-in-the-loop）的任务编排，需要自动唤醒
//   - 对状态存储有强一致 / 不可丢失的审计要求（需自行接入数据库存储）
//   - 要求完整可观测告警闭环（失败自动进死信队列、超时告警等）
//
// 快速开始：
//
//	// 1. 定义任务实体
//	type OrderTask struct {
//	    taskID string
//	    Amount int64
//	}
//	func (t *OrderTask) ID() string { return t.taskID }
//
//	// 2. 定义状态机流程
//	def, err := state.Define(func(main state.MainPathBuilder[*OrderTask, *state.MemoryStorage[*OrderTask]]) {
//	    main.
//	        AddMain(state.Paid, func(ctx *state.TaskContext[*OrderTask, *state.MemoryStorage[*OrderTask]]) error {
//	            task := ctx.GetTask()
//	            fmt.Printf("订单 %s 完成支付，金额 %d\n", task.ID(), task.Amount)
//	            return nil
//	        }).
//	        AddMain(state.Shipped, func(ctx *state.TaskContext[*OrderTask, *state.MemoryStorage[*OrderTask]]) error {
//	            fmt.Printf("订单 %s 已发货\n", ctx.GetTask().ID())
//	            return nil
//	        }).
//	        AddMain(state.Success, nil)
//	})
//
//	// 3. 创建内存存储并执行
//	storage := state.NewMemoryStorage[*OrderTask, *state.MemoryStorage[*OrderTask]]()
//	mgr, err := state.NewManager(ctx, storage, nil, def)
//	result, err := mgr.Execute(ctx, &OrderTask{taskID: "order-1", Amount: 19900})
//	fmt.Printf("最终状态: %s\n", result.FinalState)
package state
