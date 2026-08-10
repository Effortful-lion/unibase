package configx

/*
1. 读配置文件
2. 支持类型：yaml、toml
3. 本地配置：支持输入 path（项目根目录/.../config.yaml），然后自动从工作目录开始向上搜索到根目录，再到目标文件
4. 远程配置：暂不支持（几乎用不到，本地开发后，config远程只是为了不和代码在一个机器上而已）
*/

import (
	"fmt"
	"os"
	"path/filepath"
)

//========================================导出方法=======================================

// ReadConfig 是读取配置文件的唯一入口。
// 按以下顺序尝试加载：
//  1. 如果 path 是绝对路径或相对路径且文件存在，直接加载
//  2. 否则从当前工作目录开始向上逐级搜索该文件名
//
// 要做到的其实就是如果有该配置文件，一定能找到
// 支持格式：yaml、toml、json（由文件扩展名自动推断）
func ReadConfig(path string) (*Config, error) {
	// 先尝试直接加载（支持相对路径和绝对路径）
	if _, err := os.Stat(path); err == nil {
		return New(path)
	}

	// 直接加载失败，尝试从当前工作目录向上搜索
	filename := filepath.Base(path)
	_, cfg, err := searchUpward(filename)
	if err == nil {
		return cfg, nil
	}

	// 都失败了，返回原始路径的错误（可能是路径写错了）
	return nil, fmt.Errorf("read config from path: %s,err: %w", path, err)
}

//========================================非导出方法=======================================

// searchUpward 从当前工作目录开始，向上逐级搜索目标文件。
// 例如：输入 "config.yaml"，会依次查找：
//
//	./config.yaml -> ../config.yaml -> ../../config.yaml -> ...
//
// 找到后返回绝对路径和 Config，未找到返回错误。
func searchUpward(filename string) (string, *Config, error) {
	// 获取工作目录
	wd, err := os.Getwd()
	if err != nil {
		return "", nil, fmt.Errorf("get work dir: %w", err)
	}

	// for
	for {
		candidate := filepath.Join(wd, filename)
		if _, err := os.Stat(candidate); err == nil {
			// if file exist
			cfg, err := New(candidate)
			if err != nil {
				return "", nil, fmt.Errorf("load %s: %w", candidate, err)
			}
			return candidate, cfg, nil
		}

		parent := filepath.Dir(wd)
		if parent == wd {
			break // 已到根目录
		}
		wd = parent
	}

	return "", nil, fmt.Errorf("config file %q not found in any parent directory", filename)
}
