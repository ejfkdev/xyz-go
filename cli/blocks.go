package cli

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"

	errs "github.com/ejfkdev/xyz-go/errors"

	"github.com/ejfkdev/xyz-go/block"
)

// projectBlocks 识别 §12.7 保留信封并做 CLI 投影：文本块逐行打印，二进制块
// 写入系统临时目录并打印路径。非信封返回 (false, nil)，交由普通渲染。
func projectBlocks(w io.Writer, v any) (bool, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return false, nil // 交给普通渲染路径
	}
	env, ok := block.DetectJSON(raw)
	if !ok {
		return false, nil
	}
	for _, it := range env.Content {
		switch it.Type {
		case "text":
			fmt.Fprintln(w, it.Text)
		case "image":
			data, err := base64.StdEncoding.DecodeString(it.Data)
			if err != nil {
				return true, errs.Wrap(errs.KindInvalidInput, err)
			}
			f, err := os.CreateTemp("", "xyz-block-image-*")
			if err != nil {
				return true, err
			}
			path := f.Name()
			if _, err := f.Write(data); err != nil {
				f.Close()
				os.Remove(path)
				return true, err
			}
			if err := f.Close(); err != nil {
				os.Remove(path)
				return true, err
			}
			fmt.Fprintln(w, path)
		}
	}
	return true, nil
}
