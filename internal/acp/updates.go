package acp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DecodeSessionUpdate 将 session/update notification 的 params 转为面向 TUI 的事件。
// ACP v2 使用 sessionUpdate 作为 update 判别字段；为兼容部分实现，也接受 type。
func DecodeSessionUpdate(params json.RawMessage) (Event, error) {
	var envelope struct {
		SessionID string          `json:"sessionId"`
		Update    json.RawMessage `json:"update"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil {
		return nil, fmt.Errorf("decode session/update params: %w", err)
	}
	if envelope.SessionID == "" {
		return nil, fmt.Errorf("session/update missing sessionId")
	}
	if len(envelope.Update) == 0 || string(envelope.Update) == "null" {
		return nil, fmt.Errorf("session/update missing update")
	}
	return DecodeSessionUpdatePayload(envelope.SessionID, envelope.Update)
}

// DecodeSessionUpdatePayload 解码单个 session update payload。
func DecodeSessionUpdatePayload(sessionID string, raw json.RawMessage) (Event, error) {
	var header struct {
		SessionUpdate string `json:"sessionUpdate"`
		Type          string `json:"type"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return nil, fmt.Errorf("decode session update header: %w", err)
	}
	kind := header.SessionUpdate
	if kind == "" {
		kind = header.Type
	}
	switch kind {
	case "user_message_chunk", "agent_message_chunk", "agent_thought_chunk", "user_thought_chunk":
		var update struct {
			MessageID string          `json:"messageId"`
			Content   json.RawMessage `json:"content"`
			Text      string          `json:"text"`
		}
		if err := json.Unmarshal(raw, &update); err != nil {
			return nil, err
		}
		text := update.Text
		if text == "" {
			t, err := decodeChunkContentText(update.Content)
			if err != nil {
				return nil, err
			}
			text = t
		}
		return &MessageChunkEvent{
			SessionID: sessionID,
			MessageID: update.MessageID,
			IsUser:    kind == "user_message_chunk" || kind == "user_thought_chunk",
			IsThought: kind == "agent_thought_chunk" || kind == "user_thought_chunk",
			Text:      text,
		}, nil
	case "user_message", "agent_message", "agent_thought", "user_thought":
		var update struct {
			MessageID string          `json:"messageId"`
			Content   json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(raw, &update); err != nil {
			return nil, err
		}
		message, err := decodeMessage(update.MessageID, update.Content)
		if err != nil {
			return nil, err
		}
		return &MessageUpdateEvent{
			SessionID: sessionID,
			Message:   message,
			IsUser:    kind == "user_message" || kind == "user_thought",
			IsThought: kind == "agent_thought" || kind == "user_thought",
		}, nil
	case "state_update":
		var update StateUpdate
		if err := json.Unmarshal(raw, &update); err != nil {
			return nil, err
		}
		ev := &StateChangeEvent{SessionID: sessionID, State: update.State, StopReason: update.StopReason}
		if update.ErrorMsg != nil && *update.ErrorMsg != "" {
			ev.Notice = update.ErrorMsg
		}
		return ev, nil
	case "tool_call_update":
		return decodeToolCallUpdate(sessionID, raw)
	case "tool_call_content_chunk":
		// content chunk 不是整体替换：原始值交给 model 按工具 ID 聚合。
		var update struct {
			ToolCallID string          `json:"toolCallId"`
			Content    json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(raw, &update); err != nil {
			return nil, err
		}
		content, err := decodeToolContentList(update.Content)
		if err != nil {
			return nil, err
		}
		return &ToolCallUpdateEvent{SessionID: sessionID, ToolCallID: update.ToolCallID, Content: content, ContentAppend: true}, nil
	case "plan_update":
		var update struct {
			Plan Plan `json:"plan"`
		}
		if err := json.Unmarshal(raw, &update); err != nil {
			return nil, err
		}
		return &PlanUpdateEvent{SessionID: sessionID, Plan: update.Plan}, nil
	case "usage_update":
		var update Usage
		if err := json.Unmarshal(raw, &update); err != nil {
			return nil, err
		}
		return &UsageUpdateEvent{SessionID: sessionID, Used: update.Used, Size: update.Size, Cost: update.Cost}, nil
	case "alk.cxykevin.top/terminal_update":
		var update struct {
			UpdateType string         `json:"updateType"`
			Terminals  []TerminalInfo `json:"terminals"`
			Terminal   TerminalInfo   `json:"terminal"`
			TerminalID string         `json:"terminalId"`
			ID         string         `json:"id"`
			Title      string         `json:"title"`
			Command    string         `json:"command"`
			Status     string         `json:"status"`
			Content    string         `json:"content"`
			Output     string         `json:"output"`
			Chunk      string         `json:"chunk"`
			Text       string         `json:"text"`
		}
		if err := json.Unmarshal(raw, &update); err != nil {
			return nil, err
		}
		if update.TerminalID == "" {
			update.TerminalID = update.ID
		}
		output := update.Content
		if output == "" {
			output = update.Output
		}
		if output == "" {
			output = update.Chunk
		}
		if output == "" {
			output = update.Text
		}
		if update.Terminal.TerminalID == "" {
			update.Terminal.TerminalID = update.TerminalID
		}
		if update.Terminal.TerminalID == "" {
			update.Terminal.TerminalID = update.ID
		}
		if update.TerminalID == "" {
			update.TerminalID = update.Terminal.TerminalID
		}
		if update.TerminalID == "" {
			update.TerminalID = update.ID
		}
		if update.Terminal.Command == "" {
			update.Terminal.Command = update.Command
		}
		if update.Terminal.Status == "" {
			update.Terminal.Status = update.Status
		}
		return &TerminalUpdateEvent{SessionID: sessionID, TerminalID: update.TerminalID, Title: update.Title, Command: update.Command, Status: update.Status, Output: output, UpdateType: update.UpdateType, Terminals: update.Terminals, Terminal: update.Terminal, Raw: append(json.RawMessage(nil), raw...)}, nil
	case "available_commands_update":
		var update struct {
			AvailableCommands json.RawMessage `json:"availableCommands"`
			Commands          json.RawMessage `json:"commands"`
		}
		if err := json.Unmarshal(raw, &update); err != nil {
			return nil, err
		}
		payload := update.AvailableCommands
		if len(payload) == 0 {
			payload = update.Commands
		}
		var wire []json.RawMessage
		if len(payload) != 0 && string(payload) != "null" {
			if err := json.Unmarshal(payload, &wire); err != nil {
				return nil, err
			}
		}
		commands := make([]AvailableCommand, 0, len(wire))
		for _, entry := range wire {
			var command AvailableCommand
			if err := json.Unmarshal(entry, &command); err != nil {
				return nil, err
			}
			command.Raw = append(json.RawMessage(nil), entry...)
			commands = append(commands, command)
		}
		return &CommandsUpdateEvent{SessionID: sessionID, Commands: commands, Raw: append(json.RawMessage(nil), raw...)}, nil
	case "config_option_update":
		return decodeConfigOptionUpdate(sessionID, raw)
	case "session_info_update":
		var update struct {
			Title     *string `json:"title"`
			Model     *string `json:"model"`
			CWD       *string `json:"cwd"`
			UpdatedAt *string `json:"updatedAt"`
		}
		if err := json.Unmarshal(raw, &update); err != nil {
			return nil, err
		}
		return &SessionInfoUpdateEvent{SessionID: sessionID, Title: update.Title, Model: update.Model, CWD: update.CWD, UpdatedAt: update.UpdatedAt, Raw: append(json.RawMessage(nil), raw...)}, nil
	case "other", "":
		return &UnknownSessionUpdateEvent{SessionID: sessionID, Discriminator: kind, Raw: append(json.RawMessage(nil), raw...)}, nil
	default:
		return &UnknownSessionUpdateEvent{SessionID: sessionID, Discriminator: kind, Raw: append(json.RawMessage(nil), raw...)}, nil
	}
}

// decodeChunkContentText 从消息 chunk 的 content 中提取文本。
// wire 上 content 可能是 ContentBlock 对象（alkaid0 实时推送）、字符串或 ContentBlock 数组。
func decodeChunkContentText(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}
	var blocks []ContentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var sb strings.Builder
		for i := range blocks {
			if blocks[i].Text != nil {
				sb.WriteString(*blocks[i].Text)
			}
		}
		return sb.String(), nil
	}
	var block ContentBlock
	if err := json.Unmarshal(raw, &block); err == nil && block.Text != nil {
		return *block.Text, nil
	}
	return "", fmt.Errorf("decode chunk content: %s", truncateRaw(raw, 80))
}

