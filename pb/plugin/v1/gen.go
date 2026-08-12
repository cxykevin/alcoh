// Package v1 是 alcoh 插件协议的 protobuf Go 绑定（schema 见
// proto/plugin/v1/plugin.proto）。
//
// 重新生成（需要 protoc 与 protoc-gen-go）：
//
//	protoc --plugin=protoc-gen-go=$(go env GOPATH)/bin/protoc-gen-go \
//	  --go_out=. --go_opt=module=github.com/cxykevin/alcoh \
//	  proto/plugin/v1/plugin.proto
package v1
