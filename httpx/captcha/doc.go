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
