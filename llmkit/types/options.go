package types

// CallOptions 是单次聊天调用的配置。
// 指针字段为 nil 表示"未设置"，由 Provider 使用模型默认值。
type CallOptions struct {
	Model       *string
	Temperature *float32
	MaxTokens   *int
	TopP        *float32
	Stop        []string
	Tools       []ToolInfo
	ToolChoice  *ToolChoice
	Stream      bool
}

// ToolChoice 控制模型是否调用工具。
type ToolChoice string

const (
	ToolChoiceNone     ToolChoice = "none"
	ToolChoiceAuto     ToolChoice = "auto"
	ToolChoiceRequired ToolChoice = "required"
)

// ToolInfo 是传递给模型的工具元数据。
type ToolInfo struct {
	Name        string
	Description string
	Parameters  map[string]any
	Strict      bool
}

// Option 是 CallOptions 的函数式配置选项。
type Option func(*CallOptions)

func WithModel(name string) Option {
	return func(o *CallOptions) { o.Model = &name }
}

func WithTemperature(t float32) Option {
	return func(o *CallOptions) { o.Temperature = &t }
}

func WithMaxTokens(n int) Option {
	return func(o *CallOptions) { o.MaxTokens = &n }
}

func WithTopP(p float32) Option {
	return func(o *CallOptions) { o.TopP = &p }
}

func WithStop(words []string) Option {
	return func(o *CallOptions) { o.Stop = words }
}

func WithTools(tools []ToolInfo) Option {
	return func(o *CallOptions) { o.Tools = tools }
}

func WithToolChoice(choice ToolChoice) Option {
	return func(o *CallOptions) { o.ToolChoice = &choice }
}

func WithStream(enable bool) Option {
	return func(o *CallOptions) { o.Stream = enable }
}