// truncateRaw 截断超长原始 JSON 用于错误诊断。
func truncateRaw(raw json.RawMessage, max int) string {
	if len(raw) <= max {
		return string(raw)
	}
	return string(raw[:max]) + "…"
}

func decodeMessage(id string, raw json.RawMessage) (Message, error) {
	message := Message{MessageID: id}
	if len(raw) == 0 {
		return message, nil
	}
	message.ContentSet = true
	if string(raw) == "null" {
		return message, nil
	}
	if err := json.Unmarshal(raw, &message.Content); err != nil {
		return Message{}, fmt.Errorf("decode message content: %w", err)
	}
	for i := range message.Content {
		message.Content[i].Raw = append(json.RawMessage(nil), message.Content[i].Raw...)
	}
	return message, nil
}

func decodeToolCallUpdate(sessionID string, raw json.RawMessage) (Event, error) {
	var wire struct {
		ToolCallID string             `json:"toolCallId"`
		Title      *string            `json:"title"`
		Kind       *ToolCallKind      `json:"kind"`
		Status     *ToolCallStatus    `json:"status"`
		Content    json.RawMessage    `json:"content"`
		Locations  []ToolCallLocation `json:"locations"`
		RawInput   json.RawMessage    `json:"rawInput"`
		RawOutput  json.RawMessage    `json:"rawOutput"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, err
	}
	content, err := decodeToolContentList(wire.Content)
	if err != nil {
		return nil, err
	}
	return &ToolCallUpdateEvent{
		SessionID: sessionID, ToolCallID: wire.ToolCallID, Status: wire.Status,
		Title: wire.Title, Kind: wire.Kind, Content: content, Locations: wire.Locations,
		RawInput: wire.RawInput, RawOutput: wire.RawOutput, ContentSet: len(wire.Content) != 0,
	}, nil
}

// decodeConfigOptionUpdate 提取 ACP v2 config 项（configId/name/category/type/currentValue/options）。
// 兼容三种 wire 形状：configOptions 数组、options 数组、单个 option 对象；总是保留 Raw。
func decodeConfigOptionUpdate(sessionID string, raw json.RawMessage) (Event, error) {
	rawCopy := append(json.RawMessage(nil), raw...)
	var wire struct {
		ConfigOptions []json.RawMessage `json:"configOptions"`
		Options       []json.RawMessage `json:"options"`
		Option        json.RawMessage   `json:"option"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, err
	}
	entries := wire.ConfigOptions
	if len(entries) == 0 {
		entries = wire.Options
	}
	if len(entries) == 0 && len(wire.Option) > 0 && string(wire.Option) != "null" {
		entries = []json.RawMessage{wire.Option}
	}
	options := make([]ConfigOption, 0, len(entries))
	for _, entry := range entries {
		var option ConfigOption
		if err := json.Unmarshal(entry, &option); err != nil {
			return nil, err
		}
		option.Raw = append(json.RawMessage(nil), entry...)
		options = append(options, option)
	}
	return &ConfigOptionUpdateEvent{SessionID: sessionID, Options: options, Raw: rawCopy}, nil
}

func decodeToolContentList(raw json.RawMessage) ([]ToolCallContent, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var list []ToolCallContent
	if err := json.Unmarshal(raw, &list); err != nil {
		var one ToolCallContent
		if err := json.Unmarshal(raw, &one); err != nil {
			return nil, fmt.Errorf("decode tool content: %w", err)
		}
		return []ToolCallContent{one}, nil
	}
	return list, nil
}

// UnmarshalJSON 保留未知 content block 的完整原始 JSON。
func (b *ContentBlock) UnmarshalJSON(data []byte) error {
	type contentBlockAlias ContentBlock
	var value contentBlockAlias
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*b = ContentBlock(value)
	b.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// UnmarshalJSON 保留未知 tool content 的完整原始 JSON。
func (c *ToolCallContent) UnmarshalJSON(data []byte) error {
	type toolContentAlias ToolCallContent
	var value toolContentAlias
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*c = ToolCallContent(value)
	c.Raw = append(json.RawMessage(nil), data...)
	return nil
}
