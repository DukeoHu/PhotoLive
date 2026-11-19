package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"photolive/internal/diskfree"
	"photolive/internal/media"
	"photolive/internal/storage"
	"photolive/internal/store"
)

func (s *Server) photoJSON(p *store.Photo, selected bool) map[string]any {
	m := map[string]any{
		"id":          p.ID,
		"name":        p.OrigName,
		"width":       p.Width,
		"height":      p.Height,
		"size":        p.Size,
		"taken_at":    p.TakenAt,
		"uploaded_at": p.UploadedAt,
		"selected":    selected,
		"status":      p.Status,
		"uploader":    p.Uploader,
		"thumb_url":   s.storage.PublicURL(p.ThumbKey),
		"preview_url": s.storage.PublicURL(p.PreviewKey),
	}
	if p.MarkedAt > 0 {
		m["marked_at"] = p.MarkedAt
	}
	return m
}

func (s *Server) apiListPhotos(w http.ResponseWriter, r *http.Request, u *store.User) {
	room, ok := s.ownedRoom(w, r, u)
	if !ok {
		return
	}
	photos, err := s.st.PhotosByRoom(room.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "加载照片失败")
		return
	}
	marked, err := s.st.MarkedPhotoSet(room.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "加载选中状态失败")
		return
	}
	markedAt, _ := s.st.MarkedAtByRoom(room.ID)
	for _, p := range photos {
		p.MarkedAt = markedAt[p.ID]
	}
	out := make([]map[string]any, 0, len(photos))
	for _, p := range photos {
		out = append(out, s.photoJSON(p, marked[p.ID]))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"photos": out,
		"room":   s.roomJSON(r, room),
	})
}

func (s *Server) apiSelectedPhotos(w http.ResponseWriter, r *http.Request, u *store.User) {
	room, ok := s.ownedRoom(w, r, u)
	if !ok {
		return
	}
	photos, err := s.st.SelectedPhotos(room.ID, "")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "加载选中照片失败")
		return
	}
	out := make([]map[string]any, 0, len(photos))
	for _, p := range photos {
		out = append(out, s.photoJSON(p, true))
	}
	writeJSON(w, http.StatusOK, map[string]any{"photos": out})
}

func (s *Server) apiUploadPhoto(w http.ResponseWriter, r *http.Request, u *store.User) {
	room, ok := s.ownedRoom(w, r, u)
	if !ok {
		return
	}
	if room.Status != "live" {
		writeErr(w, http.StatusConflict, "活动已结束，无法继续上传")
		return
	}
	s.receivePhoto(w, r, room, "photographer")
}

// receivePhoto 处理单张上传并直接写回响应；调用方负责校验房间归属与上传权限。
func (s *Server) receivePhoto(w http.ResponseWriter, r *http.Request, room *store.Room, uploader string) {
	total, err := s.st.CountAllPhotos(room.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "检查房间容量失败")
		return
	}
	if total >= s.cfg.MaxRoomPhotos {
		writeErr(w, http.StatusConflict, fmt.Sprintf("单个房间最多 %d 张照片，请新建房间", s.cfg.MaxRoomPhotos))
		return
	}

	limit := int64(s.cfg.MaxUploadMB) << 20
	r.Body = http.MaxBytesReader(w, r.Body, limit+2<<20)
	if err := r.ParseMultipartForm(limit); err != nil {
		writeErr(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("单张照片不能超过 %dMB", s.cfg.MaxUploadMB))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "没有收到文件")
		return
	}
	defer file.Close()

	// 本地存储时先确认磁盘空间，避免整批上传到一半才失败
	if local, ok := s.storage.(*storage.Local); ok {
		if free, err := diskfree.Free(local.Root()); err == nil {
			need := uint64(header.Size) + 256<<20
			if free < need {
				writeErr(w, http.StatusInsufficientStorage, fmt.Sprintf("磁盘空间不足，剩余 %d MB，请清理后重试", free>>20))
				return
			}
		}
	}

	name := cleanFileName(header.Filename)
	if name == "" || name == "." {
		writeErr(w, http.StatusBadRequest, "文件名不正确")
		return
	}

	dup, err := s.st.FindDuplicate(room.ID, name, header.Size)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "检查重复文件失败")
		return
	}
	if dup {
		writeJSON(w, http.StatusOK, map[string]any{"skipped": true, "name": name, "reason": "重复文件，已跳过"})
		return
	}

	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "读取文件失败")
		return
	}
	if int64(len(data)) > limit {
		writeErr(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("单张照片不能超过 %dMB", s.cfg.MaxUploadMB))
		return
	}

	res, err := media.Process(data, name)
	if err != nil {
		switch {
		case errors.Is(err, media.ErrHEICUnavailable):
			writeErr(w, http.StatusUnsupportedMediaType, "该服务器暂不支持 HEIC，请将照片转为 JPG 后上传")
		case errors.Is(err, media.ErrUnsupported):
			writeErr(w, http.StatusUnsupportedMediaType, "仅支持 JPG / PNG / WebP / HEIC")
		case errors.Is(err, media.ErrTooLarge):
			writeErr(w, http.StatusRequestEntityTooLarge, "图片尺寸过大")
		default:
			writeErr(w, http.StatusBadRequest, "图片处理失败："+err.Error())
		}
		return
	}

	displayName, err := s.uniquePhotoName(room.ID, displayFileName(name, res.Ext))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "生成文件名失败")
		return
	}

	stored := randomHex(12) + res.Ext
	origKey := fmt.Sprintf("orig/%d/%s", room.ID, stored)
	thumbKey := fmt.Sprintf("pub/%d/%s_480.jpg", room.ID, stored)
	previewKey := fmt.Sprintf("pub/%d/%s_1600.jpg", room.ID, stored)

	ctx := r.Context()
	if err := s.storage.Put(ctx, origKey, res.File); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存原图失败")
		return
	}
	if err := s.storage.Put(ctx, thumbKey, res.Thumb); err != nil {
		s.cleanupKeys(ctx, origKey)
		writeErr(w, http.StatusInternalServerError, "保存缩略图失败")
		return
	}
	if err := s.storage.Put(ctx, previewKey, res.Preview); err != nil {
		s.cleanupKeys(ctx, origKey, thumbKey)
		writeErr(w, http.StatusInternalServerError, "保存预览图失败")
		return
	}

	takenAt := res.TakenAt
	if !res.HasTakenAt || takenAt <= 0 {
		takenAt = time.Now().Unix()
	}
	photoStatus := "ready"
	if uploader == "guest" && room.GuestUploadReview {
		photoStatus = "pending"
	}
	p := &store.Photo{
		RoomID:     room.ID,
		OrigName:   displayName,
		StoredName: stored,
		Size:       int64(len(res.File)),
		Width:      res.Width,
		Height:     res.Height,
		TakenAt:    takenAt,
		UploadedAt: time.Now().Unix(),
		Status:     photoStatus,
		Uploader:   uploader,
		Mime:       res.Mime,
		OrigKey:    origKey,
		ThumbKey:   thumbKey,
		PreviewKey: previewKey,
	}
	if err := s.st.InsertPhoto(p); err != nil {
		s.cleanupKeys(ctx, origKey, thumbKey, previewKey)
		writeErr(w, http.StatusInternalServerError, "保存照片记录失败")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"photo": s.photoJSON(p, false),
	})
}

