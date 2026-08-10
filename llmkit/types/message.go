// Package types 提供了 llmkit 的核心类型定义。
package types

// Role 是消息角色。
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ContentBlockType 是消息内容块的类型。
type ContentBlockType string

const (
	BlockText     ContentBlockType = "text"
	BlockThinking ContentBlockType = "thinking"
	BlockToolCall ContentBlockType = "tool_call"
	BlockImage    ContentBlockType = "image"
	BlockAudio    ContentBlockType = "audio"
	BlockVideo    ContentBlockType = "video"
	BlockFile     ContentBlockType = "file"
)

// ContentBlock 是消息中的富内容块。
// 仅 Type 对应的字段有效。
type ContentBlock struct {
	Type      ContentBlockType
	Text      string
	Thinking  string
	ToolCall  *ToolCall
	ImageURL  string
	ImageData []byte
	AudioURL  string
	AudioData []byte
	VideoURL  string
	VideoData []byte
	FileName  string
	FileData  []byte
	MimeType  string
}

// ToolCall 表示助手发起的工具调用。
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON 格式的调用参数
}

// Message 表示对话中的一条消息。
//
// 序列化约定：如果 Parts 非空，优先使用 Parts 作为富内容；
// 否则使用 Content 作为纯文本内容。
// User/System/Tool 消息通常只使用 Content。
// Assistant 消息使用 Parts 携带富内容（文本、思考、工具调用等）。
type Message struct {
	Role             Role
	Content          string         // 纯文本内容（User/System/Tool 消息使用）
	Parts            []ContentBlock // 富内容块（Assistant 消息使用，非空时优先）
	ToolCallID       string         // 工具调用回溯 ID（Tool 消息使用）
	ToolName         string         // 工具名称（Tool 消息使用）
	ReasoningContent string         // 推理内容（部分模型支持）
	Extra            map[string]any // 扩展字段
}

// NewSystemMessage 创建 system 消息。
func NewSystemMessage(content string) Message {
	return Message{Role: RoleSystem, Content: content}
}

// NewUserMessage 创建 user 消息。
func NewUserMessage(content string) Message {
	return Message{Role: RoleUser, Content: content}
}

// NewAssistantMessage 创建 assistant 消息。
func NewAssistantMessage(content string) Message {
	return Message{Role: RoleAssistant, Content: content}
}

// NewToolMessage 创建 tool 结果消息。
func NewToolMessage(toolCallID, toolName, content string) Message {
	return Message{
		Role:       RoleTool,
		Content:    content,
		ToolCallID: toolCallID,
		ToolName:   toolName,
	}
}
