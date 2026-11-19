package storage

import (
	"context"
	"errors"
	"io"
)

// Storage 抽象照片的持久化位置。本地磁盘与对象存储（OSS）都实现该接口，
// 上层只通过 key 读写，不关心具体介质。
type Storage interface {
	Put(ctx context.Context, key string, data []byte) error
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	// PublicURL 返回可被浏览器直接访问的地址；原始文件返回空串。
	PublicURL(key string) string
	// PublicRoot 返回本地公开目录；对象存储返回空串。
	PublicRoot() string
}

func NewLocal(root string) (Storage, error) {
	return newLocal(root)
}

// NewOSS 预留给对象存储接入。实现该接口后即可不修改业务代码切换存储。
func NewOSS(bucket, endpoint, accessKey, secretKey string) (Storage, error) {
	return nil, errors.New("OSS 存储尚未接入：请实现 storage.Storage 接口后在 storage.NewOSS 中返回实例")
}
