package server

import (
	"context"
	"log"
	"time"
)

// StartBackground 启动保留期清理任务：每天检查一次，删除过期房间的照片文件。
func (s *Server) StartBackground() {
	go func() {
		s.runRetention()
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			s.runRetention()
		}
	}()
}

func (s *Server) runRetention() {
	if s.cfg.RetentionDays <= 0 {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -s.cfg.RetentionDays).Unix()
	photos, err := s.st.PhotosToExpire(cutoff)
	if err != nil {
		log.Printf("保留期清理失败: %v", err)
		return
	}
	if len(photos) == 0 {
		return
	}
	ctx := context.Background()
	removed := 0
	for _, p := range photos {
		for _, key := range []string{p.OrigKey, p.ThumbKey, p.PreviewKey} {
			if err := s.storage.Delete(ctx, key); err != nil {
				log.Printf("删除过期文件失败 %s: %v", key, err)
			}
		}
		if err := s.st.MarkPhotoExpired(p.ID); err != nil {
			log.Printf("标记过期照片失败 %d: %v", p.ID, err)
			continue
		}
		removed++
	}
	log.Printf("保留期清理: %d 张照片已过期，文件已删除", removed)
}
