// Package delayqueue 提供基于 Redis ZSet 的延迟队列。
//
// 核心思路：将消息的投递时间作为 ZSet 的 score，
// 通过 ZRANGEBYSCORE 获取到期的消息，由消费者循环拉取处理。
//
// 适用场景：延迟关闭、定时提醒、订单超时取消、重试调度等。
//
// 快速开始：
//
//	q := delayqueue.New(rdb, "order:delay:queue")
//
//	// 投递消息，30 秒后可被取出
//	id, err := q.Add(ctx, "order-123", 30*time.Second)
//
//	// 消费循环：最多取 10 条，阻塞最多 1 秒
//	msgs, err := q.Poll(ctx, 10, time.Second)
//	for _, m := range msgs {
//	    fmt.Println("处理:", m)
//	    q.Ack(ctx, m)
//	}
//
// 能力：Add（延迟投递）、Poll（拉取到期消息）、Ack（确认处理）、Len（队列长度）。
package delayqueue
