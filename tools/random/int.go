package random

import (
	"crypto/rand"
	"errors"
	"math/big"
)

// Int 返回 [0, n) 范围内的密码学安全随机整数。
// 用于生成 OTP、临时编号等需要不可预测随机数的场景。
// n 必须大于 0。
func Int(n int) (int, error) {
	if n <= 0 {
		return 0, errors.New("random: n must be positive")
	}
	bigN := big.NewInt(int64(n))
	result, err := rand.Int(rand.Reader, bigN)
	if err != nil {
		return 0, err
	}
	return int(result.Int64()), nil
}

// MustInt 同 Int，但 panic 于错误。
func MustInt(n int) int {
	v, err := Int(n)
	if err != nil {
		panic(err)
	}
	return v
}
