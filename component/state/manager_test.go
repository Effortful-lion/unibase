package state

import (
	"context"
	"testing"
	"time"
)

// testTask 测试用的任务实体。
type testTask struct {
	id string
}

func (t *testTask) ID() string { return t.id }

// testSubTask 测试用的子任务实体。
type testSubTask struct {
	id       string
	parentID string
	step     string
}

func (t *testSubTask) GetSubTaskId() string    { return t.id }
func (t *testSubTask) GetParentTaskId() string { return t.parentID }

func TestNewManager(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage[*testTask, any]()

	def, err := Define[*testTask, any](func(main MainPathBuilder[*testTask, any]) {
		main.
			AddMain(State("step1"), func(ctx *TaskContext[*testTask, any]) error {
				return nil
			}).
			AddMain(SuccessState, nil)
	})
	if err != nil {
		t.Fatalf("Define failed: %v", err)
	}

	mgr, err := NewManager(ctx, storage, nil, def)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	if mgr == nil {
		t.Fatal("manager is nil")
	}
}

func TestExecute_MainPath(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage[*testTask, any]()

	var executed []string
	def, err := Define[*testTask, any](func(main MainPathBuilder[*testTask, any]) {
		main.
			AddMain(State("step1"), func(taskCtx *TaskContext[*testTask, any]) error {
				executed = append(executed, "step1")
				return nil
			}).
			AddMain(State("step2"), func(taskCtx *TaskContext[*testTask, any]) error {
				executed = append(executed, "step2")
				return nil
			}).
			AddMain(SuccessState, nil)
	})
	if err != nil {
		t.Fatalf("Define failed: %v", err)
	}

	mgr, err := NewManager(ctx, storage, nil, def)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	task := &testTask{id: "task-1"}
	result, err := mgr.Execute(ctx, task)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.FinalState != SuccessState {
		t.Errorf("expected final state %s, got %s", SuccessState, result.FinalState)
	}
	if len(executed) != 2 {
		t.Errorf("expected 2 steps executed, got %d", len(executed))
	}
	if executed[0] != "step1" || executed[1] != "step2" {
		t.Errorf("unexpected execution order: %v", executed)
	}

	// 验证状态已保存
	states := storage.TaskStates("task-1")
	if len(states) < 2 {
		t.Errorf("expected at least 2 states saved, got %v", states)
	}
}

func TestExecute_Failure(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage[*testTask, any]()

	def, err := Define[*testTask, any](func(main MainPathBuilder[*testTask, any]) {
		main.
			AddMain(State("step1"), func(taskCtx *TaskContext[*testTask, any]) error {
				return ErrInvalidTransition
			}).
			AddMain(SuccessState, nil)
	})
	if err != nil {
		t.Fatalf("Define failed: %v", err)
	}

	mgr, err := NewManager(ctx, storage, nil, def)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	task := &testTask{id: "task-fail"}
	result, err := mgr.Execute(ctx, task)
	if err == nil {
		t.Fatal("expected error on failure, got nil")
	}

	if result.FinalState != FailedState {
		t.Errorf("expected final state %s, got %s", FailedState, result.FinalState)
	}
}

func TestResume(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage[*testTask, any]()

	var executed []string
	def, err := Define[*testTask, any](func(main MainPathBuilder[*testTask, any]) {
		main.
			AddMain(State("step1"), func(taskCtx *TaskContext[*testTask, any]) error {
				executed = append(executed, "step1")
				return nil
			}).
			AddMain(State("step2"), func(taskCtx *TaskContext[*testTask, any]) error {
				executed = append(executed, "step2")
				return nil
			}).
			AddMain(SuccessState, nil)
	})
	if err != nil {
		t.Fatalf("Define failed: %v", err)
	}

	mgr, err := NewManager(ctx, storage, nil, def)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// 第一次执行到 step1
	task := &testTask{id: "task-resume"}
	_, _ = mgr.Execute(ctx, task)

	// 设置快照：状态在 step1
	_ = storage.SaveSnapshot(ctx, "task-resume", ExecutionSnapshot{
		State: State("step1"),
	})

	// 清空执行记录，Resume 应该只执行 step2
	executed = nil

	_, err = mgr.Resume(ctx, task)
	if err != nil {
		t.Fatalf("Resume failed: %v", err)
	}

	// Resume 从快照恢复，应该只执行 step2
	if len(executed) != 1 || executed[0] != "step2" {
		t.Errorf("expected only step2 executed, got %v", executed)
	}
}

