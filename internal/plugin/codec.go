package plugin

import (
	"encoding/json"
	"errors"
	"fmt"

	"google.golang.org/protobuf/proto"
)

// envelope 是 JSON-RPC params/result 的载体：protobuf 二进制以 base64 内嵌
// 在 JSON 中（encoding/json 对 []byte 自动 base64 编解码），保持 JSON-RPC
// envelope 纯 JSON、payload 为 protobuf。
type envelope struct {
	Data []byte `json:"data"`
}

// marshalEnvelope 把 protobuf 消息编码为 JSON-RPC params/result 的 JSON 字节。
func marshalEnvelope(msg proto.Message) ([]byte, error) {
	data, err := proto.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshal protobuf: %w", err)
	}
	return json.Marshal(envelope{Data: data})
}

// unmarshalEnvelope 从 JSON-RPC params/result 的 JSON 字节解码 envelope。
func unmarshalEnvelope(line []byte, env *envelope) error {
	if len(line) == 0 {
		return errors.New("empty envelope")
	}
	return json.Unmarshal(line, env)
}

// decodeResult 把 JSON-RPC result 字节解码为 protobuf 消息；null/空结果
// 返回 (nil, nil)，调用方决定是否解析（对应 Empty 之类无字段消息）。
func decodeResult(line []byte, msg proto.Message) error {
	var env envelope
	if err := unmarshalEnvelope(line, &env); err != nil {
		return fmt.Errorf("decode envelope: %w", err)
	}
	if len(env.Data) == 0 {
		return nil
	}
	return proto.Unmarshal(env.Data, msg)
}

// decodeParams 把 JSON-RPC params 字节解码为 protobuf 消息。
func decodeParams(line []byte, msg proto.Message) error {
	var env envelope
	if err := unmarshalEnvelope(line, &env); err != nil {
		return fmt.Errorf("decode envelope: %w", err)
	}
	return proto.Unmarshal(env.Data, msg)
}
