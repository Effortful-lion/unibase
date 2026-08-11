package state

// transitionDef 保存一条状态切换上的回调和它专属的中间件。
type transitionDef[D Task, S any] struct {
	callback    TransitionCallback[D, S]
	middlewares []TransitionCallbackMiddleware[D, S]
}

// subFlowDef 保存某个主状态下面挂载的一整段子任务流程定义。
type subFlowDef[D Task, S any] struct {
	entry             State
	generator         transitionDef[D, S]
	path              []State
	closed            bool
	invalidFinalState bool
	cancel            map[State]transitionDef[D, S]
	fail              map[State]transitionDef[D, S]
	transitions       map[State]map[State]transitionDef[D, S]
}

// definitionCore 是流程定义在内存中的真实数据结构。
type definitionCore[D Task, S any] struct {
	mainPath        []State
	mainCancel      map[State]transitionDef[D, S]
	mainFail        map[State]transitionDef[D, S]
	mainTransitions map[State]map[State]transitionDef[D, S]
	subFlows        map[State]*subFlowDef[D, S]
	globalMW        []TransitionCallbackMiddleware[D, S]
	errs            []error
}

// Definition 表示完整流程定义。
//
// 不挂子任务时，它就是一条普通主任务路径；调用 AddSub(...) 后，当前主状态下面会挂上一段子任务路径。
type Definition[D Task, S any] struct {
	core *definitionCore[D, S]
}

func newDefinitionCore[D Task, S any]() *definitionCore[D, S] {
	return &definitionCore[D, S]{
		mainPath:        []State{CreatedState},
		mainCancel:      make(map[State]transitionDef[D, S]),
		mainFail:        make(map[State]transitionDef[D, S]),
		mainTransitions: make(map[State]map[State]transitionDef[D, S]),
		subFlows:        make(map[State]*subFlowDef[D, S]),
	}
}

// addErr 追加定义阶段发现的结构错误，统一在 Validate 时返回。
func (c *definitionCore[D, S]) addErr(err error) {
	if err == nil {
		return
	}
	c.errs = append(c.errs, err)
}

// NewDefinition 创建一个从隐式 created 状态开始的流程定义。
func NewDefinition[D Task, S any]() *Definition[D, S] {
	return &Definition[D, S]{core: newDefinitionCore[D, S]()}
}

// Define 在一次调用中构建并校验流程定义。
func Define[D Task, S any](build func(PathBuilder[D, S])) (*Definition[D, S], error) {
	def := NewDefinition[D, S]()
	build(def.Main())
	if err := def.Validate(); err != nil {
		return nil, err
	}
	return def, nil
}

// Main 返回当前主任务流程末尾位置的构建器。
func (d *Definition[D, S]) Main() PathBuilder[D, S] {
	curr := d.core.mainPath[len(d.core.mainPath)-1]
	prev := State("")
	if len(d.core.mainPath) > 1 {
		prev = d.core.mainPath[len(d.core.mainPath)-2]
	}
	return &pathBuilder[D, S]{core: d.core, mode: builderModeMain, prevMain: prev, currMain: curr}
}

// Use 注册全局中间件，它们会作用于定义中编译出的每个转换。
func (d *Definition[D, S]) Use(mws ...TransitionCallbackMiddleware[D, S]) {
	d.core.globalMW = append(d.core.globalMW, mws...)
}

// Validate 校验定义在结构上是否合法。
func (d *Definition[D, S]) Validate() error {
	return validateCore(d.core)
}

// View 返回定义的只读拓扑结果。
func (d *Definition[D, S]) View() DefinitionView {
	return buildView(d.core)
}

type builderMode uint8

const (
	builderModeMain builderMode = iota
	builderModeSub
)

// pathBuilder 负责按当前所处位置继续往后拼接状态路径。
type pathBuilder[D Task, S any] struct {
	core     *definitionCore[D, S]
	mode     builderMode
	prevMain State
	currMain State
	subEntry State
	currSub  State
	consumed bool
}

// addConsumedErr 记录"同一个构建器被重复使用"带来的定义错误。
func (b *pathBuilder[D, S]) addConsumedErr() {
	entry := State("")
	from := b.currMain
	if b.mode == builderModeSub {
		entry = b.subEntry
		from = b.currSub
	}
	b.core.addErr(&DefinitionError{Kind: ErrBuilderConsumed, Entry: entry, From: from})
	b.core.addErr(&DefinitionError{Kind: ErrInvalidBuilderStage, Entry: entry, From: from})
}

