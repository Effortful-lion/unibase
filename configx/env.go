package configx

/*
1. 用于 .env 文件的读写。
2. 支持常用操作 / 操作对应常用场景，eg：
// （详情举例）用于 ....
// function （简短）用于
*/

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

//========================================导出方法=======================================

// LoadEnv 加载 .env 文件到环境变量。
// paths 为 .env 文件路径列表，按顺序加载，后加载的覆盖先加载的。
// 如果 paths 为空，默认加载当前目录的 .env 文件。
func LoadEnv(paths ...string) error {
	if len(paths) == 0 {
		paths = []string{".env"}
	}

	var lastErr error
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			lastErr = err
			continue
		}

		if err := godotenv.Load(path); err != nil {
			lastErr = fmt.Errorf("load env %s: %w", path, err)
			continue
		}
	}

	return lastErr
}
