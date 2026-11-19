package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"photolive/internal/store"
)

func (s *Server) guestPage(w http.ResponseWriter, r *http.Request) {
	room, err := s.st.RoomByCode(strings.TrimSpace(r.PathValue("code")))
	if err != nil {
		s.render(w, "guest", pageData{Title: "房间已失效", Missing: true})
		return
	}
	gid := s.guestID(w, r)
	allowed := s.hasAccess(r, room.ID, gid)
	data := pageData{
		Title: room.Name,
		Room:  room,
		Code:  room.AccessCode,
		Gate:  !allowed,
		Share: s.absURL(r, "/r/"+room.AccessCode),
	}
	if allowed {
		marked, err := s.st.MarkedPhotoIDs(room.ID, gid)
		if err == nil {
			data.Selected = len(marked)
		}
	}
	s.render(w, "guest", data)
}

// guestAccess 校验房间与访问凭证，返回访客标识。
func (s *Server) guestAccess(w http.ResponseWriter, r *http.Request) (*store.Room, string, bool) {
	room, err := s.st.RoomByCode(strings.TrimSpace(r.PathValue("code")))
	if err != nil {
		writeErr(w, http.StatusNotFound, "房间已失效，请联系摄影师")
		return nil, "", false
	}
	gid := s.guestID(w, r)
	if !s.hasAccess(r, room.ID, gid) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "需要验证访问码", "need_code": true})
		return nil, "", false
	}
	return room, gid, true
}

func (s *Server) apiGuestVerify(w http.ResponseWriter, r *http.Request) {
	room, err := s.st.RoomByCode(strings.TrimSpace(r.PathValue("code")))
	if err != nil {
		writeErr(w, http.StatusNotFound, "房间已失效，请联系摄影师")
		return
	}
	gid := s.guestID(w, r)
	key := fmt.Sprintf("%d|%s", room.ID, clientIP(r))
	if d, locked := s.locks.locked(key); locked {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error":   fmt.Sprintf("尝试次数过多，请 %d 分钟后再试", int(d.Minutes())+1),
			"seconds": int(d.Seconds()),
		})
		return
	}

	var in struct {
		Code string `json:"code"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	if strings.ToUpper(strings.TrimSpace(in.Code)) != room.AccessCode {
		s.locks.fail(key)
		if _, locked := s.locks.locked(key); locked {
			writeErr(w, http.StatusTooManyRequests, "尝试次数过多，请 10 分钟后再试")
			return
		}
		left := 5 - s.locks.count(key)
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error":         "访问码不正确",
			"attempts_left": left,
		})
		return
	}

	s.locks.reset(key)
	s.grantAccess(w, room.ID, gid)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) apiGuestPhotos(w http.ResponseWriter, r *http.Request) {
	room, gid, ok := s.guestAccess(w, r)
	if !ok {
		return
	}
	photos, err := s.st.PhotosByRoom(room.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "加载照片失败")
		return
	}
	marked, err := s.st.MarkedPhotoIDs(room.ID, gid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "加载选中状态失败")
		return
	}
	out := make([]map[string]any, 0, len(photos))
	for _, p := range photos {
		out = append(out, s.photoJSON(p, marked[p.ID]))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"room": map[string]any{
			"name":           room.Name,
			"event_date":     room.EventDate,
			"status":         room.Status,
			"allow_download": room.AllowDownload,
			"photo_count":    len(photos),
		},
		"photos":         out,
		"selected_count": len(marked),
		"server_time":    time.Now().Unix(),
	})
}

func (s *Server) apiGuestState(w http.ResponseWriter, r *http.Request) {
	room, gid, ok := s.guestAccess(w, r)
	if !ok {
		return
	}
	marked, err := s.st.MarkedPhotoIDs(room.ID, gid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "加载状态失败")
		return
	}
	count, err := s.st.CountPhotos(room.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "加载状态失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"photo_count":    count,
		"selected_count": len(marked),
		"status":         room.Status,
		"allow_download": room.AllowDownload,
	})
}

func (s *Server) apiGuestUpload(w http.ResponseWriter, r *http.Request) {
	room, _, ok := s.guestAccess(w, r)
	if !ok {
		return
	}
	if room.Status != "live" {
		writeErr(w, http.StatusConflict, "活动已结束，无法上传照片")
		return
	}
	if !room.AllowGuestUpload {
		writeErr(w, http.StatusForbidden, "摄影师未开放访客上传")
		return
	}
	s.receivePhoto(w, r, room, "guest")
}

func (s *Server) apiGuestMark(w http.ResponseWriter, r *http.Request) {
	room, gid, ok := s.guestAccess(w, r)
	if !ok {
		return
	}
	if room.Status != "live" {
		writeErr(w, http.StatusConflict, "活动已结束，无法修改选择")
		return
	}
	var in struct {
		PhotoID  int64 `json:"photo_id"`
		Selected bool  `json:"selected"`
	}
	if err := decodeJSON(r, &in); err != nil || in.PhotoID <= 0 {
		writeErr(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	p, err := s.st.PhotoByID(in.PhotoID)
	if err != nil || p.RoomID != room.ID || p.Status != "ready" {
		writeErr(w, http.StatusNotFound, "该照片已被移除")
		return
	}
	if err := s.st.SetMark(room.ID, p.ID, gid, in.Selected); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存失败，请重试")
		return
	}
	marked, err := s.st.MarkedPhotoIDs(room.ID, gid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "保存失败，请重试")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"selected":       in.Selected,
		"selected_count": len(marked),
	})
}

func (s *Server) apiGuestDownload(w http.ResponseWriter, r *http.Request) {
	room, gid, ok := s.guestAccess(w, r)
	if !ok {
		return
	}
	if !room.AllowDownload {
		writeErr(w, http.StatusForbidden, "摄影师未开放下载")
		return
	}
	photos, err := s.st.SelectedPhotos(room.ID, gid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "加载选中照片失败")
		return
	}
	if len(photos) == 0 {
		writeErr(w, http.StatusBadRequest, "还没有选中照片")
		return
	}
	chunk, offset, total := zipChunk(photos, r)
	if len(chunk) == 0 {
		writeErr(w, http.StatusBadRequest, "下载批次超出范围")
		return
	}
	w.Header().Set("X-Total-Selected", strconv.Itoa(total))
	s.writeZip(w, r, zipFilename(room.Name, offset, total), chunk)
}
