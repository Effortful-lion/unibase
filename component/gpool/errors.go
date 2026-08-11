package gpool

import "errors"

// ErrPoolClosed 池已关闭，不能再提交新任务。
var ErrPoolClosed = errors.New("pool closed")

// ErrPoolFull 池已满，TrySubmit 在容量不足时返回此错误。
var ErrPoolFull = errors.New("pool full")
