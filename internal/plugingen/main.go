// Package main 实现 alcoh 插件协议 protobuf 绑定的生成器。
//
// 由 pb/plugin/v1/gen.go 的 go:generate 指令驱动（cwd 为 pb/plugin/v1）。
// 纯 Go 管线（无需安装 protoc）：protoparse 解析 proto/plugin/v1/plugin.proto
// 构造 CodeGeneratorRequest，经 go run 以插件协议驱动 protoc-gen-go，
// 输出写入 pb/plugin/v1/plugin.pb.go（gitignore，不进仓库）。
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jhump/protoreflect/desc/protoparse"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/pluginpb"
)

const (
	protoFile = "proto/plugin/v1/plugin.proto"
	output    = "plugin.pb.go"
)

func main() {
	// go:generate 以本文件所在目录（pb/plugin/v1）为工作目录运行。
	root := filepath.Join("..", "..", "..")
	parser := protoparse.Parser{ImportPaths: []string{root}, IncludeSourceCodeInfo: true}
	fds, err := parser.ParseFiles(protoFile)
	if err != nil {
		fatalf("解析 %s 失败: %v", protoFile, err)
	}
	req := &pluginpb.CodeGeneratorRequest{
		FileToGenerate: []string{protoFile},
		// module 参数使输出文件路径映射到 pb/plugin/v1/plugin.pb.go。
		Parameter: proto.String("module=github.com/cxykevin/alcoh"),
	}
	for _, fd := range fds {
		req.ProtoFile = append(req.ProtoFile, fd.AsFileDescriptorProto())
	}
	reqBytes, err := proto.Marshal(req)
	if err != nil {
		fatalf("序列化 CodeGeneratorRequest 失败: %v", err)
	}

	cmd := exec.Command("go", "run", "google.golang.org/protobuf/cmd/protoc-gen-go")
	cmd.Stdin = bytes.NewReader(reqBytes)
	out, err := cmd.Output()
	if err != nil {
		fatalf("运行 protoc-gen-go 失败: %v", err)
	}
	var resp pluginpb.CodeGeneratorResponse
	if err := proto.Unmarshal(out, &resp); err != nil {
		fatalf("解析 CodeGeneratorResponse 失败: %v", err)
	}
	if resp.Error != nil {
		fatalf("protoc-gen-go 报错: %s", *resp.Error)
	}
	if len(resp.File) != 1 {
		fatalf("protoc-gen-go 返回 %d 个文件，预期 1 个", len(resp.File))
	}
	if err := os.WriteFile(output, []byte(resp.File[0].GetContent()), 0o644); err != nil {
		fatalf("写入 %s 失败: %v", output, err)
	}
	fmt.Printf("generated %s\n", output)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "plugingen: "+format+"\n", args...)
	os.Exit(1)
}
