package sse

import (
	"fmt"
	"io"
)

// Event 表示一个 SSE 事件。
type Event struct {
	ID      []byte
	Data    []byte
	Event   []byte
	Retry   []byte
	Comment []byte
}

// MarshalTo 将事件序列化并写入 w。
func (e *Event) MarshalTo(w io.Writer) error {
	if len(e.Data) == 0 && len(e.Comment) == 0 {
		return nil
	}

	if len(e.Data) > 0 {
		if len(e.ID) > 0 {
			if _, err := fmt.Fprintf(w, "id: %s\n", e.ID); err != nil {
				return err
			}
		}
		lines := splitLines(e.Data)
		for _, line := range lines {
			if _, err := fmt.Fprintf(w, "data: %s\n", line); err != nil {
				return err
			}
		}
		if len(e.Event) > 0 {
			if _, err := fmt.Fprintf(w, "event: %s\n", e.Event); err != nil {
				return err
			}
		}
		if len(e.Retry) > 0 {
			if _, err := fmt.Fprintf(w, "retry: %s\n", e.Retry); err != nil {
				return err
			}
		}
	}

	if len(e.Comment) > 0 {
		if _, err := fmt.Fprintf(w, ": %s\n", e.Comment); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprint(w, "\n"); err != nil {
		return err
	}
	return nil
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			end := i
			if end > start && data[end-1] == '\r' {
				end--
			}
			lines = append(lines, data[start:end])
			start = i + 1
		}
	}
	if start < len(data) {
		end := len(data)
		if end > start && data[end-1] == '\r' {
			end--
		}
		lines = append(lines, data[start:end])
	}
	return lines
}
