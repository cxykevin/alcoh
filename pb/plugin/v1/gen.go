// Package v1 是 alcoh 插件协议的 protobuf Go 绑定。
//
// 本目录的 plugin.pb.go 由 go generate 生成（生成器在 internal/plugingen，
// 纯 Go 管线，无需安装 protoc），生成文件不入仓库：
//
//	go generate ./...        # 或 go generate pb/plugin/v1/gen.go
//
// 协议 schema 见 proto/plugin/v1/plugin.proto。
//
//go:generate go run github.com/cxykevin/alcoh/internal/plugingen
package v1
