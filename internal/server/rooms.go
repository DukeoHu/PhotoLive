package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"photolive/internal/store"
)

func (s *Server) roomJSON(r *http.Request, room *store.Room) map[string]any {
	return map[string]any{
		"id":                  room.ID,
		"name":                room.Name,
		"event_date":          room.EventDate,
		"access_code":         room.AccessCode,
		"allow_download":      room.AllowDownload,
		"allow_guest_upload":  room.AllowGuestUpload,
		"guest_upload_review": room.GuestUploadReview,
		"pending_count":       room.PendingCount,
		"status":              room.Status,
		"note":                room.Note,
		"created_at":          room.CreatedAt,
		"photo_count":         room.PhotoCount,
		"selected_count":      room.SelectedCount,
		"share_url":           s.absURL(r, "/r/"+room.AccessCode),
	}
}

func (s *Server) apiCreateRoom(w http.ResponseWriter, r *http.Request, u *store.User) {
	var in struct {
		Name          string `json:"name"`
		EventDate     string `json:"event_date"`
		Note          string `json:"note"`
		AllowDownload *bool  `json:"allow_download"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	name := strings.TrimSpace(in.Name)
	if n := utf8.RuneCountInString(name); n < 2 || n > 40 {
		writeErr(w, http.StatusBadRequest, "房间名称需为 2–40 个字")
		return
	}
	date := strings.TrimSpace(in.EventDate)
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		writeErr(w, http.StatusBadRequest, "活动日期格式不正确")
		return
	}
	note := strings.TrimSpace(in.Note)
	if utf8.RuneCountInString(note) > 200 {
		writeErr(w, http.StatusBadRequest, "备注不能超过 200 字")
		return
	}
	allow := true
	if in.AllowDownload != nil {
		allow = *in.AllowDownload
	}
	room, err := s.st.CreateRoom(u.ID, name, date, allow, note)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "创建房间失败，请重试")
		return
	}
	writeJSON(w, http.StatusCreated, s.roomJSON(r, room))
}

func (s *Server) apiUpdateRoom(w http.ResponseWriter, r *http.Request, u *store.User) {
	room, ok := s.ownedRoom(w, r, u)
	if !ok {
		return
	}
	var in struct {
		Status            *string `json:"status"`
		AllowDownload     *bool   `json:"allow_download"`
		AllowGuestUpload  *bool   `json:"allow_guest_upload"`
		GuestUploadReview *bool   `json:"guest_upload_review"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	if in.Status != nil {
		st := *in.Status
		if st != "live" && st != "ended" {
			writeErr(w, http.StatusBadRequest, "房间状态不正确")
			return
		}
		if err := s.st.UpdateRoomStatus(room.ID, st); err != nil {
			writeErr(w, http.StatusInternalServerError, "更新房间失败")
			return
		}
	}
	if in.AllowDownload != nil {
		if err := s.st.UpdateRoomSettings(room.ID, *in.AllowDownload); err != nil {
			writeErr(w, http.StatusInternalServerError, "更新房间失败")
			return
		}
	}
	if in.AllowGuestUpload != nil {
		if err := s.st.UpdateRoomGuestUpload(room.ID, *in.AllowGuestUpload); err != nil {
			writeErr(w, http.StatusInternalServerError, "更新房间失败")
			return
		}
	}
	if in.GuestUploadReview != nil {
		if err := s.st.UpdateRoomGuestReview(room.ID, *in.GuestUploadReview); err != nil {
			writeErr(w, http.StatusInternalServerError, "更新房间失败")
			return
		}
	}
	updated, err := s.st.RoomByID(room.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "更新房间失败")
		return
	}
	writeJSON(w, http.StatusOK, s.roomJSON(r, updated))
}

func (s *Server) apiDeleteRoom(w http.ResponseWriter, r *http.Request, u *store.User) {
	room, ok := s.ownedRoom(w, r, u)
	if !ok {
		return
	}
	keys, err := s.st.DeleteRoom(room.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "删除房间失败")
		return
	}
	for _, k := range keys {
		_ = s.storage.Delete(r.Context(), k)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) apiResetAccessCode(w http.ResponseWriter, r *http.Request, u *store.User) {
	room, ok := s.ownedRoom(w, r, u)
	if !ok {
		return
	}
	code, err := s.st.ResetAccessCode(room.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "重置访问码失败")
		return
	}
	updated, err := s.st.RoomByID(room.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "重置访问码失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_code": code,
		"share_url":   s.absURL(r, "/r/"+code),
		"room":        s.roomJSON(r, updated),
	})
}

func (s *Server) apiUpdateAccessCode(w http.ResponseWriter, r *http.Request, u *store.User) {
	room, ok := s.ownedRoom(w, r, u)
	if !ok {
		return
	}
	var in struct {
		Code string `json:"code"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	code := strings.ToUpper(strings.TrimSpace(in.Code))
	if !validAccessCode(code) {
		writeErr(w, http.StatusBadRequest, "访问码需为 4–8 位字母或数字")
		return
	}
	if existing, err := s.st.RoomByCode(code); err == nil && existing.ID != room.ID {
		writeErr(w, http.StatusConflict, "该访问码已被占用，请更换")
		return
	}
	if err := s.st.SetAccessCode(room.ID, code); err != nil {
		writeErr(w, http.StatusInternalServerError, "更新访问码失败")
		return
	}
	updated, err := s.st.RoomByID(room.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "更新访问码失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_code": code,
		"share_url":   s.absURL(r, "/r/"+code),
		"room":        s.roomJSON(r, updated),
	})
}

func validAccessCode(code string) bool {
	if len(code) < 4 || len(code) > 8 {
		return false
	}
	for _, c := range code {
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func (s *Server) apiRoomStats(w http.ResponseWriter, r *http.Request, u *store.User) {
	room, ok := s.ownedRoom(w, r, u)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"photo_count":    room.PhotoCount,
		"selected_count": room.SelectedCount,
		"pending_count":  room.PendingCount,
		"status":         room.Status,
		"allow_download": room.AllowDownload,
	})
}

// ownedRoom 解析路径中的房间 ID 并校验归属。
func (s *Server) ownedRoom(w http.ResponseWriter, r *http.Request, u *store.User) (*store.Room, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "房间不存在")
		return nil, false
	}
	room, err := s.st.RoomByID(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "房间不存在")
		return nil, false
	}
	if room.UserID != u.ID {
		writeErr(w, http.StatusForbidden, "无权操作该房间")
		return nil, false
	}
	return room, true
}
