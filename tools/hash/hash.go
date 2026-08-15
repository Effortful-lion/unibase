package hash

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"hash/crc32"
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

func crc32Sum(data []byte) []byte {
	h := crc32.NewIEEE()
	h.Write(data)
	sum := h.Sum32()
	b := make([]byte, 4)
	b[0] = byte(sum >> 24)
	b[1] = byte(sum >> 16)
	b[2] = byte(sum >> 8)
	b[3] = byte(sum)
	return b
}

// Sha256 计算 SHA-256 哈希，返回原始字节（32 字节）。
// 用于签名、校验等安全场景。
func Sha256(data []byte) []byte {
	return sha256Sum(data)
}

// Sha256Hex 计算 SHA-256 哈希，返回 hex 编码字符串。
func Sha256Hex(data []byte) string {
	return hex.EncodeToString(sha256Sum(data))
}

// MD5 计算 MD5 哈希，返回原始字节（16 字节）。
// 仅用于非安全场景：文件 checksum、缓存 key 等。
func MD5(data []byte) []byte {
	return md5Sum(data)
}

// MD5Hex 计算 MD5 哈希，返回 hex 编码字符串。
func MD5Hex(data []byte) string {
	return hex.EncodeToString(md5Sum(data))
}

// CRC32 计算 CRC32 (IEEE) 哈希，返回 4 字节大端序结果。
// 用于文件 checksum、缓存 key、快速数据完整性校验。
func CRC32(data []byte) []byte {
	return crc32Sum(data)
}

// CRC32Hex 计算 CRC32 哈希，返回 hex 编码字符串。
func CRC32Hex(data []byte) string {
	return hex.EncodeToString(crc32Sum(data))
}

// CRC32Uint32 计算 CRC32 (IEEE) 哈希，返回 uint32 值。
// 用于一致性哈希等需要数值哈希的场景。
func CRC32Uint32(data []byte) uint32 {
	h := crc32.NewIEEE()
	h.Write(data)
	return h.Sum32()
}
