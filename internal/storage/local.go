package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Local 把文件保存在本地目录：
//
//	root/orig/...  原始文件，不对外暴露，只能通过带鉴权的下载接口读取
//	root/pub/...   缩略图与预览图，可直接访问
type Local struct {
	root string
}

func newLocal(root string) (*Local, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	for _, d := range []string{filepath.Join(abs, "orig"), filepath.Join(abs, "pub")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	return &Local{root: abs}, nil
}

func (l *Local) path(key string) (string, error) {
	clean := filepath.Clean(strings.TrimPrefix(key, "/"))
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", fmt.Errorf("非法存储路径: %s", key)
	}
	return filepath.Join(l.root, clean), nil
}

func (l *Local) Put(ctx context.Context, key string, data []byte) error {
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func (l *Local) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	p, err := l.path(key)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

func (l *Local) Delete(ctx context.Context, key string) error {
	if key == "" {
		return nil
	}
	p, err := l.path(key)
	if err != nil {
		return err
	}
	err = os.Remove(p)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (l *Local) PublicURL(key string) string {
	if strings.HasPrefix(key, "pub/") {
		return "/media/" + strings.TrimPrefix(key, "pub/")
	}
	return ""
}

func (l *Local) PublicRoot() string {
	return filepath.Join(l.root, "pub")
}

// Root 返回本地存储根目录，用于磁盘空间检查等运维能力。
func (l *Local) Root() string {
	return l.root
}
