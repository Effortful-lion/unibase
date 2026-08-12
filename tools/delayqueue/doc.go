// Package delayqueue 提供生产级 Redis 延迟队列。
//
// 基于 Redis ZSet 实现，具备以下生产级能力：
//
//   - 原子 Poll：Lua 脚本 + ZPOPMINBYSCORE 保证原子拉取，多消费者不重复抢消息
//   - 可见性超时：Poll 后消息进入 In-Flight，超时未 Ack 由 Sweeper 自动重试
//   - NACK + 重试 + DLQ：消费失败按退避重试，超限移入死信队列
//   - 兜底 Sweeper：定期回收超时 In-Flight 消息，防止消费者宕机导致消息卡死
//   - 优雅关闭：Stop 等待 In-Flight 处理完毕再退出
//   - Consumer 模式：Start(ctx, consumer) 一行启动消费循环
//
// 要求 Redis >= 6.2（使用 ZPOPMINBYSCORE 原子弹出）。
//
// 快速开始：
//
//	q := delayqueue.New(rdb, "order:delay",
//	    delayqueue.WithMaxRetries(3),
//	    delayqueue.WithVisibilityTimeout(30*time.Second),
//	)
//	q.Start(ctx, &handler{})
//
// 能力：New / Add / AddAt / Poll / Ack / Nack / Start / Stop / DLQMessages / DLQPop / Stats / Keys。
package delayqueue
