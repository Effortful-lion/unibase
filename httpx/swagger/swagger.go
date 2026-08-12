package swagger

import (
	"path"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// Setup 在 router 上注册 Swagger UI 路由。
//
// 注册路径为 basePath + "/swagger/*any"（如 "/api/v1/swagger/index.html"）。
// specURL 指定 Swagger 文档的 JSON 路径。
//
// 前置条件：项目中已通过 swag init 生成 docs 包。
//
// 示例：
//
//	import _ "yourProject/docs" // 确保 docs 包被初始化
//
//	r := gin.Default()
//	swagger.Setup(r, "/api/v1", "/swagger/doc.json")
//	// 访问 http://host/api/v1/swagger/index.html
func Setup(r *gin.Engine, basePath, specURL string) {
	mountPath := path.Join(basePath, "/swagger/*any")
	r.GET(mountPath, ginSwagger.WrapHandler(
		swaggerFiles.Handler,
		ginSwagger.URL(specURL),
	))
}
