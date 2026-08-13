// Package captcha 提供图形验证码生成与校验能力。
//
// 基于 mojocn/base64Captcha，封装为简洁的 API。
// 支持字符串验证码、算术验证码等多种类型。
//
// 快速开始：
//
//	// 生成验证码
//	result := captcha.Generate()
//	// result.ID       — 验证码 ID（需发送给前端）
//	// result.Image    — base64 编码图片（可直接嵌入 <img>）
//
//	// 校验
//	if captcha.Verify(result.ID, userInput) {
//	    // 校验通过
//	}
//
// 能力：Generate（生成验证码）、Verify（校验验证码）。
package captcha

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/mojocn/base64Captcha"
)

// Result 是验证码生成的结果。
type Result struct {
	// ID 是验证码的唯一标识，用于后续校验。
	// 生产环境应将 ID 通过 Cookie 或 Session 与用户关联。
	ID string
	// Image 是 base64 编码的验证码图片（不含 "data:image/png;base64," 前缀）。
	Image string
	// Answer 是验证码的正确答案（仅开发/测试场景使用，生产环境不应返回）。
	Answer string
}

// Config 配置验证码生成参数。
type Config struct {
	// Height 图片高度（像素），默认 80。
	Height int
	// Width 图片宽度（像素），默认 240。
	Width int
	// Length 验证码字符长度，默认 6。
	Length int
	// NoiseCount 干扰线数量，默认 1。
	NoiseCount int
	// ShowLineOptions 干扰线样式（使用 OptionShowHollowLine 等常量组合），0 为不显示。
	ShowLineOptions int
}

// DefaultConfig 返回默认验证码配置（字符串类型，含数字+字母）。
func DefaultConfig() *Config {
	return &Config{
		Height:          80,
		Width:           240,
		Length:          6,
		NoiseCount:      1,
		ShowLineOptions: base64Captcha.OptionShowHollowLine,
	}
}

var defaultStore = base64Captcha.DefaultMemStore

// Generate 生成一个字符串验证码。
//
// 返回的 Result.Image 可直接用于 <img src="data:image/png;base64,{{.Image}}">。
// Result.ID 需保存（建议通过 Cookie 发送给前端），用于 Verify 时匹配。
func Generate(opts ...func(*Config)) *Result {
	cfg := DefaultConfig()
	for _, apply := range opts {
		apply(cfg)
	}

	driver := base64Captcha.NewDriverString(
		cfg.Height, cfg.Width, cfg.NoiseCount, cfg.ShowLineOptions,
		cfg.Length,
		"23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz", // 去除易混淆字符 0/O/1/I/l
		nil, nil, nil,
	)

	c := base64Captcha.NewCaptcha(driver, defaultStore)
	id, b64Img, answer, err := c.Generate()
	if err != nil {
		panic(fmt.Sprintf("captcha: generate failed: %v", err))
	}

	return &Result{
		ID:     id,
		Image:  b64Img,
		Answer: answer,
	}
}

// Verify 校验验证码。
//
// id 是 Generate 返回的验证码 ID（从客户端带回）。
// answer 是用户输入的答案。
// clear 为 true 时，校验后立即销毁该验证码（防止重复提交）。
//
// 返回 true 表示校验通过。
func Verify(id, answer string, clear ...bool) bool {
	c := base64Captcha.NewCaptcha(nil, defaultStore)
	clearFlag := len(clear) > 0 && clear[0]
	return c.Verify(id, answer, clearFlag)
}

// GinHandler 返回一个 Gin 处理函数，用于生成验证码接口。
//
// 返回 JSON：`{"captcha_id": "...", "captcha_img": "data:image/png;base64,..."}`。
// 验证码 ID 同时通过 HttpOnly Cookie 下发。
func GinHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		result := Generate()
		c.SetCookie("captcha_id", result.ID, 300, "/", "", false, true)
		c.JSON(200, gin.H{
			"captcha_id":  result.ID,
			"captcha_img": "data:image/png;base64," + result.Image,
		})
	}
}

// WithHeight 设置验证码图片高度。
func WithHeight(h int) func(*Config) {
	return func(c *Config) { c.Height = h }
}

// WithWidth 设置验证码图片宽度。
func WithWidth(w int) func(*Config) {
	return func(c *Config) { c.Width = w }
}

// WithLength 设置验证码字符长度。
func WithLength(l int) func(*Config) {
	return func(c *Config) { c.Length = l }
}

// WithNoiseCount 设置干扰线数量。
func WithNoiseCount(n int) func(*Config) {
	return func(c *Config) { c.NoiseCount = n }
}

// WithShowLineOptions 设置干扰线样式。
// 使用 base64Captcha 包中的 OptionShowHollowLine 等常量组合。
func WithShowLineOptions(opts int) func(*Config) {
	return func(c *Config) { c.ShowLineOptions = opts }
}
