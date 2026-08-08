// Package product 提供应用产品信息常量与版本号封装。
//
// 该包定义版本号、构建信息等产品级元数据。build.go 由 go generate 自动生成：
//
//	cd product && go generate
//
// 版本号取自最新 git tag（无 tag 时为 0.0.0），CommitID 为当前提交，
// BuildTime 为生成时刻 Unix 秒，BuildNote 读环境变量 ALCOH_BUILD_NOTE。
package product
