package types

// Usage 表示单次 LLM 调用的 token 用量统计。
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}
