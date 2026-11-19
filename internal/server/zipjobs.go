package server

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"photolive/internal/storage"
	"photolive/internal/store"
)

// zipJob 是一次后台打包任务。文件生成在临时目录，下载一次后删除。
type zipJob struct {
	Key      string
	Status   string // running / ready / failed
	Path     string
	Filename string
	Total    int
	Created  time.Time
	Err      string
}

type zipJobManager struct {
	mu   sync.Mutex
	jobs map[string]*zipJob
	dir  string
}

func newZipJobManager(dir string) *zipJobManager {
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "photolive-zips")
	}
	_ = os.MkdirAll(dir, 0o755)
	return &zipJobManager{jobs: map[string]*zipJob{}, dir: dir}
}

// start 返回已有任务或启动新的打包任务。
func (m *zipJobManager) start(key, filename string, photos []*store.Photo, stg storage.Storage) *zipJob {
	m.mu.Lock()
	m.cleanupLocked()
	if j, ok := m.jobs[key]; ok {
		if j.Status == "running" || (j.Status == "ready" && time.Since(j.Created) < time.Hour) {
			cp := *j
			m.mu.Unlock()
			return &cp
		}
	}
	j := &zipJob{
		Key:      key,
		Status:   "running",
		Filename: filename,
		Total:    len(photos),
		Created:  time.Now(),
	}
	m.jobs[key] = j
	cp := *j
	m.mu.Unlock()

	go m.build(j, photos, stg)
	return &cp
}

func (m *zipJobManager) status(key string) (*zipJob, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[key]
	if !ok {
		return nil, false
	}
	cp := *j
	return &cp, true
}

// take 取出可下载任务并从队列移除，文件由调用方负责删除。
func (m *zipJobManager) take(key string) (*zipJob, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[key]
	if !ok || j.Status != "ready" {
		return nil, false
	}
	delete(m.jobs, key)
	cp := *j
	return &cp, true
}

func (m *zipJobManager) build(j *zipJob, photos []*store.Photo, stg storage.Storage) {
	tmp, err := os.CreateTemp(m.dir, "build-*.part")
	if err != nil {
		m.fail(j, "创建临时文件失败")
		return
	}
	tmpPath := tmp.Name()
	defer func() {
		if tmp != nil {
			_ = tmp.Close()
		}
	}()

	zw := zip.NewWriter(tmp)
	used := map[string]int{}
	ctx := context.Background()
	for _, p := range photos {
		name := p.OrigName
		if n := used[p.OrigName]; n > 0 {
			ext := filepath.Ext(name)
			name = strings.TrimSuffix(name, ext) + fmt.Sprintf("_%d", n+1) + ext
		}
		used[p.OrigName]++

		rc, err := stg.Open(ctx, p.OrigKey)
		if err != nil {
			zw.Close()
			_ = os.Remove(tmpPath)
			m.fail(j, "读取照片失败："+p.OrigName)
			return
		}
		w, err := zw.Create(name)
		if err == nil {
			_, err = io.Copy(w, rc)
		}
		rc.Close()
		if err != nil {
			zw.Close()
			_ = os.Remove(tmpPath)
			m.fail(j, "写入压缩包失败："+p.OrigName)
			return
		}
	}
	if err := zw.Close(); err != nil {
		_ = os.Remove(tmpPath)
		m.fail(j, "写入压缩包失败")
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		m.fail(j, "写入压缩包失败")
		return
	}
	tmp = nil

	final := filepath.Join(m.dir, fmt.Sprintf("%s-%d.zip", sanitizeKey(j.Key), time.Now().Unix()))
	if err := os.Rename(tmpPath, final); err != nil {
		_ = os.Remove(tmpPath)
		m.fail(j, "保存压缩包失败")
		return
	}

	m.mu.Lock()
	j.Status = "ready"
	j.Path = final
	m.mu.Unlock()
}

func (m *zipJobManager) fail(j *zipJob, msg string) {
	m.mu.Lock()
	j.Status = "failed"
	j.Err = msg
	m.mu.Unlock()
}

// cleanupLocked 清理超过一小时的任务与文件，调用方需持有锁。
func (m *zipJobManager) cleanupLocked() {
	for key, j := range m.jobs {
		if time.Since(j.Created) < time.Hour {
			continue
		}
		if j.Path != "" {
			_ = os.Remove(j.Path)
		}
		delete(m.jobs, key)
	}
}

func sanitizeKey(key string) string {
	var b strings.Builder
	for _, c := range key {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			b.WriteRune(c)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}
