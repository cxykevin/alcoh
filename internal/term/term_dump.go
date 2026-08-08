package term

import "io"

// Dump 返回一个把帧输出到 w、无输入、固定尺寸的 Terminal。
// 用于无 TTY 的冒烟测试（--dump N 模式）：只渲染不交互。
func Dump(w io.Writer, width, height int) Terminal {
	return &dumpTerm{out: w, w: width, h: height}
}

type dumpTerm struct {
	out io.Writer
	w   int
	h   int
}

func (d *dumpTerm) EnterRaw() error { return nil }
func (d *dumpTerm) ExitRaw() error  { return nil }
func (d *dumpTerm) Size() (int, int) {
	return d.w, d.h
}
func (d *dumpTerm) Events() <-chan Event { return nil }
func (d *dumpTerm) Write(p []byte) error {
	_, err := d.out.Write(p)
	return err
}
func (d *dumpTerm) CopyToClipboard(text string) error { return nil }
