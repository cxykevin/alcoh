package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// 连接密钥文件（conn.keys）：按规范化连接 URL 记住 WebSocket 认证密钥，
// 下次启动同一连接时直接复用，认证失败才重新询问。

const connKeysVersion = 1

// connKeysFile 是 conn.keys 的磁盘格式。键为不含认证信息的规范化连接 URL
// （scheme://host:port/端点路径），值为明文密钥；文件权限 0600。
type connKeysFile struct {
	Version int               `json:"version"`
	Keys    map[string]string `json:"keys"`
}

// ConnKeysPath 返回 conn.keys 的路径：与 alcoh 本地配置文件同目录。
func ConnKeysPath() (string, error) {
	p, err := Path()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(p), "conn.keys"), nil
}

// LoadConnKey 返回 conn 对应的已保存密钥；没有记录时返回空串。
func LoadConnKey(conn string) string {
	conn = strings.TrimSpace(conn)
	if conn == "" {
		return ""
	}
	f, ok := loadConnKeysFile()
	if !ok {
		return ""
	}
	return f.Keys[conn]
}

// SaveConnKey 记住 conn 对应的密钥；key 为空时删除该条记录。
func SaveConnKey(conn, key string) error {
	conn = strings.TrimSpace(conn)
	if conn == "" {
		return errors.New("connection identity is empty")
	}
	f, _ := loadConnKeysFile()
	if f.Keys == nil {
		f.Keys = make(map[string]string)
	}
	f.Version = connKeysVersion
	key = strings.TrimSpace(key)
	if key == "" {
		delete(f.Keys, conn)
	} else {
		f.Keys[conn] = key
	}
	return saveConnKeysFile(f)
}

func loadConnKeysFile() (connKeysFile, bool) {
	f := connKeysFile{Version: connKeysVersion, Keys: make(map[string]string)}
	path, err := ConnKeysPath()
	if err != nil {
		return f, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return f, false
	}
	var got connKeysFile
	if err := json.Unmarshal(data, &got); err != nil || got.Version != connKeysVersion {
		return f, false
	}
	if got.Keys == nil {
		got.Keys = make(map[string]string)
	}
	return got, true
}

func saveConnKeysFile(f connKeysFile) error {
	if f.Version == 0 {
		f.Version = connKeysVersion
	}
	path, err := ConnKeysPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".conn.keys-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	// 密钥文件只允许当前用户读写。
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
