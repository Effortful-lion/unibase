package response

import (
	"net/http"

	"github.com/Effortful-lion/unibase/mux/internal/httpx/code"
	"github.com/gin-gonic/gin"
)

// Response 统一响应结构。框架无关，可直接 JSON 序列化。
type Response struct {
	Code    code.BusinessCode `json:"code"`
	Message string            `json:"message"`
	Data    any               `json:"data"`
}

// ResponseOK 写入成功响应（HTTP 200）。
func ResponseOK(c *gin.Context, data any) {
	c.AbortWithStatusJSON(http.StatusOK, &Response{
		Code:    code.OK,
		Message: "success",
		Data:    data,
	})
}

// ResponseFail 写入错误响应。
func ResponseFail(c *gin.Context, httpCode int, bizCode code.BusinessCode, message string) {
	c.AbortWithStatusJSON(httpCode, &Response{
		Code:    bizCode,
		Message: message,
		Data:    nil,
	})
}
