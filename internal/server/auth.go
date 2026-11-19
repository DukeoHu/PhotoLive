package server

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"photolive/internal/store"
)

var emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

func validPassword(p string) bool {
	if len(p) < 8 {
		return false
	}
	var letter, digit bool
	for _, r := range p {
		switch {
		case r >= '0' && r <= '9':
			digit = true
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			letter = true
		}
	}
	return letter && digit
}

func (s *Server) apiRegister(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if !emailPattern.MatchString(email) {
		writeErr(w, http.StatusBadRequest, "请输入正确的邮箱地址")
		return
	}
	if !validPassword(in.Password) {
		writeErr(w, http.StatusBadRequest, "密码至少 8 位，且需同时包含字母和数字")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "注册失败，请重试")
		return
	}
	u, err := s.st.CreateUser(email, string(hash))
	if err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			writeErr(w, http.StatusConflict, "该邮箱已注册，请直接登录")
			return
		}
		writeErr(w, http.StatusInternalServerError, "注册失败，请重试")
		return
	}
	if err := s.newSession(w, u.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, "创建登录态失败")
		return
	}
	s.st.CleanupSessions()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "email": u.Email})
}

func (s *Server) apiLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	u, err := s.st.UserByEmail(email)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "邮箱或密码不正确")
		return
	}
	now := time.Now().Unix()
	if u.LockedUntil > now {
		mins := (u.LockedUntil-now)/60 + 1
		writeJSON(w, http.StatusLocked, map[string]any{
			"error":   "尝试次数过多，请稍后再试",
			"minutes": mins,
			"seconds": u.LockedUntil - now,
		})
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.Password)) != nil {
		attempts := u.FailedAttempts + 1
		lockedUntil := int64(0)
		if attempts >= 5 {
			attempts = 0
			lockedUntil = time.Now().Add(15 * time.Minute).Unix()
		}
		_ = s.st.SetLoginFailure(u.ID, int64(attempts), lockedUntil)
		writeErr(w, http.StatusUnauthorized, "邮箱或密码不正确")
		return
	}
	_ = s.st.ResetLoginFailure(u.ID)
	if err := s.newSession(w, u.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, "登录失败，请重试")
		return
	}
	s.st.CleanupSessions()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "email": u.Email})
}

func (s *Server) apiLogout(w http.ResponseWriter, r *http.Request) {
	s.clearSession(w, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
