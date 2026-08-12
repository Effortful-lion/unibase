package adapter

import "github.com/Effortful-lion/unibase/logx"

// Option 配置适配器的可选参数。
type Option func(*clientOptions)

// clientOptions 全局可选配置。
type clientOptions struct {
	logger *logx.Logger
}

// defaultClientOptions 返回默认配置。
func defaultClientOptions() clientOptions {
	return clientOptions{
		logger: logx.Module("adapter"),
	}
}

// WithLogger 注入自定义日志器。
func WithLogger(logger *logx.Logger) Option {
	return func(o *clientOptions) {
		if logger != nil {
			o.logger = logger
		}
	}
}
