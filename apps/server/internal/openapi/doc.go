// Package openapi 承载据仓库根 api/openapi.yaml 由 oapi-codegen 生成的类型与 Gin ServerInterface。
//
// 契约（api/openapi.yaml）是机器可读真源（FR2-071）。生成产物 api.gen.go 不得手改；
// 契约变更后在 apps/server 下运行 `task gen` 重生成，并用 `task gen:check` 防漂移。
//
// 本包与 internal/api / internal/web 并存：现有 Handler 仍手写挂载路由。
// 已落地：GetHealth 由 web 单挂 /health（契约类型响应）；其余 ServerInterface 方法仍为 stub，
// 后续可逐步实现并切换注册，禁止在现网直接 RegisterHandlers 全量挂载（会与手写 auth 冲突）。
package openapi

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0 -config cfg.yaml ../../../api/openapi.yaml
