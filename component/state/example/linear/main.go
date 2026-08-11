package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Effortful-lion/unibase/component/state"
)

// OrderTask 订单任务实体。
type OrderTask struct {
	taskID  string
	orderNo string
	amount  int64
}

func (t *OrderTask) ID() string { return t.taskID }

// 状态常量。
var (
	CreatedState = state.State("created")
	PaidState    = state.State("paid")
	ShippedState = state.State("shipped")
)

// ---- 回调函数（先定义，再传给 DSL）----

// onPay 处理支付逻辑。
func onPay(ctx *state.TaskContext[*OrderTask, any]) error {
	task := ctx.GetTask()
	log.Printf("[订单 %s] 处理支付，金额: %d，单号: %s", task.ID(), task.amount, task.orderNo)
	return nil
}

// onShip 发货逻辑。演示用：总是失败，触发重试和 AddFail。
func onShip(ctx *state.TaskContext[*OrderTask, any]) error {
	task := ctx.GetTask()
	log.Printf("[订单 %s] 尝试发货...", task.ID())
	return errors.New("shipping system under maintenance")
}

// onShipFail 发货失败后的收尾回调。
// 当 onShip 重试 3 次仍然失败后，框架会调用此函数。
func onShipFail(ctx *state.TaskContext[*OrderTask, any]) error {
	task := ctx.GetTask()
	log.Printf("[订单 %s] 发货最终失败，已重试 3 次，转人工处理", task.ID())
	return nil
}

func main() {
	log.SetFlags(0)
	log.SetOutput(os.Stdout)
	log.Printf("==== 示例 1：线性主流程 + AddFail 失败回调 + 重试 ====")

	ctx := context.Background()
	storage := state.NewMemoryStorage[*OrderTask, any]()

	// 定义状态机：
	//   created(隐式) → paid → shipped → success
	//   AddFail 注册在 PaidState 上：如果 Paid→Shipped 转换失败，调用 onShipFail
	def, err := state.Define(func(main state.MainPathBuilder[*OrderTask, any]) {
		main.
			AddMain(PaidState, onPay).       // 创建 → 支付：onPay 处理支付
			AddFail(onShipFail).             // 注册 PaidState 的失败回调
			AddMain(ShippedState, onShip).   // 支付 → 发货：onShip 总是失败
			AddMain(state.SuccessState, nil) // 发货 → 成功
	})
	if err != nil {
		log.Fatalf("定义失败: %v", err)
	}

	mgr, err := state.NewManager(ctx, storage, nil, def,
		state.WithDescription("订单线性流程"),
		// 全局重试：最多 3 次，每次间隔 500ms
		state.WithRetryPolicy(state.RetryPolicy{
			MaxAttempts:     3,
			InitialInterval: 500 * time.Millisecond,
		}),
	)
	if err != nil {
		log.Fatalf("创建管理器失败: %v", err)
	}

	// 执行高价值订单（会触发发货失败）
	task := &OrderTask{
		taskID:  "ORD-001",
		orderNo: "20240812-001",
		amount:  29900,
	}

	result, err := mgr.Execute(ctx, task)
	if err != nil {
		log.Fatalf("执行失败: %v", err)
	}

	fmt.Println()
	fmt.Printf("==== 执行结果 ====\n")
	fmt.Printf("任务ID:   %s\n", task.ID())
	fmt.Printf("最终状态: %s\n", result.FinalState)
	fmt.Printf("状态轨迹: %v\n", storage.TaskStates(task.ID()))
	fmt.Printf("说明：onShip 总是失败，重试 3 次后触发 AddFail 回调 onShipFail，流程终止于 FailedState\n")
}
