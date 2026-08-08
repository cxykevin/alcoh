package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

const jsonRPCVersion = "2.0"

// rpcPending 记录一个正在等待 response 的 JSON-RPC 请求。
type rpcPending struct {
	method string
	result chan rpcResult
}

// rpcResult 是 transport 层收到的响应结果或错误。
type rpcResult struct {
	result []byte
	err    error
}

// Transport 是 JSON-RPC transport 抽象：stdio 子进程与 WebSocket 均实现。
// 生命周期语义与 StdioTransport 一致：Close 后所有 pending 请求以明确错误解除，
// Done 关闭表示 transport 完整回收。
type Transport interface {
	Request(ctx context.Context, method string, params any, result any) error
	Notify(method string, params any) error
	Respond(id RPCID, result any) error
	RespondError(id RPCID, rpcErr RPCError) error
	Close() error
}

// TransportFactory 创建一条已开始读取入站消息的 Transport。
// ClientBackend 通过它隔离 stdio/WebSocket 两种连接的启动逻辑。
type TransportFactory func(ctx context.Context, handler IncomingHandler, onError TransportErrorHandler) (Transport, error)

// RPCID 是 JSON-RPC 请求标识。它保留原始 JSON，因此能无损区分数字与字符串 ID。
type RPCID json.RawMessage

// NewRPCID 把 JSON-RPC ID 复制为独立值。
func NewRPCID(raw json.RawMessage) RPCID {
	return append(RPCID(nil), raw...)
}

// Valid 报告 ID 是否是合法的 JSON-RPC string、number 或 null。
func (id RPCID) Valid() bool {
	if len(id) == 0 || !json.Valid(id) {
		return false
	}
	var value any
	if json.Unmarshal(id, &value) != nil {
		return false
	}
	switch value.(type) {
	case string, float64, nil:
		return true
	default:
		return false
	}
}

// Key 返回可作为 pending-request map 键的带类型编码值。
func (id RPCID) Key() string { return string(bytes.TrimSpace(id)) }

// MarshalJSON 直接输出原始 JSON ID，避免 []byte 被编码为 base64 字符串。
func (id RPCID) MarshalJSON() ([]byte, error) {
	if !id.Valid() {
		return nil, errors.New("invalid JSON-RPC id")
	}
	return append([]byte(nil), id...), nil
}

// String 返回便于诊断的 JSON 值。
func (id RPCID) String() string { return string(id) }

// RPCProtocolError 表示不符合 JSON-RPC 2.0 的输入。
type RPCProtocolError struct {
	Message string
}

func (e *RPCProtocolError) Error() string { return "JSON-RPC protocol error: " + e.Message }

// RPCRemoteError 表示 agent 返回的 JSON-RPC error。
type RPCRemoteError struct {
	Method   string
	RPCError RPCError
}

func (e *RPCRemoteError) Error() string {
	if e.Method == "" {
		return fmt.Sprintf("RPC error %d: %s", e.RPCError.Code, e.RPCError.Message)
	}
	return fmt.Sprintf("RPC %s error %d: %s", e.Method, e.RPCError.Code, e.RPCError.Message)
}

// IncomingMessage 是解析后但尚未业务分派的 JSON-RPC 入站消息。
type IncomingMessage struct {
	Request      *RPCRequest
	Response     *RPCResponse
	Notification *RPCNotification
}

// DecodeIncoming 严格区分 request、response 与 notification。
func DecodeIncoming(line []byte) (IncomingMessage, error) {
	var raw struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  *string         `json:"method"`
		Params  json.RawMessage `json:"params"`
		Result  json.RawMessage `json:"result"`
		Error   *RPCError       `json:"error"`
	}
	if err := json.Unmarshal(line, &raw); err != nil {
		return IncomingMessage{}, fmt.Errorf("decode JSON: %w", err)
	}
	if raw.JSONRPC != jsonRPCVersion {
		return IncomingMessage{}, &RPCProtocolError{Message: "jsonrpc must be 2.0"}
	}
	if raw.Method != nil {
		if *raw.Method == "" {
			return IncomingMessage{}, &RPCProtocolError{Message: "method must not be empty"}
		}
		if len(raw.ID) == 0 {
			return IncomingMessage{Notification: &RPCNotification{JSONRPC: raw.JSONRPC, Method: *raw.Method, Params: raw.Params}}, nil
		}
		id := NewRPCID(raw.ID)
		if !id.Valid() || bytes.Equal(bytes.TrimSpace(raw.ID), []byte("null")) {
			return IncomingMessage{}, &RPCProtocolError{Message: "request id must be a string or number"}
		}
		return IncomingMessage{Request: &RPCRequest{JSONRPC: raw.JSONRPC, ID: id, Method: *raw.Method, Params: raw.Params}}, nil
	}
	if len(raw.ID) == 0 {
		return IncomingMessage{}, &RPCProtocolError{Message: "response missing id"}
	}
	id := NewRPCID(raw.ID)
	if !id.Valid() {
		return IncomingMessage{}, &RPCProtocolError{Message: "invalid response id"}
	}
	if len(raw.Result) != 0 && raw.Error != nil {
		return IncomingMessage{}, &RPCProtocolError{Message: "response cannot contain both result and error"}
	}
	if len(raw.Result) == 0 && raw.Error == nil {
		return IncomingMessage{}, &RPCProtocolError{Message: "response must contain result or error"}
	}
	return IncomingMessage{Response: &RPCResponse{JSONRPC: raw.JSONRPC, ID: id, Result: raw.Result, Error: raw.Error}}, nil
}

// MarshalRequest 生成一条 JSON-RPC request。params 必须是可编码 JSON 值。
func MarshalRequest(id RPCID, method string, params any) ([]byte, error) {
	if !id.Valid() || bytes.Equal(bytes.TrimSpace(id), []byte("null")) {
		return nil, errors.New("invalid JSON-RPC request id")
	}
	if method == "" {
		return nil, errors.New("JSON-RPC method is empty")
	}
	return json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		ID      RPCID  `json:"id"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{jsonRPCVersion, id, method, params})
}

// MarshalNotification 生成一条 JSON-RPC notification。
func MarshalNotification(method string, params any) ([]byte, error) {
	if method == "" {
		return nil, errors.New("JSON-RPC method is empty")
	}
	return json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{jsonRPCVersion, method, params})
}

// MarshalResult 用原 server-request ID 编码成功 response。
func MarshalResult(id RPCID, result any) ([]byte, error) {
	if !id.Valid() {
		return nil, errors.New("invalid JSON-RPC response id")
	}
	return json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		ID      RPCID  `json:"id"`
		Result  any    `json:"result"`
	}{jsonRPCVersion, id, result})
}

// MarshalError 用原 server-request ID 编码失败 response。
func MarshalError(id RPCID, rpcErr RPCError) ([]byte, error) {
	if !id.Valid() {
		return nil, errors.New("invalid JSON-RPC response id")
	}
	return json.Marshal(struct {
		JSONRPC string   `json:"jsonrpc"`
		ID      RPCID    `json:"id"`
		Error   RPCError `json:"error"`
	}{jsonRPCVersion, id, rpcErr})
}
