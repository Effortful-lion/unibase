// Package encodingx 提供 JSON / YAML / TOML 三种格式的文件读写能力。
//
// 设计原则：
//   - 入口单一：每个格式暴露 Read / Write 两个包级函数
//   - 简单优先：不追求大而全，只覆盖高频场景
//   - 错误友好：返回底层错误，不额外包装
//
// 快速开始：
//
//	// 读取 JSON 文件
//	var cfg Config
//	if err := encodingx.ReadJSON("config.json", &cfg); err != nil {
//	    log.Fatal(err)
//	}
//
//	// 写入 YAML 文件
//	if err := encodingx.WriteYAML("output.yaml", data); err != nil {
//	    log.Fatal(err)
//	}
//
//	// TOML 带缩进写入
//	if err := encodingx.WriteTOML("output.toml", data, encodingx.WithIndent("\t")); err != nil {
//	    log.Fatal(err)
//	}
//
//	// 注意：WithIndent 对不同格式有不同语义：
//	//   JSON: 直接使用字符串作为缩进（支持 "\t"）
//	//   YAML: 使用字符串的字节长度作为缩进空格数（"\t" 长度为 1，即 1 个空格）
//	//   TOML: 直接使用字符串作为缩进符号（支持 "\t"）
//
// 能力：ReadJSON / WriteJSON / ReadYAML / WriteYAML / ReadTOML / WriteTOML。
package encodingx
