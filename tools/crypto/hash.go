package crypto

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"hash"
)

func sha256Sum(data []byte) []byte {
	h := sha256.New()
	h.Write(data)
	return h.Sum(nil)
}

func md5Sum(data []byte) []byte {
	h := md5.New()
	h.Write(data)
	return h.Sum(nil)
}

// Sha256 计算 SHA-256 哈希，返回原始字节。
func Sha256(data []byte) []byte {
	return sha256Sum(data)
}

// Sha256Hex 计算 SHA-256 哈希，返回 hex 编码字符串。
func Sha256Hex(data []byte) string {
	return hex.EncodeToString(sha256Sum(data))
}

// MD5 计算 MD5 哈希，返回原始字节（仅用于非安全场景，如 checksum）。
func MD5(data []byte) []byte {
	return md5Sum(data)
}

// MD5Hex 计算 MD5 哈希，返回 hex 编码字符串。
func MD5Hex(data []byte) string {
	return hex.EncodeToString(md5Sum(data))
}

// poolHash 从池中获取 hash 实例，执行 f 后归还。
// 用于高并发场景下减少 hash 对象分配。
func poolHash(newHash func() hash.Hash, f func(h hash.Hash) []byte) []byte {
	h := newHash()
	defer h.Reset()
	return f(h)
}
