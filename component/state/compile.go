package state

import (
	"fmt"

	"github.com/Effortful-lion/unibase/logx"
)

// compiledDefinition 表示定义对象在运行前编译出来的只读结果。
type compiledDefinition[D Task, S any] struct {
	view            DefinitionView
	mainPath        []State
	mainTransitions map[State]map[State]TransitionCallback[D, S]
	mainCancel      map[State]TransitionCallback[D, S]
	mainFail        map[State]TransitionCallback[D, S]
	subFlows        map[State]*compiledSubFlow[D, S]
}

// compiledSubFlow 表示某个子任务流程入口编译后的只读结果。
type compiledSubFlow[D Task, S any] struct {
	entry       State
	generator   TransitionCallback[D, S]
	transitions map[State]map[State]TransitionCallback[D, S]
	cancel      map[State]TransitionCallback[D, S]
	fail        map[State]TransitionCallback[D, S]
	path        []State
}

// compileDefinition 把定义编译成运行时结构。
func compileDefinition[D Task, S any](def *Definition[D, S]) (*compiledDefinition[D, S], error) {
	if def == nil {
		return nil, &DefinitionError{Msg: "nil definition"}
	}
	if err := def.Validate(); err != nil {
		return nil, err
	}
	return compileCore(def.core), nil
}

// compileCore 是定义编译的公共主逻辑。
func compileCore[D Task, S any](core *definitionCore[D, S]) *compiledDefinition[D, S] {
	compiled := &compiledDefinition[D, S]{
		view:            buildView(core),
		mainPath:        append([]State(nil), core.mainPath...),
		mainTransitions: make(map[State]map[State]TransitionCallback[D, S], len(core.mainTransitions)),
		mainCancel:      make(map[State]TransitionCallback[D, S], len(core.mainCancel)),
		mainFail:        make(map[State]TransitionCallback[D, S], len(core.mainFail)),
		subFlows:        make(map[State]*compiledSubFlow[D, S], len(core.subFlows)),
	}

	for from, targets := range core.mainTransitions {
		compiled.mainTransitions[from] = make(map[State]TransitionCallback[D, S], len(targets))
		for to, transition := range targets {
			compiled.mainTransitions[from][to] = wrapTransition(
				transition.callback,
				compileMiddlewares(core.globalMW, transition.middlewares)...,
			)
		}
	}
	for from, transition := range core.mainCancel {
		compiled.mainCancel[from] = wrapTransition(
			transition.callback,
			compileMiddlewares(core.globalMW, transition.middlewares)...,
		)
	}
	for from, transition := range core.mainFail {
		compiled.mainFail[from] = wrapTransition(
			transition.callback,
			compileMiddlewares(core.globalMW, transition.middlewares)...,
		)
	}

	for entry, flow := range core.subFlows {
		compiledFlow := &compiledSubFlow[D, S]{
			entry:       entry,
			generator:   wrapTransition(flow.generator.callback, compileMiddlewares(core.globalMW, flow.generator.middlewares)...),
			transitions: make(map[State]map[State]TransitionCallback[D, S], len(flow.transitions)),
			cancel:      make(map[State]TransitionCallback[D, S], len(flow.cancel)),
			fail:        make(map[State]TransitionCallback[D, S], len(flow.fail)),
			path:        append([]State(nil), flow.path...),
		}
		for from, targets := range flow.transitions {
			compiledFlow.transitions[from] = make(map[State]TransitionCallback[D, S], len(targets))
			for to, transition := range targets {
				compiledFlow.transitions[from][to] = wrapTransition(
					transition.callback,
					compileMiddlewares(core.globalMW, transition.middlewares)...,
				)
			}
		}
		for from, transition := range flow.cancel {
			compiledFlow.cancel[from] = wrapTransition(
				transition.callback,
				compileMiddlewares(core.globalMW, transition.middlewares)...,
			)
		}
		for from, transition := range flow.fail {
			compiledFlow.fail[from] = wrapTransition(
				transition.callback,
				compileMiddlewares(core.globalMW, transition.middlewares)...,
			)
		}
		compiled.subFlows[entry] = compiledFlow
	}

	return compiled
}

// wrapTransition 按"内层回调 + 外层中间件"的顺序组装出最终执行函数。
func wrapTransition[D Task, S any](callback TransitionCallback[D, S], middlewares ...TransitionCallbackMiddleware[D, S]) TransitionCallback[D, S] {
	if callback == nil {
		callback = func(*TaskContext[D, S]) error { return nil }
	}
	for i := len(middlewares) - 1; i >= 0; i-- {
		callback = middlewares[i](callback)
	}
	return callback
}

// compileMiddlewares 统一整理一条转换最终要套用的中间件列表。
func compileMiddlewares[D Task, S any](global []TransitionCallbackMiddleware[D, S], local []TransitionCallbackMiddleware[D, S]) []TransitionCallbackMiddleware[D, S] {
	compiled := make([]TransitionCallbackMiddleware[D, S], 0, 1+len(global)+len(local))
	compiled = append(compiled, recoverMiddleware[D, S])
	compiled = append(compiled, global...)
	compiled = append(compiled, local...)
	return compiled
}

// recoverMiddleware 包装回调，在 panic 时恢复并记录日志。
func recoverMiddleware[D Task, S any](next TransitionCallback[D, S]) TransitionCallback[D, S] {
	return func(ctx *TaskContext[D, S]) (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = &StateError{message: fmt.Sprintf("panic recovered: %v", r)}
				logger := ctx.logger
				if logger == nil {
					logger = logx.Module("state")
				}
				logger.Error("panic recovered",
					logx.Fields{"task_id": ctx.GetTask().ID(), "err": err})
			}
		}()
		return next(ctx)
	}
}
