// Package memory 提供了 Agent 记忆管理。
//
// 记忆管理解决的核心问题是：对话历史会无限增长，超出 LLM 上下文窗口。
// 本包提供了滑动窗口记忆（Sliding Window），自动保留最近 N 条消息。
//
// 使用示例：
//
//	// 1. 创建滑动窗口记忆（保留最近 20 条消息）
//	mem := memory.NewSlidingWindow(20)
//
//	// 2. 在 Agent 的 Run 前注入记忆
//	mem.Inject(agent.Messages())
//	filtered := mem.Retrieve() // 返回不超过 20 条的最近消息
//
//	// 3. 将过滤后的消息注入 Agent
//	// （需要结合 Agent 的高级用法，在调用前裁剪消息）
package memory

import (
	"sync"

	"github.com/Effortful-lion/unibase/llmkit/types"
)

// Memory 记忆接口。
type Memory interface {
	// Inject 注入新消息到记忆中。
	Inject(messages []types.Message)

	// Retrieve 返回当前记忆中的所有消息（经过过滤/裁剪）。
	Retrieve() []types.Message

	// Clear 清空记忆。
	Clear()
}

// SlidingWindowConfig 滑动窗口配置。
type SlidingWindowConfig struct {
	// MaxMessages 最大保留消息数，超过后自动丢弃最旧的消息。
	// 默认 20，≤0 表示不限制。
	MaxMessages int
}

// SlidingWindow 滑动窗口记忆，保留最近 N 条消息。
//
// 适用场景：
//   - 多轮对话，需要限制上下文长度
//   - 不需要长期记忆，只关注最近对话
//
// 不适用场景：
//   - 需要长期记忆（使用其他 Memory 实现）
//   - 需要智能摘要（使用 SummarizedMemory）
//
// 线程安全：所有方法均通过 mu 保护内部状态，可并发调用。
type SlidingWindow struct {
	mu          sync.Mutex
	maxMessages int
	messages    []types.Message
}

// NewSlidingWindow 创建滑动窗口记忆。
//
// maxMessages 指定最大保留消息数，0 或负数表示不限制。
func NewSlidingWindow(maxMessages int) *SlidingWindow {
	if maxMessages <= 0 {
		maxMessages = 20 // 默认值
	}
	return &SlidingWindow{
		maxMessages: maxMessages,
		messages:    make([]types.Message, 0, maxMessages),
	}
}

// Inject 注入新消息。
func (s *SlidingWindow) Inject(messages []types.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, messages...)
	s.trim()
}

// Add 注入单条消息（便捷方法）。
func (s *SlidingWindow) Add(msg types.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, msg)
	s.trim()
}

// Retrieve 返回当前记忆中的所有消息。
func (s *SlidingWindow) Retrieve() []types.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]types.Message, len(s.messages))
	copy(result, s.messages)
	return result
}

// Clear 清空记忆。
func (s *SlidingWindow) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = s.messages[:0]
}

// trim 裁剪到 maxMessages。
func (s *SlidingWindow) trim() {
	if len(s.messages) > s.maxMessages {
		// 丢弃最旧的消息（从头部删除）
		excess := len(s.messages) - s.maxMessages
		s.messages = s.messages[excess:]
	}
}

// Len 返回当前消息数。
func (s *SlidingWindow) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.messages)
}

// ────────────────── 记忆增强器 ──────────────────

// MemoryEnhancer 记忆增强器，在 Agent 调用前后自动管理记忆。
type MemoryEnhancer struct {
	memory Memory
}

// NewEnhancer 创建记忆增强器。
func NewEnhancer(mem Memory) *MemoryEnhancer {
	return &MemoryEnhancer{memory: mem}
}

// PreProcess 在 Agent 调用前处理消息历史。
// 将记忆中的消息与当前消息合并，返回裁剪后的消息列表。
func (e *MemoryEnhancer) PreProcess(currentMessages []types.Message) []types.Message {
	stored := e.memory.Retrieve()
	return append(stored, currentMessages...)
}

// PostProcess 在 Agent 调用后处理消息，将结果存入记忆。
func (e *MemoryEnhancer) PostProcess(messages []types.Message) {
	e.memory.Inject(messages)
}
