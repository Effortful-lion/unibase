package encodingx

import (
	"bytes"
	"encoding/json"
	"os"

	toml "github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

// ── 配置选项 ─────────────────────────────────────────────────────

// EncodeOption 编码写入配置。
type EncodeOption func(*encodeConfig)

type encodeConfig struct {
	indent string // 缩进字符串，空表示紧凑模式
}

func defaultEncodeConfig() *encodeConfig {
	return &encodeConfig{}
}

// WithIndent 设置缩进字符串（如 "  " 两个空格、"\t" 制表符）。
// 不调用此选项则输出紧凑格式（JSON 无缩进，YAML/TOML 使用默认缩进）。
//
// 注意：YAML 的 SetIndent 接受 int 类型，因此 YAML 使用字符串的字节长度作为缩进空格数
// （如 "\t" 长度为 1，等价于 1 个空格）。JSON 和 TOML 直接使用原始字符串。
func WithIndent(indent string) EncodeOption {
	return func(c *encodeConfig) {
		c.indent = indent
	}
}

// ── JSON ─────────────────────────────────────────────────────────

// ReadJSON 从文件读取 JSON 并反序列化到 v。
// v 必须为指针类型。
func ReadJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// WriteJSON 将 v 序列化为 JSON 并写入文件。
// opts 控制输出格式，默认紧凑输出。
func WriteJSON(path string, v any, opts ...EncodeOption) error {
	cfg := defaultEncodeConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", cfg.indent)
	if err := enc.Encode(v); err != nil {
		return err
	}

	return os.WriteFile(path, buf.Bytes(), 0644)
}

// ── YAML ─────────────────────────────────────────────────────────

// ReadYAML 从文件读取 YAML 并反序列化到 v。
// v 必须为指针类型。
func ReadYAML(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, v)
}

// WriteYAML 将 v 序列化为 YAML 并写入文件。
// opts 控制输出格式，默认使用两个空格缩进。
func WriteYAML(path string, v any, opts ...EncodeOption) error {
	cfg := defaultEncodeConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	indent := cfg.indent
	if indent == "" {
		indent = "  "
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(len(indent))
	if err := enc.Encode(v); err != nil {
		return err
	}
	enc.Close()

	return os.WriteFile(path, buf.Bytes(), 0644)
}

// ── TOML ─────────────────────────────────────────────────────────

// ReadTOML 从文件读取 TOML 并反序列化到 v。
// v 必须为指针类型。
func ReadTOML(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return toml.Unmarshal(data, v)
}

// WriteTOML 将 v 序列化为 TOML 并写入文件。
// opts 控制输出格式，默认使用两个空格缩进。
func WriteTOML(path string, v any, opts ...EncodeOption) error {
	cfg := defaultEncodeConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	indent := cfg.indent
	if indent == "" {
		indent = "  "
	}

	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.SetIndentSymbol(indent)
	if err := enc.Encode(v); err != nil {
		return err
	}

	return os.WriteFile(path, buf.Bytes(), 0644)
}
