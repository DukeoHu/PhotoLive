package server

import (
	"net/http"
	"strconv"

	"photolive/internal/store"
)

func (s *Server) landingPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, "landing", pageData{Title: "拍完即看，挑完即走", User: s.currentUser(r)})
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	if u := s.currentUser(r); u != nil {
		http.Redirect(w, r, "/admin", http.StatusFound)
		return
	}
	s.render(w, "login", pageData{Title: "登录"})
}

func (s *Server) adminPage(w http.ResponseWriter, r *http.Request, u *store.User) {
	rooms, err := s.st.RoomsByUser(u.ID)
	if err != nil {
		http.Error(w, "加载房间失败", http.StatusInternalServerError)
		return
	}
	s.render(w, "admin", pageData{Title: "我的房间", User: u, Rooms: rooms})
}

func (s *Server) roomPage(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	room, err := s.st.RoomByID(id)
	if err != nil || room.UserID != u.ID {
		http.NotFound(w, r)
		return
	}
	share := s.absURL(r, "/r/"+room.AccessCode)
	s.render(w, "room", pageData{Title: room.Name, User: u, Room: room, Share: share, MaxFiles: s.cfg.MaxFilesPerBatch})
}
