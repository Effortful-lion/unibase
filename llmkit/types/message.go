// Package types 提供了 llmkit 的核心类型定义。
package types

import "encoding/json"

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
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// Message 表示对话中的一条消息。
type Message struct {
	Role             Role
	Content          string
	Parts            []ContentBlock
	ToolCallID       string
	ToolName         string
	ReasoningContent string
	Extra            map[string]any
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
