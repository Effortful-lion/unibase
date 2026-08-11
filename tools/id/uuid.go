package id

import (
	"github.com/google/uuid"
)

// UUID 生成标准 UUID v4。
func UUID() string {
	return uuid.New().String()
}
