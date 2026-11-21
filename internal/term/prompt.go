package term

import (
	"io"
	"strings"
)

// readSecretLine 逐字节读取一行（直到 \n），去掉行尾的 \r。
//
// 不用 bufio.Reader：它会把换行之后的字节读进内部缓冲，而后续 TUI 的输入
// 解析直接读 os.Stdin，缓冲里的按键就丢了。这里只消费到换行为止。
func readSecretLine(r io.Reader) (string, error) {
	var sb strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			switch buf[0] {
			case '\n':
				return sb.String(), nil
			case '\r':
				// 忽略（Windows 的 CRLF 与粘贴的换行）
			default:
				sb.WriteByte(buf[0])
			}
		}
		if err != nil {
			return sb.String(), err
		}
	}
}
