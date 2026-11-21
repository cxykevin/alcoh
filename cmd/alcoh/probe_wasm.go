//go:build js || wasip1

package main

import "errors"

// probeWSAuth 在 wasm 构建里不可用：浏览器/WASI 环境没有可用的 WebSocket
// 握手预检，远端连接只能靠调用方传入的密钥直接尝试。
func probeWSAuth(string) (int, error) {
	return 0, errors.New("WebSocket precheck is not supported in the wasm build")
}

// isWSAuthFailure 在 wasm 构建里恒为 false：预检永远失败，不进入密钥询问循环。
func isWSAuthFailure(int) bool { return false }