func (s *Server) apiPendingPhotos(w http.ResponseWriter, r *http.Request, u *store.User) {
	room, ok := s.ownedRoom(w, r, u)
	if !ok {
		return
	}
	photos, err := s.st.PendingPhotos(room.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "加载待审核照片失败")
		return
	}
	out := make([]map[string]any, 0, len(photos))
	for _, p := range photos {
		out = append(out, s.photoJSON(p, false))
	}
	writeJSON(w, http.StatusOK, map[string]any{"photos": out, "pending_count": len(out)})
}

func (s *Server) apiApprovePhoto(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "照片不存在")
		return
	}
	p, err := s.st.PhotoByID(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "照片不存在")
		return
	}
	room, err := s.st.RoomByID(p.RoomID)
	if err != nil || room.UserID != u.ID {
		writeErr(w, http.StatusForbidden, "无权操作该照片")
		return
	}
	if p.Status != "pending" {
		writeErr(w, http.StatusConflict, "该照片不在待审核状态")
		return
	}
	if err := s.st.SetPhotoStatus(p.ID, "ready"); err != nil {
		writeErr(w, http.StatusInternalServerError, "审核失败，请重试")
		return
	}
	p.Status = "ready"
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "photo": s.photoJSON(p, false)})
}

func (s *Server) apiDeletePhoto(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "照片不存在")
		return
	}
	p, err := s.st.PhotoByID(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "照片不存在")
		return
	}
	room, err := s.st.RoomByID(p.RoomID)
	if err != nil || room.UserID != u.ID {
		writeErr(w, http.StatusForbidden, "无权操作该照片")
		return
	}
	keys, err := s.st.DeletePhoto(p.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	s.cleanupKeys(r.Context(), keys...)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) uniquePhotoName(roomID int64, name string) (string, error) {
	exists, err := s.st.PhotoNameExists(roomID, name)
	if err != nil {
		return "", err
	}
	if !exists {
		return name, nil
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 2; i < 1000; i++ {
		candidate := fmt.Sprintf("%s_%d%s", stem, i, ext)
		exists, err := s.st.PhotoNameExists(roomID, candidate)
		if err != nil {
			return "", err
		}
		if !exists {
			return candidate, nil
		}
	}
	return fmt.Sprintf("%s_%d%s", stem, time.Now().UnixNano(), ext), nil
}

func (s *Server) cleanupKeys(ctx context.Context, keys ...string) {
	for _, k := range keys {
		_ = s.storage.Delete(ctx, k)
	}
}

func displayFileName(name, newExt string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == ".heic" || ext == ".heif" {
		return strings.TrimSuffix(name, filepath.Ext(name)) + newExt
	}
	return name
}

func cleanFileName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, "/", "_")
	r := []rune(name)
	out := make([]rune, 0, len(r))
	for _, c := range r {
		if c < 32 {
			continue
		}
		out = append(out, c)
	}
	res := strings.TrimSpace(string(out))
	if len([]rune(res)) > 120 {
		ext := filepath.Ext(res)
		stem := []rune(strings.TrimSuffix(res, ext))
		res = string(stem[:100]) + ext
	}
	return res
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
