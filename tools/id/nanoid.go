package id

import (
	gonanoid "github.com/matoous/go-nanoid/v2"
)

// Nanoid 生成指定长度的 URL-safe 随机 ID。
// size <= 0 时使用默认长度 21 字符，与 JS nanoid 对齐。
// 底层使用 crypto/rand，密码学安全。
func Nanoid(size int) (string, error) {
	if size <= 0 {
		return gonanoid.New()
	}
	return gonanoid.New(size)
}

// MustNanoid 同 Nanoid，但 panic 于错误。
func MustNanoid(size int) string {
	if size < 0 {
		panic("id: nanoid size must be non-negative")
	}
	if size == 0 {
		return gonanoid.Must()
	}
	return gonanoid.Must(size)
}