// consume 标记当前构建器已经完成一次链式调用。
func (b *pathBuilder[D, S]) consume() bool {
	if b.consumed {
		b.addConsumedErr()
		return false
	}
	b.consumed = true
	return true
}

func (b *pathBuilder[D, S]) consumedClone() PathBuilder[D, S] {
	clone := *b
	clone.consumed = true
	return &clone
}

func (b *pathBuilder[D, S]) mainClone(prev, curr State) PathBuilder[D, S] {
	return &pathBuilder[D, S]{core: b.core, mode: builderModeMain, prevMain: prev, currMain: curr}
}

func (b *pathBuilder[D, S]) subClone(entry, curr State) PathBuilder[D, S] {
	return &pathBuilder[D, S]{
		core:     b.core,
		mode:     builderModeSub,
		prevMain: b.prevMain,
		currMain: b.currMain,
		subEntry: entry,
		currSub:  curr,
	}
}

// AddMain 追加一段主任务状态切换。
//
// 如果当前正在拼子任务路径，说明当前这段子任务流程已经结束，
// 会先回到对应主状态，再继续把新的主任务状态接到主路径后面。
func (b *pathBuilder[D, S]) AddMain(to State, cb TransitionCallback[D, S], mws ...TransitionCallbackMiddleware[D, S]) PathBuilder[D, S] {
	if !b.consume() {
		return b.consumedClone()
	}
	if b.mode == builderModeSub {
		b = b.closeSubFlow()
	}
	return b.appendMainTransition(to, transitionDef[D, S]{callback: cb, middlewares: mws})
}

// AddSub 在当前主状态下面追加一段子任务状态。
//
// 第一次从主任务切到子任务时，会把前一个 AddMain(...) 的回调拿来当子任务生成逻辑。
func (b *pathBuilder[D, S]) AddSub(to State, cb TransitionCallback[D, S], mws ...TransitionCallbackMiddleware[D, S]) PathBuilder[D, S] {
	if !b.consume() {
		return b.consumedClone()
	}

	flow := b.currentSubFlow()
	from := b.currSub
	if b.mode == builderModeMain {
		if IsFinalState(b.currMain) {
			b.core.addErr(&DefinitionError{Kind: ErrTransitionAfterFinal, From: b.currMain, To: to})
			return b.mainClone(b.prevMain, b.currMain)
		}
		flow = b.startSubFlow()
		if flow == nil {
			return b.mainClone(b.prevMain, b.currMain)
		}
		from = CreatedState
	}

	if IsFinalState(to) {
		flow.invalidFinalState = true
		b.core.addErr(&DefinitionError{Kind: ErrInvalidSubFinalState, Entry: flow.entry, From: from, To: to})
		return b.subClone(flow.entry, from)
	}
	if IsFinalState(from) {
		b.core.addErr(&DefinitionError{Kind: ErrTransitionAfterFinal, Entry: flow.entry, From: from, To: to})
		return b.subClone(flow.entry, from)
	}
	if _, ok := flow.transitions[from]; !ok {
		flow.transitions[from] = make(map[State]transitionDef[D, S])
	}
	flow.transitions[from][to] = transitionDef[D, S]{callback: cb, middlewares: mws}
	flow.path = append(flow.path, to)
	return b.subClone(flow.entry, to)
}

// AddCancel 为当前位置登记取消收尾回调。
func (b *pathBuilder[D, S]) AddCancel(cb TransitionCallback[D, S], mws ...TransitionCallbackMiddleware[D, S]) PathBuilder[D, S] {
	if b.consumed {
		b.addConsumedErr()
		return b
	}
	transition := transitionDef[D, S]{callback: cb, middlewares: mws}
	if b.mode == builderModeSub {
		flow := b.currentSubFlow()
		if flow != nil {
			flow.cancel[b.currSub] = transition
		}
		return b
	}
	b.core.mainCancel[b.currMain] = transition
	return b
}

