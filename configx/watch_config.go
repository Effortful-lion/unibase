package configx

/*
1. 监听 config
2. 支持类型 yaml、toml
3. 可配置监听时间变化、事件hook等（如果可以再补充）
*/

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

//========================================导出方法=======================================

// WatchOptions 配置监听行为。
type WatchOptions struct {
	// Interval 检查文件变更的最小间隔，默认 100ms。
	Interval time.Duration
	// Debounce 防抖时间，连续变更在此时间内只触发一次回调，默认 200ms。
	Debounce time.Duration
	// IgnoreHidden 是否忽略隐藏文件（. 开头的文件），默认 true。
	IgnoreHidden bool
}

// Watch 开始监听配置文件变化。
// 当配置文件被修改时，自动重新加载并触发 OnChange 回调。
// 阻塞运行，应在独立 goroutine 中调用。
func (c *Config) Watch(opts WatchOptions) error {
	if opts.Interval <= 0 {
		opts.Interval = 100 * time.Millisecond
	}
	if opts.Debounce <= 0 {
		opts.Debounce = 200 * time.Millisecond
	}

	c.mu.RLock()
	configFile := c.v.ConfigFileUsed()
	c.mu.RUnlock()

	if configFile == "" {
		return fmt.Errorf("config has no source file to watch")
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}
	defer func() { _ = watcher.Close() }()

	dir := filepath.Dir(configFile)
	if err := watcher.Add(dir); err != nil {
		return fmt.Errorf("watch %s: %w", dir, err)
	}

	var lastWrite time.Time
	var pending bool

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}

			filename := filepath.Base(event.Name)
			if opts.IgnoreHidden && strings.HasPrefix(filename, ".") {
				continue
			}

			if !strings.EqualFold(filepath.Base(event.Name), filepath.Base(configFile)) {
				continue
			}

			if event.Op&fsnotify.Write == 0 {
				continue
			}

			now := time.Now()
			if !pending && now.Sub(lastWrite) < opts.Interval {
				continue
			}

			lastWrite = now
			pending = true

			go func() {
				time.Sleep(opts.Debounce)
				pending = false

				c.mu.RLock()
				c.v.WatchConfig()
				c.mu.RUnlock()
				// 执行变化回调
				if c.onChange != nil {
					c.runOnChange()
				}
			}()

		case _, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			// 监听错误不中断，继续监听

		case <-time.After(opts.Interval):
			// 保活
		}
	}
}
