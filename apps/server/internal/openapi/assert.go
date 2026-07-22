package openapi

import "github.com/gin-gonic/gin"

// 编译期占位：保证 ServerInterface 可被本包引用，并把 oapi-codegen/runtime
// 保留为直接依赖。完整 handler 仍由 internal/api 手写路由承载。
var _ ServerInterface = (*unimplementedServer)(nil)

type unimplementedServer struct{}

func (*unimplementedServer) Login(c *gin.Context)                         { panic("openapi: 未实现") }
func (*unimplementedServer) Logout(c *gin.Context)                        { panic("openapi: 未实现") }
func (*unimplementedServer) Setup(c *gin.Context)                         { panic("openapi: 未实现") }
func (*unimplementedServer) GetSetupStatus(c *gin.Context)                { panic("openapi: 未实现") }
func (*unimplementedServer) ListMediaV2(c *gin.Context, _ ListMediaV2Params) {
	panic("openapi: 未实现")
}
func (*unimplementedServer) GetMediaV2(c *gin.Context, _ string, _ GetMediaV2Params) {
	panic("openapi: 未实现")
}
func (*unimplementedServer) GetTaskV2(c *gin.Context, _ string, _ GetTaskV2Params) {
	panic("openapi: 未实现")
}
// GetHealth 已由本包 GetHealth 实现；stub 仅满足接口编译，勿用于运行时注册。
func (*unimplementedServer) GetHealth(c *gin.Context) { GetHealth(c) }
