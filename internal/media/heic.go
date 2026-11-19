package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ErrHEICUnavailable 表示当前部署环境没有可用的 HEIC 转换器。
var ErrHEICUnavailable = errors.New("服务器未配置 HEIC 转换器")

type heicConverter struct {
	kind string
	path string
}

var (
	heicOnce sync.Once
	heicConv *heicConverter
)

// HEICConverterName 返回当前使用的 HEIC 转换器名称，供启动日志展示。
func HEICConverterName() string {
	c := getHEICConverter()
	if c == nil {
		return ""
	}
	return c.kind + " (" + c.path + ")"
}

func getHEICConverter() *heicConverter {
	heicOnce.Do(func() {
		heicConv = detectHEICConverter()
	})
	return heicConv
}

func detectHEICConverter() *heicConverter {
	// 人工指定优先，例如 PHOTOLIVE_HEIC_CONVERTER=C:\tools\ffmpeg.exe
	if custom := strings.TrimSpace(os.Getenv("PHOTOLIVE_HEIC_CONVERTER")); custom != "" {
		path := custom
		if !strings.ContainsAny(custom, `/\`) {
			if p, err := exec.LookPath(custom); err == nil {
				path = p
			}
		}
		kind := converterKind(filepath.Base(path))
		if kind == "" {
			if p, err := exec.LookPath(filepath.Base(path)); err == nil {
				path = p
			}
		}
		if kind != "" {
			return &heicConverter{kind: kind, path: path}
		}
	}

	// 依次探测常见工具。注意：Windows 自带的 convert.exe 是分区工具，不能使用。
	for _, name := range []string{"ffmpeg", "magick", "heif-convert", "sips"} {
		if p, err := exec.LookPath(name); err == nil {
			return &heicConverter{kind: name, path: p}
		}
	}
	return nil
}

func converterKind(base string) string {
	b := strings.ToLower(base)
	switch {
	case strings.Contains(b, "ffmpeg"):
		return "ffmpeg"
	case strings.Contains(b, "magick"):
		return "magick"
	case strings.Contains(b, "heif-convert"):
		return "heif-convert"
	case strings.Contains(b, "sips"):
		return "sips"
	}
	return ""
}

// convertHEIC 调用外部工具把 HEIC/HEIF 转成 JPEG 字节。
func convertHEIC(data []byte) ([]byte, error) {
	c := getHEICConverter()
	if c == nil {
		return nil, fmt.Errorf("%w：请在服务器安装 ffmpeg，或设置 PHOTOLIVE_HEIC_CONVERTER 指向转换工具", ErrHEICUnavailable)
	}

	dir, err := os.MkdirTemp("", "photolive-heic-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	in := filepath.Join(dir, "input.heic")
	out := filepath.Join(dir, "output.jpg")
	if err := os.WriteFile(in, data, 0o600); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var args []string
	switch c.kind {
	case "ffmpeg":
		args = []string{"-hide_banner", "-loglevel", "error", "-y", "-i", in, "-frames:v", "1", "-q:v", "2", out}
	case "magick":
		args = []string{in, "-auto-orient", "-quality", "95", out}
	case "heif-convert":
		args = []string{"-q", "95", in, out}
	case "sips":
		args = []string{"-s", "format", "jpeg", "-s", "formatOptions", "95", in, "--out", out}
	default:
		return nil, fmt.Errorf("%w：未知的转换器 %s", ErrHEICUnavailable, c.kind)
	}

	cmd := exec.CommandContext(ctx, c.path, args...)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, errors.New("HEIC 转换超时，请重试")
	}
	if err != nil {
		return nil, fmt.Errorf("HEIC 转换失败: %s", summarizeOutput(output, err))
	}

	jpg, err := os.ReadFile(out)
	if err != nil || len(jpg) == 0 {
		// heif-convert 在多图容器下会输出 output-1.jpg，这里做一次兜底查找。
		matches, _ := filepath.Glob(filepath.Join(dir, "output*.jpg"))
		if len(matches) > 0 {
			jpg, err = os.ReadFile(matches[0])
		}
	}
	if err != nil || len(jpg) == 0 {
		return nil, errors.New("HEIC 转换后没有生成有效图片")
	}
	if !isJPEG(jpg) {
		return nil, errors.New("HEIC 转换结果不是有效的 JPEG")
	}
	return jpg, nil
}

func summarizeOutput(output []byte, err error) string {
	msg := strings.TrimSpace(string(output))
	msg = strings.ReplaceAll(msg, "\r", " ")
	msg = strings.ReplaceAll(msg, "\n", " ")
	if len(msg) > 180 {
		msg = msg[:180] + "…"
	}
	if msg == "" {
		return err.Error()
	}
	return msg
}

func isJPEG(b []byte) bool {
	return len(b) > 3 && bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF})
}
