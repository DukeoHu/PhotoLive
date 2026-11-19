package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"time"

	"photolive/internal/config"
	"photolive/internal/media"
	"photolive/internal/server"
	"photolive/internal/storage"
	"photolive/internal/store"
)

//go:embed web
var webFS embed.FS

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	st, err := store.Open(cfg)
	if err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}

	var stg storage.Storage
	switch cfg.StorageDriver {
	case "local":
		stg, err = storage.NewLocal(cfg.StorageDir)
	case "oss":
		stg, err = storage.NewOSS("", "", "", "")
	default:
		log.Fatalf("不支持的存储类型: %s", cfg.StorageDriver)
	}
	if err != nil {
		log.Fatalf("初始化存储失败: %v", err)
	}

	assets, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("加载静态资源失败: %v", err)
	}
	srv, err := server.New(cfg, st, stg, assets)
	if err != nil {
		log.Fatalf("初始化服务失败: %v", err)
	}
	srv.StartBackground()

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	if cfg.BaseURL != "" {
		log.Printf("PhotoLive 已启动: %s（监听 %s，局域网内手机可直接访问）", cfg.BaseURL, cfg.Addr)
	} else {
		log.Printf("PhotoLive 已启动: 监听 %s", cfg.Addr)
	}
	log.Printf("数据库: %s  存储: %s (%s)", cfg.DBDriver, cfg.StorageDriver, cfg.StorageDir)
	if name := media.HEICConverterName(); name != "" {
		log.Printf("HEIC 转换器: %s", name)
	} else {
		log.Printf("HEIC 转换器: 未检测到（HEIC 上传将被拒绝）")
	}
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("服务异常退出: %v", err)
	}
}
