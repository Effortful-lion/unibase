// Package lg 提供轻量级结构化日志能力。
//
// 核心设计：
//   - 一个 Logger = 一个 Writer，Module 仅标记模块名
//   - Writer 接口负责存储目标，Formatter 接口负责输出格式
//   - 内置 ConsoleWriter / FileWriter / MultiWriter
//   - 内置 JSONFormatter，支持自定义 Formatter
//
// 快速开始：
//
//	// 文本输出到控制台
//	lg.Info("服务启动", lg.Fields{"port": 8080})
//
//	// 文本输出到文件
//	fw, _ := lg.NewFileWriter("logs/app.log", lg.LevelInfo)
//	lg.New(fw).Module("user").Info("用户登录")
//
//	// JSON 输出到文件
//	fw, _ := lg.NewFileWriter("logs/app.json", lg.LevelInfo)
//	fw.SetFormatter(lg.JSONFormatter)
//	lg.New(fw).Module("user").Info("登录", lg.Fields{"uid": 123})
//
// 更多示例详见各类型和函数的文档注释。
package logx