// AddFail 为当前位置登记失败收尾回调。
func (b *pathBuilder[D, S]) AddFail(cb TransitionCallback[D, S], mws ...TransitionCallbackMiddleware[D, S]) PathBuilder[D, S] {
	if b.consumed {
		b.addConsumedErr()
		return b
	}
	transition := transitionDef[D, S]{callback: cb, middlewares: mws}
	if b.mode == builderModeSub {
		flow := b.currentSubFlow()
		if flow != nil {
			flow.fail[b.currSub] = transition
		}
		return b
	}
	b.core.mainFail[b.currMain] = transition
	return b
}

func (b *pathBuilder[D, S]) appendMainTransition(to State, transition transitionDef[D, S]) PathBuilder[D, S] {
	if IsFinalState(b.currMain) {
		b.core.addErr(&DefinitionError{Kind: ErrTransitionAfterFinal, From: b.currMain, To: to})
		return b.mainClone(b.prevMain, b.currMain)
	}
	from := b.currMain
	if _, ok := b.core.mainTransitions[from]; !ok {
		b.core.mainTransitions[from] = make(map[State]transitionDef[D, S])
	}
	b.core.mainTransitions[from][to] = transition
	b.core.mainPath = append(b.core.mainPath, to)
	return b.mainClone(from, to)
}

func (b *pathBuilder[D, S]) closeSubFlow() *pathBuilder[D, S] {
	flow := b.currentSubFlow()
	if flow == nil {
		return &pathBuilder[D, S]{core: b.core, mode: builderModeMain, prevMain: b.prevMain, currMain: b.currMain}
	}
	flow.closed = true
	return &pathBuilder[D, S]{core: b.core, mode: builderModeMain, prevMain: b.prevMain, currMain: flow.entry}
}

func (b *pathBuilder[D, S]) currentSubFlow() *subFlowDef[D, S] {
	entry := b.subEntry
	if entry == "" {
		entry = b.currMain
	}
	return b.core.subFlows[entry]
}

func (b *pathBuilder[D, S]) startSubFlow() *subFlowDef[D, S] {
	if _, exists := b.core.subFlows[b.currMain]; exists {
		b.core.addErr(&DefinitionError{Kind: ErrDuplicateSubFlow, Entry: b.currMain})
		return nil
	}
	flow := &subFlowDef[D, S]{
		entry:       b.currMain,
		path:        []State{CreatedState},
		cancel:      make(map[State]transitionDef[D, S]),
		fail:        make(map[State]transitionDef[D, S]),
		transitions: make(map[State]map[State]transitionDef[D, S]),
	}
	if b.prevMain != "" {
		if targets, ok := b.core.mainTransitions[b.prevMain]; ok {
			if transition, ok := targets[b.currMain]; ok {
				flow.generator = transition
				targets[b.currMain] = transitionDef[D, S]{}
			}
		}
	}
	b.core.subFlows[b.currMain] = flow
	return flow
}

// validateCore 对流程定义做最终结构校验。
func validateCore[D Task, S any](core *definitionCore[D, S]) error {
	if len(core.errs) == 1 {
		return core.errs[0]
	}
	if len(core.errs) > 1 {
		return &DefinitionErrors{Items: append([]error(nil), core.errs...)}
	}

	subErrs := make([]error, 0)
	for _, flow := range core.subFlows {
		if flow.invalidFinalState {
			continue
		}
		if !flow.closed {
			subErrs = append(subErrs, &DefinitionError{Kind: ErrSubFlowNotClosed, Entry: flow.entry})
		}
	}
	switch len(subErrs) {
	case 0:
		// no sub-flow errors
	case 1:
		return subErrs[0]
	default:
		return &DefinitionErrors{Items: subErrs}
	}

	if len(core.mainPath) <= 1 {
		return &DefinitionError{Kind: ErrEmptyMainPath}
	}
	if !IsFinalState(core.mainPath[len(core.mainPath)-1]) {
		return &DefinitionError{Kind: ErrMainPathNotTerminal, From: core.mainPath[len(core.mainPath)-1]}
	}
	return nil
}

// buildView 把内部定义结构转成只读拓扑结果，供页面、监控和查询使用。
func buildView[D Task, S any](core *definitionCore[D, S]) DefinitionView {
	view := DefinitionView{
		MainPath: append([]State(nil), core.mainPath...),
		SubPaths: make(map[State][]State, len(core.subFlows)),
	}
	for entry, flow := range core.subFlows {
		view.SubPaths[entry] = append([]State(nil), flow.path...)
	}
	return view
}
