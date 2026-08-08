package product

import (
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	if Version == "" {
		t.Error("Version should not be empty")
	}

	// 验证版本号格式（应该是 x.x.x 格式）
	parts := strings.Split(Version, ".")
	if len(parts) != 3 {
		t.Errorf("Version should be in x.x.x format, got %s", Version)
	}
	for _, p := range parts {
		if p == "" {
			t.Errorf("Version segment should not be empty, got %s", Version)
		}
	}
}

func TestVersionID(t *testing.T) {
	if VersionID < 0 {
		t.Errorf("VersionID should be non-negative, got %d", VersionID)
	}
}

func TestCommitID(t *testing.T) {
	// 无 git 时回退 "unknown"；有 git 时为 40 位完整 SHA（此处只断言非空）。
	if CommitID == "" {
		t.Error("CommitID should not be empty")
	}
}

func TestBuildTime(t *testing.T) {
	// BuildTime 是 Unix 秒，应大于 0（>= 2000 年）。
	if BuildTime < 946684800 { // 2000-01-01T00:00:00Z
		t.Errorf("BuildTime looks invalid: %d", BuildTime)
	}
}

func TestBuildNoteEscape(t *testing.T) {
	// BuildNote 不应含原始换行（生成时已转义），避免破坏字符串字面量。
	if strings.Contains(BuildNote, "\n") {
		t.Errorf("BuildNote should be escaped, got %q", BuildNote)
	}
}