func TestSubTaskMode(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage[*testTask, any]()

	var subExecuted []string
	def, err := Define[*testTask, any](func(main MainPathBuilder[*testTask, any]) {
		main.
			AddMain(State("preparing"), func(taskCtx *TaskContext[*testTask, any]) error {
				if err := taskCtx.CreateSubTask(0, func(context.Context, any) (SubTask, error) {
					return &testSubTask{id: "sub-1", parentID: taskCtx.GetTask().ID()}, nil
				}); err != nil {
					return err
				}
				return nil
			}).
			AddSub(State("copied"), func(taskCtx *TaskContext[*testTask, any]) error {
				subExecuted = append(subExecuted, taskCtx.CurrentSubTaskRef().SubTaskID)
				return nil
			}).
			AddMain(SuccessState, nil)
	})
	if err != nil {
		t.Fatalf("Define failed: %v", err)
	}

	mgr, err := NewManager(ctx, storage, nil, def,
		WithTaskLoader(TaskLoader[*testTask, any]{
			LoadSubTask: func(_ context.Context, task *testTask, record SubTaskRecord, _ any) (SubTask, error) {
				return &testSubTask{id: record.SubTaskID, parentID: record.ParentTaskID}, nil
			},
		}),
	)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	task := &testTask{id: "task-sub"}
	result, err := mgr.Execute(ctx, task)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.FinalState != SuccessState {
		t.Errorf("expected final state %s, got %s", SuccessState, result.FinalState)
	}
	if len(subExecuted) != 1 {
		t.Errorf("expected 1 sub task executed, got %d", len(subExecuted))
	}
}

func TestConcurrentSameTask(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage[*testTask, any]()

	def, err := Define[*testTask, any](func(main MainPathBuilder[*testTask, any]) {
		main.
			AddMain(State("step1"), func(taskCtx *TaskContext[*testTask, any]) error {
				time.Sleep(100 * time.Millisecond)
				return nil
			}).
			AddMain(SuccessState, nil)
	})
	if err != nil {
		t.Fatalf("Define failed: %v", err)
	}

	mgr, err := NewManager(ctx, storage, nil, def)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	task := &testTask{id: "task-concurrent"}

	// 两个 Execute 并发调用，由于进程内锁，它们会串行执行
	errCh := make(chan error, 2)
	start := time.Now()
	go func() {
		_, err := mgr.Execute(ctx, task)
		errCh <- err
	}()
	go func() {
		_, err := mgr.Execute(ctx, task)
		errCh <- err
	}()

	err1 := <-errCh
	err2 := <-errCh
	elapsed := time.Since(start)

	// 两个都应该成功，但串行执行（每个需要 100ms，总计约 200ms）
	if err1 != nil || err2 != nil {
		t.Errorf("expected both to succeed, got err1=%v, err2=%v", err1, err2)
	}
	if elapsed < 200*time.Millisecond {
		t.Errorf("expected sequential execution (~200ms), got %v", elapsed)
	}
}

func TestDefinition_Validate(t *testing.T) {
	tests := []struct {
		name    string
		build   func(PathBuilder[*testTask, any])
		wantErr bool
	}{
		{
			name: "valid linear path",
			build: func(main MainPathBuilder[*testTask, any]) {
				main.AddMain(State("a"), nil).AddMain(SuccessState, nil)
			},
			wantErr: false,
		},
		{
			name: "empty main path",
			build: func(main MainPathBuilder[*testTask, any]) {
				// no transitions
			},
			wantErr: true,
		},
		{
			name: "not ending with final state",
			build: func(main MainPathBuilder[*testTask, any]) {
				main.AddMain(State("a"), nil)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := NewDefinition[*testTask, any]()
			tt.build(def.Main())
			err := def.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
