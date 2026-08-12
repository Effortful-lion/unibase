// Package params 提供请求参数绑定与校验增强能力。
//
// 校验规则采用统一设计：Rule 构造器 + RegisterRule 注册，
// 框架自动处理引擎注册和 Bind 时的应用，用户只需维护一份规则定义。
//
// 快速开始：
//
//	// 绑定 + 校验，struct tag 驱动
//	var req CreateUserReq
//	params.MustBindJSON(c, &req)
//
//	// 注册自定义规则，之后 struct tag 自动生效
//	params.RegisterRule("mobile", func(fl validator.FieldLevel) bool {
//		return regexp.MustCompile(`^1[3-9]\d{9}$`).MatchString(fl.Field().String())
//	})
//
//	type CreateUserReq struct {
//	    Mobile string `json:"mobile" validate:"required,mobile"`
//	}
//
//	// 或通过 Rule 构造器直接传入 MustBindWith
//	params.MustBindWith(c, &req, binding.JSON,
//	    params.Rule("Name").Required().Min(1).Max(100),
//	)
//
//	// Rule.Custom 引用已注册的 tag
//	params.MustBindWith(c, &req, binding.JSON,
//	    params.Rule("Mobile").Required().Custom("mobile"),
//	)
//
//	// 查询参数类型转换
//	page := params.QueryIntDefault(c, "page", 1)
//
// 能力：Bind 系列函数、Query 类型转换、Fluent 校验规则构造器、规则注册。
package params
