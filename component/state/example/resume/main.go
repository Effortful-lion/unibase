package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Effortful-lion/unibase/component/state"
)

// TransferTask 转账任务实体。
type TransferTask struct {
	taskID      string
	fromAccount string
	toAccount   string
	amount      int64
}

func (t *TransferTask) ID() string { return t.taskID }

// 状态常量。
var (
	CreatedState = state.State("created")
	DebitState   = state.State("debit")
	CreditState  = state.State("credit")
	NotifyState  = state.State("notified")
)

// ---- 回调函数 ----

// onDebit 扣款。
func onDebit(ctx *state.TaskContext[*TransferTask, any]) error {
	task := ctx.GetTask()
	log.Printf("[转账 %s] 从 %s 扣款 %d", task.ID(), task.fromAccount, task.amount)
	return nil
}

// onCredit 入账。
func onCredit(ctx *state.TaskContext[*TransferTask, any]) error {
	task := ctx.GetTask()
	log.Printf("[转账 %s] 向 %s 入账 %d", task.ID(), task.toAccount, task.amount)
	return nil
}

// onNotify 发送通知。
func onNotify(ctx *state.TaskContext[*TransferTask, any]) error {
	task := ctx.GetTask()
	log.Printf("[转账 %s] 发送通知给 %s", task.ID(), task.toAccount)
	return nil
}

func main() {
	log.SetFlags(0)
	log.SetOutput(os.Stdout)
	log.Printf("==== 示例 3：断点续执行（Resume）====")
	log.Printf("场景：转账流程 created → debit → credit → notified → success")
	log.Printf("模拟：进程在 debit 完成后、credit 执行前崩溃")
	log.Println()

	ctx := context.Background()
	storage := state.NewMemoryStorage[*TransferTask, any]()

	// 定义状态机：
	//   created → debit → credit → notified → success
	def, err := state.Define(func(main state.MainPathBuilder[*TransferTask, any]) {
		main.
			AddMain(DebitState, onDebit).
			AddMain(CreditState, onCredit).
			AddMain(NotifyState, onNotify).
			AddMain(state.SuccessState, nil)
	})
	if err != nil {
		log.Fatalf("定义失败: %v", err)
	}

	mgr, err := state.NewManager(ctx, storage, nil, def,
		state.WithDescription("转账流水线"),
		state.WithRetryPolicy(state.RetryPolicy{
			MaxAttempts:     2,
			InitialInterval: 200 * time.Millisecond,
		}),
	)
	if err != nil {
		log.Fatalf("创建管理器失败: %v", err)
	}

	// ---- 第一步：正常执行到 debit ----
	log.Println(">>> 第一次执行：模拟正常跑完 debit，然后进程崩溃")
	task := &TransferTask{
		taskID:      "TXN-001",
		fromAccount: "ACC-A",
		toAccount:   "ACC-B",
		amount:      50000,
	}

	_, _ = mgr.Execute(ctx, task)

	// 手动构造快照：状态停在 CreditState（模拟 debit 完成后、credit 执行前崩溃）
	// 注意：真实场景中快照由存储层自动保存，这里为了演示手动设置
	_ = storage.SaveSnapshot(ctx, task.ID(), state.ExecutionSnapshot{
		State: CreditState,
	})

	// ---- 第二步： Resume 恢复 ----
	log.Println()
	log.Println(">>> 进程重启，从快照恢复执行")
	log.Printf(">>> 快照记录的状态: %s，将从这里继续往后执行", CreditState)
	log.Println()

	_, err = mgr.Resume(ctx, task)
	if err != nil {
		log.Fatalf("恢复执行失败: %v", err)
	}

	// ---- 结果 ----
	fmt.Println()
	fmt.Printf("==== 执行结果 ====\n")
	fmt.Printf("任务ID:   %s\n", task.ID())
	fmt.Printf("最终状态: success\n")
	fmt.Printf("状态轨迹: %v\n", storage.TaskStates(task.ID()))
	fmt.Printf("说明：Resume 从 CreditState 继续，只执行了 credit → notified → success\n")
	fmt.Printf("      debit 不会重复执行，因为快照已经记录状态在 CreditState\n")
}
