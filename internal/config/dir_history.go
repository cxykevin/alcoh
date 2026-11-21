package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// DirHistoryLimit 是每个连接（历史 key）最多保留的常用目录数。
const DirHistoryLimit = 64

const dirHistoryVersion = 1

// dirHistoryFile 是 dir.history 的磁盘格式：按连接 URL 分桶，每桶保存最近
// 使用的目录（新的在前）。连接 key 由 cmd/alcoh 根据启动参数计算；无参且无
// 环境变量启动时统一为 "localhost"（忽略端口）。
type dirHistoryFile struct {
	Version int                 `json:"version"`
	Buckets map[string][]string `json:"buckets"`
}

// DirHistoryPath 返回 dir.history 的路径：与 alcoh 本地配置文件同目录。
func DirHistoryPath() (string, error) {
	p, err := Path()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(p), "dir.history"), nil
}

// LoadDirHistory 读取指定连接 key 的常用目录。文件不存在、损坏或版本不兼容
// 时返回空列表；结果已去重并截断到 DirHistoryLimit。
func LoadDirHistory(key string) []string {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	f, ok := loadDirHistoryFile()
	if !ok {
		return nil
	}
	return normalizeDirHistory(f.Buckets[key])
}

// RecordDirHistory 把 dir 记录到 key 对应的桶：去重、置顶、截断到
// DirHistoryLimit 并原子写回。写入失败时仍返回更新后的内存列表，调用方
// 不因历史文件不可写而阻塞目录切换。
func RecordDirHistory(key, dir string) []string {
	key = strings.TrimSpace(key)
	dir = strings.TrimSpace(dir)
	if key == "" || dir == "" {
		return nil
	}
	f, _ := loadDirHistoryFile()
	if f.Buckets == nil {
		f.Buckets = make(map[string][]string)
	}
	dirs := normalizeDirHistory(append([]string{dir}, f.Buckets[key]...))
	f.Version = dirHistoryVersion
	f.Buckets[key] = dirs
	_ = saveDirHistoryFile(f)
	return append([]string(nil), dirs...)
}

// SaveDirHistory 覆盖指定连接 key 的常用目录（测试与将来的清理入口使用）。
func SaveDirHistory(key string, dirs []string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("dir history key is empty")
	}
	f, _ := loadDirHistoryFile()
	if f.Buckets == nil {
		f.Buckets = make(map[string][]string)
	}
	f.Version = dirHistoryVersion
	f.Buckets[key] = normalizeDirHistory(dirs)
	return saveDirHistoryFile(f)
}

func loadDirHistoryFile() (dirHistoryFile, bool) {
	f := dirHistoryFile{Version: dirHistoryVersion, Buckets: make(map[string][]string)}
	path, err := DirHistoryPath()
	if err != nil {
		return f, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return f, false
	}
	var got dirHistoryFile
	if err := json.Unmarshal(data, &got); err != nil || got.Version != dirHistoryVersion {
		return f, false
	}
	if got.Buckets == nil {
		got.Buckets = make(map[string][]string)
	}
	return got, true
}

func saveDirHistoryFile(f dirHistoryFile) error {
	if f.Version == 0 {
		f.Version = dirHistoryVersion
	}
	path, err := DirHistoryPath()
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
	tmp, err := os.CreateTemp(dir, ".dir.history-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
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

// normalizeDirHistory 去空、去重（保留首次出现，即最新的在前）、截断。
func normalizeDirHistory(dirs []string) []string {
	out := make([]string, 0, min(len(dirs), DirHistoryLimit))
	seen := make(map[string]bool, len(dirs))
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		out = append(out, dir)
		if len(out) >= DirHistoryLimit {
			break
		}
	}
	return out
}
