package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Effortful-lion/unibase/component/state"
)

// OrderTask 订单任务实体。
type OrderTask struct {
	taskID string
	items  []Item
}

func (t *OrderTask) ID() string { return t.taskID }

type Item struct {
	SKU   string
	Count int
}

// PackItem 打包子任务。
type PackItem struct {
	subTaskID string
	parentID  string
	sku       string
}

func (t *PackItem) GetSubTaskId() string    { return t.subTaskID }
func (t *PackItem) GetParentTaskId() string { return t.parentID }

// 状态常量。
var (
	CreatedState = state.State("created")
	PaidState    = state.State("paid")
	PackingState = state.State("packing")
	ShippedState = state.State("shipped")
)

// ---- 主任务回调 ----

// onPay 处理支付。
func onPay(ctx *state.TaskContext[*OrderTask, any]) error {
	task := ctx.GetTask()
	log.Printf("[订单 %s] 支付成功，共 %d 个商品", task.ID(), len(task.items))
	return nil
}

// onShip 发货。
func onShip(ctx *state.TaskContext[*OrderTask, any]) error {
	task := ctx.GetTask()
	log.Printf("[订单 %s] 已发货", task.ID())
	return nil
}

// onLeavePackingFail 打包阶段失败后的收尾。
func onLeavePackingFail(ctx *state.TaskContext[*OrderTask, any]) error {
	task := ctx.GetTask()
	log.Printf("[订单 %s] 打包阶段失败，部分商品未打包完成", task.ID())
	return nil
}

// ---- 子任务回调 ----

// onPackItem 打包单个商品。
func onPackItem(ctx *state.TaskContext[*OrderTask, any]) error {
	sub := ctx.CurrentSubTask().(*PackItem)
	log.Printf("  [子任务 %s] 打包商品 %s", sub.subTaskID, sub.sku)
	return nil
}

// onPackItemFail 打包子任务失败后的收尾。
func onPackItemFail(ctx *state.TaskContext[*OrderTask, any]) error {
	sub := ctx.CurrentSubTask().(*PackItem)
	log.Printf("  [子任务 %s] 打包失败: %s", sub.subTaskID, sub.sku)
	return nil
}

func main() {
	log.SetFlags(0)
	log.SetOutput(os.Stdout)
	log.Printf("==== 示例 2：主流程 + 子流程 + AddFail + 重试 ====")

	ctx := context.Background()
	storage := state.NewMemoryStorage[*OrderTask, any]()

	// 定义状态机：
	//   created(隐式) → paid → packing → shipped → success
	//   packing 主状态下挂子任务流程：每个商品一个打包子任务
	//
	//   AddFail(ShippedState, ...) 会在 AddMain(ShippedState, ...) 之后注册，
	//   实际处理的是 PackingState → ShippedState 的失败（如果打包阶段有问题）
	def, err := state.Define(func(main state.MainPathBuilder[*OrderTask, any]) {
		main.
			AddMain(PaidState, onPay). // 创建 → 支付
			AddMain(PackingState, func(ctx *state.TaskContext[*OrderTask, any]) error {
				// 打包阶段：为每个商品生成子任务
				task := ctx.GetTask()
				for i, item := range task.items {
					if err := ctx.CreateSubTask(i, func(context.Context, any) (state.SubTask, error) {
						return &PackItem{
							subTaskID: fmt.Sprintf("%s-pack-%d", task.ID(), i),
							parentID:  task.ID(),
							sku:       item.SKU,
						}, nil
					}); err != nil {
						return err
					}
				}
				return nil
			}).
			AddSub(state.State("packed"), onPackItem). // 子任务流程：打包每个商品
			AddFail(onPackItemFail).                   // 注册子任务失败回调
			AddMain(ShippedState, onShip).             // 打包完成 → 发货
			AddMain(state.SuccessState, nil)           // 发货 → 成功
	})
	if err != nil {
		log.Fatalf("定义失败: %v", err)
	}

	mgr, err := state.NewManager(ctx, storage, nil, def,
		state.WithDescription("订单主任务+子任务流程"),
		// 子任务失败时跳过继续，不阻断整单
		state.WithSubTaskFailStrategy(state.SubTaskFailContinue),
		// 重试：最多 2 次
		state.WithRetryPolicy(state.RetryPolicy{
			MaxAttempts:     2,
			InitialInterval: 300 * time.Millisecond,
		}),
		// 子任务加载器：恢复执行时根据记录重建子任务对象
		state.WithTaskLoader(state.TaskLoader[*OrderTask, any]{
			LoadSubTask: func(_ context.Context, task *OrderTask, record state.SubTaskRecord, _ any) (state.SubTask, error) {
				return &PackItem{
					subTaskID: record.SubTaskID,
					parentID:  record.ParentTaskID,
					sku:       fmt.Sprintf("sku-%s", record.SubTaskID),
				}, nil
			},
		}),
	)
	if err != nil {
		log.Fatalf("创建管理器失败: %v", err)
	}

	task := &OrderTask{
		taskID: "ORD-002",
		items: []Item{
			{SKU: "A100", Count: 2},
			{SKU: "B200", Count: 1},
		},
	}

	result, err := mgr.Execute(ctx, task)
	if err != nil {
		log.Fatalf("执行失败: %v", err)
	}

	fmt.Println()
	fmt.Printf("==== 执行结果 ====\n")
	fmt.Printf("任务ID:   %s\n", task.ID())
	fmt.Printf("最终状态: %s\n", result.FinalState)
	fmt.Printf("子任务数量: %d\n", len(result.SubTasks))
	fmt.Printf("状态轨迹: %v\n", storage.TaskStates(task.ID()))
}
