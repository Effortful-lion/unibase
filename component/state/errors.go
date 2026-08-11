package state

import (
	"errors"
	"strings"
)

// DefinitionErrorKind 表示定义校验阶段的错误分类。
type DefinitionErrorKind string

// StateError 表示运行期暴露给调用方的状态机哨兵错误。
type StateError struct {
	message string
}

// 定义校验错误类型集合。
const (
	ErrEmptyMainPath        DefinitionErrorKind = "empty_main_path"
	ErrMainPathNotTerminal  DefinitionErrorKind = "main_path_not_terminal"
	ErrSubFlowNotClosed     DefinitionErrorKind = "sub_flow_not_closed"
	ErrDuplicateSubFlow     DefinitionErrorKind = "duplicate_sub_flow"
	ErrInvalidSubFinalState DefinitionErrorKind = "invalid_sub_final_state"
	ErrBuilderConsumed      DefinitionErrorKind = "builder_consumed"
	ErrTransitionAfterFinal DefinitionErrorKind = "transition_after_final"
	ErrInvalidBuilderStage  DefinitionErrorKind = "invalid_builder_stage"
)

// DefinitionError 描述一条具体的状态定义校验失败信息。
type DefinitionError struct {
	Kind  DefinitionErrorKind
	Entry State
	From  State
	To    State
	Msg   string
}

// Error 返回可读的定义校验错误信息。
func (e *DefinitionError) Error() string {
	if e == nil {
		return ""
	}
	if e.Msg != "" {
		return e.Msg
	}
	return string(e.Kind)
}

// DefinitionErrors 聚合一次校验过程中发现的多条错误。
type DefinitionErrors struct {
	Items []error
}

// Error 将聚合的多条定义错误拼接成单条字符串。
func (e *DefinitionErrors) Error() string {
	if e == nil || len(e.Items) == 0 {
		return ""
	}
	parts := make([]string, 0, len(e.Items))
	for _, item := range e.Items {
		parts = append(parts, item.Error())
	}
	return strings.Join(parts, "; ")
}

// Unwrap 返回底层错误切片，便于 errors.Is/errors.As 使用。
func (e *DefinitionErrors) Unwrap() []error {
	if e == nil {
		return nil
	}
	return e.Items
}

// Error 返回运行期错误信息。
func (e *StateError) Error() string {
	if e == nil {
		return ""
	}
	return e.message
}

// IsInvalidTransition 判断 err 是否为无效状态转换错误。
func IsInvalidTransition(err error) bool {
	return errors.Is(err, ErrInvalidTransition)
}

// IsTaskWaiting 判断 err 是否为任务等待错误。
func IsTaskWaiting(err error) bool {
	return errors.Is(err, ErrTaskWaiting)
}

// IsSubTasksFailed 判断 err 是否为子任务失败错误。
func IsSubTasksFailed(err error) bool {
	return errors.Is(err, ErrSubTasksFailed)
}

// IsTaskNotFound 判断 err 是否为任务未找到错误。
func IsTaskNotFound(err error) bool {
	return errors.Is(err, ErrTaskNotFound)
}

// IsTaskAlreadyRunning 判断 err 是否为任务已在执行中错误。
func IsTaskAlreadyRunning(err error) bool {
	return errors.Is(err, ErrTaskAlreadyRunning)
}

// IsSubTaskNotLoaded 判断 err 是否为子任务未加载错误。
func IsSubTaskNotLoaded(err error) bool {
	return errors.Is(err, ErrSubTaskNotLoaded)
}

var (
	ErrInvalidTransition          = &StateError{message: "invalid state transition"}
	ErrTaskWaiting                = &StateError{message: "task waiting"}
	ErrSubTasksFailed             = &StateError{message: "sub tasks failed"}
	ErrTaskNotFound               = &StateError{message: "task not found"}
	ErrTaskAlreadyRunning         = &StateError{message: "task already running"}
	ErrSubTaskLoaderNotRegistered = &StateError{message: "sub task loader not registered"}
	ErrSubTaskNotLoaded           = &StateError{message: "sub task not loaded"}
)
