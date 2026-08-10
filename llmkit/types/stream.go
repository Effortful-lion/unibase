package types

// StreamChunk 是流式响应的一个数据块。
type StreamChunk struct {
	Content   string
	ToolCalls []ToolCall
	Usage     *Usage
	Done      bool
	Err       error
}

// Usage 表示 token 使用情况。
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}
