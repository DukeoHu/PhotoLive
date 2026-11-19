package server

import (
	"fmt"
	"net/http"
	"os"

	"photolive/internal/store"
)

func ownerZipKey(u *store.User, roomID int64) string {
	return fmt.Sprintf("u%d-r%d", u.ID, roomID)
}

func guestZipKey(roomID int64, gid string) string {
	return fmt.Sprintf("g%d-%s", roomID, gid)
}

func jobJSON(j *zipJob) map[string]any {
	return map[string]any{
		"status":   j.Status,
		"total":    j.Total,
		"filename": j.Filename,
		"error":    j.Err,
	}
}

func (s *Server) apiOwnerZipStart(w http.ResponseWriter, r *http.Request, u *store.User) {
	room, ok := s.ownedRoom(w, r, u)
	if !ok {
		return
	}
	s.startZipJob(w, room, "", ownerZipKey(u, room.ID))
}

func (s *Server) apiGuestZipStart(w http.ResponseWriter, r *http.Request) {
	room, gid, ok := s.guestAccess(w, r)
	if !ok {
		return
	}
	if !room.AllowDownload {
		writeErr(w, http.StatusForbidden, "摄影师未开放下载")
		return
	}
	s.startZipJob(w, room, gid, guestZipKey(room.ID, gid))
}

func (s *Server) startZipJob(w http.ResponseWriter, room *store.Room, gid, key string) {
	photos, err := s.st.SelectedPhotos(room.ID, gid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "加载选中照片失败")
		return
	}
	if len(photos) == 0 {
		writeErr(w, http.StatusBadRequest, "还没有客户选中照片")
		return
	}
	job := s.zips.start(key, room.Name+"_已选.zip", photos, s.storage)
	writeJSON(w, http.StatusOK, jobJSON(job))
}

func (s *Server) apiOwnerZipStatus(w http.ResponseWriter, r *http.Request, u *store.User) {
	room, ok := s.ownedRoom(w, r, u)
	if !ok {
		return
	}
	s.writeZipStatus(w, ownerZipKey(u, room.ID))
}

func (s *Server) apiGuestZipStatus(w http.ResponseWriter, r *http.Request) {
	room, gid, ok := s.guestAccess(w, r)
	if !ok {
		return
	}
	s.writeZipStatus(w, guestZipKey(room.ID, gid))
}

func (s *Server) writeZipStatus(w http.ResponseWriter, key string) {
	job, ok := s.zips.status(key)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"status": "none"})
		return
	}
	writeJSON(w, http.StatusOK, jobJSON(job))
}

func (s *Server) apiOwnerZipFile(w http.ResponseWriter, r *http.Request, u *store.User) {
	room, ok := s.ownedRoom(w, r, u)
	if !ok {
		return
	}
	s.serveZipJob(w, r, ownerZipKey(u, room.ID))
}

func (s *Server) apiGuestZipFile(w http.ResponseWriter, r *http.Request) {
	room, gid, ok := s.guestAccess(w, r)
	if !ok {
		return
	}
	if !room.AllowDownload {
		writeErr(w, http.StatusForbidden, "摄影师未开放下载")
		return
	}
	s.serveZipJob(w, r, guestZipKey(room.ID, gid))
}

func (s *Server) serveZipJob(w http.ResponseWriter, r *http.Request, key string) {
	job, ok := s.zips.take(key)
	if !ok {
		writeErr(w, http.StatusNotFound, "打包文件不存在，请重新打包")
		return
	}
	defer os.Remove(job.Path)
	w.Header().Set("Content-Type", "application/zip")
	setAttachment(w, job.Filename)
	http.ServeFile(w, r, job.Path)
}
