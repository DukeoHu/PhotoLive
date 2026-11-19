package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"photolive/internal/config"
	"photolive/internal/storage"
	"photolive/internal/store"
)

const sessionCookie = "pl_session"
const guestCookie = "pl_guest"

// Server 负责路由、模板渲染与所有 HTTP 处理。
type Server struct {
	cfg     config.Config
	st      *store.Store
	storage storage.Storage
	assets  fs.FS
	tmpl    map[string]*template.Template
	locks   *lockTable
	zips    *zipJobManager
}

func New(cfg config.Config, st *store.Store, stg storage.Storage, assets fs.FS) (*Server, error) {
	s := &Server{
		cfg:     cfg,
		st:      st,
		storage: stg,
		assets:  assets,
		tmpl:    map[string]*template.Template{},
		locks:   newLockTable(),
		zips:    newZipJobManager(""),
	}
	funcs := template.FuncMap{
		"date": func(ts int64) string {
			if ts <= 0 {
				return "—"
			}
			return time.Unix(ts, 0).Format("2006-01-02 15:04")
		},
		"day": func(ts int64) string {
			if ts <= 0 {
				return "—"
			}
			return time.Unix(ts, 0).Format("2006.01.02")
		},
		"add": func(a, b int) int { return a + b },
	}
	pages := []string{"landing", "login", "admin", "room", "guest"}
	for _, p := range pages {
		t, err := template.New("base").Funcs(funcs).ParseFS(s.assets, "templates/base.html", "templates/"+p+".html")
		if err != nil {
			return nil, fmt.Errorf("解析模板 %s 失败: %w", p, err)
		}
		s.tmpl[p] = t
	}
	return s, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// 页面
	mux.HandleFunc("GET /{$}", s.landingPage)
	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("GET /admin", s.requirePage(s.adminPage))
	mux.HandleFunc("GET /admin/room/{id}", s.requirePage(s.roomPage))
	mux.HandleFunc("GET /r/{code}", s.guestPage)

	// 认证
	mux.HandleFunc("POST /api/auth/register", s.apiRegister)
	mux.HandleFunc("POST /api/auth/login", s.apiLogin)
	mux.HandleFunc("POST /api/auth/logout", s.apiLogout)

	// 房间
	mux.HandleFunc("POST /api/rooms", s.apiRequireUser(s.apiCreateRoom))
	mux.HandleFunc("POST /api/rooms/{id}/access-code", s.apiRequireUser(s.apiResetAccessCode))
	mux.HandleFunc("PUT /api/rooms/{id}/access-code", s.apiRequireUser(s.apiUpdateAccessCode))
	mux.HandleFunc("PATCH /api/rooms/{id}", s.apiRequireUser(s.apiUpdateRoom))
	mux.HandleFunc("DELETE /api/rooms/{id}", s.apiRequireUser(s.apiDeleteRoom))
	mux.HandleFunc("GET /api/rooms/{id}/stats", s.apiRequireUser(s.apiRoomStats))
	mux.HandleFunc("GET /api/rooms/{id}/photos", s.apiRequireUser(s.apiListPhotos))
	mux.HandleFunc("POST /api/rooms/{id}/photos", s.apiRequireUser(s.apiUploadPhoto))
	mux.HandleFunc("GET /api/rooms/{id}/selected", s.apiRequireUser(s.apiSelectedPhotos))
	mux.HandleFunc("GET /api/rooms/{id}/pending", s.apiRequireUser(s.apiPendingPhotos))
	mux.HandleFunc("POST /api/photos/{id}/approve", s.apiRequireUser(s.apiApprovePhoto))
	mux.HandleFunc("GET /api/rooms/{id}/export", s.apiRequireUser(s.apiExport))
	mux.HandleFunc("GET /api/rooms/{id}/download", s.apiRequireUser(s.apiDownload))
	mux.HandleFunc("POST /api/rooms/{id}/download-job", s.apiRequireUser(s.apiOwnerZipStart))
	mux.HandleFunc("GET /api/rooms/{id}/download-job", s.apiRequireUser(s.apiOwnerZipStatus))
	mux.HandleFunc("GET /api/rooms/{id}/download-job/file", s.apiRequireUser(s.apiOwnerZipFile))
	mux.HandleFunc("GET /api/rooms/{id}/qr", s.apiRequireUser(s.apiQR))
	mux.HandleFunc("DELETE /api/photos/{id}", s.apiRequireUser(s.apiDeletePhoto))

	// 客户端
	mux.HandleFunc("POST /api/guest/{code}/verify", s.apiGuestVerify)
	mux.HandleFunc("GET /api/guest/{code}/photos", s.apiGuestPhotos)
	mux.HandleFunc("GET /api/guest/{code}/state", s.apiGuestState)
	mux.HandleFunc("POST /api/guest/{code}/marks", s.apiGuestMark)
	mux.HandleFunc("POST /api/guest/{code}/photos", s.apiGuestUpload)
	mux.HandleFunc("GET /api/guest/{code}/download", s.apiGuestDownload)
	mux.HandleFunc("POST /api/guest/{code}/download-job", s.apiGuestZipStart)
	mux.HandleFunc("GET /api/guest/{code}/download-job", s.apiGuestZipStatus)
	mux.HandleFunc("GET /api/guest/{code}/download-job/file", s.apiGuestZipFile)

	// 静态资源
	staticFS, err := fs.Sub(s.assets, "static")
	if err == nil {
		fileServer := http.StripPrefix("/static/", http.FileServer(http.FS(staticFS)))
		mux.Handle("GET /static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 开发阶段禁用强缓存，避免浏览器继续使用旧版 JS/CSS
			w.Header().Set("Cache-Control", "no-cache, must-revalidate")
			fileServer.ServeHTTP(w, r)
		}))
	}
	// 公开缩略图目录（原图不在此目录）
	if root := s.storage.PublicRoot(); root != "" {
		media := http.StripPrefix("/media/", http.FileServer(http.Dir(root)))
		mux.Handle("GET /media/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			media.ServeHTTP(w, r)
		}))
	}

	return s.common(mux)
}

func (s *Server) common(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				log.Printf("panic: %v %s %s", v, r.Method, r.URL.Path)
				http.Error(w, "服务器内部错误", http.StatusInternalServerError)
			}
		}()
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// ---------- 页面渲染 ----------

type pageData struct {
	Title    string
	User     *store.User
	Room     *store.Room
	Rooms    []*store.Room
	Gate     bool
	Missing  bool
	Code     string
	Share    string
	Selected int
	MaxFiles int
}

func (s *Server) render(w http.ResponseWriter, page string, data pageData) {
	t, ok := s.tmpl[page]
	if !ok {
		http.Error(w, "模板不存在", http.StatusInternalServerError)
		return
	}
	if data.Title == "" {
		data.Title = "PhotoLive"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "base", data); err != nil {
		log.Printf("渲染模板失败 %s: %v", page, err)
	}
}

func (s *Server) currentUser(r *http.Request) *store.User {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	uid, err := s.st.UserIDBySession(c.Value)
	if err != nil {
		return nil
	}
	u, err := s.st.UserByID(uid)
	if err != nil {
		return nil
	}
	return u
}

func (s *Server) requirePage(next func(http.ResponseWriter, *http.Request, *store.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := s.currentUser(r)
		if u == nil {
			http.Redirect(w, r, "/login?next="+r.URL.Path, http.StatusFound)
			return
		}
		next(w, r, u)
	}
}

func (s *Server) apiRequireUser(next func(http.ResponseWriter, *http.Request, *store.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := s.currentUser(r)
		if u == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "请先登录"})
			return
		}
		next(w, r, u)
	}
}

// ---------- 通用工具 ----------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: maxAge,
	})
}

func (s *Server) newSession(w http.ResponseWriter, userID int64) error {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return err
	}
	token := hex.EncodeToString(buf)
	expires := time.Now().Add(7 * 24 * time.Hour).Unix()
	if err := s.st.CreateSession(token, userID, expires); err != nil {
		return err
	}
	s.setSessionCookie(w, token, 7*24*3600)
	return nil
}

func (s *Server) clearSession(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		_ = s.st.DeleteSession(c.Value)
	}
	s.setSessionCookie(w, "", -1)
}

// guestID 读取或生成访客标识，保证同一浏览器能恢复自己的选中结果。
func (s *Server) guestID(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(guestCookie); err == nil && len(c.Value) >= 16 {
		return c.Value
	}
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	id := hex.EncodeToString(buf)
	http.SetCookie(w, &http.Cookie{
		Name: guestCookie, Value: id, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 30 * 24 * 3600,
	})
	return id
}

func (s *Server) accessCookie(roomID int64) string {
	return fmt.Sprintf("pl_access_%d", roomID)
}

func (s *Server) signAccess(roomID int64, guestID string, exp int64) string {
	mac := hmac.New(sha256.New, []byte(s.cfg.Secret))
	fmt.Fprintf(mac, "%d|%s|%d", roomID, guestID, exp)
	return fmt.Sprintf("%d.%s", exp, hex.EncodeToString(mac.Sum(nil)))
}

func (s *Server) grantAccess(w http.ResponseWriter, roomID int64, guestID string) {
	exp := time.Now().Add(30 * 24 * time.Hour).Unix()
	http.SetCookie(w, &http.Cookie{
		Name: s.accessCookie(roomID), Value: s.signAccess(roomID, guestID, exp),
		Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 30 * 24 * 3600,
	})
}

func (s *Server) hasAccess(r *http.Request, roomID int64, guestID string) bool {
	c, err := r.Cookie(s.accessCookie(roomID))
	if err != nil || c.Value == "" {
		return false
	}
	parts := strings.SplitN(c.Value, ".", 2)
	if len(parts) != 2 {
		return false
	}
	exp, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || exp < time.Now().Unix() {
		return false
	}
	want := s.signAccess(roomID, guestID, exp)
	return hmac.Equal([]byte(want), []byte(c.Value))
}

func (s *Server) absURL(r *http.Request, path string) string {
	if s.cfg.BaseURL != "" {
		return s.cfg.BaseURL + path
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p == "https" || p == "http" {
		scheme = p
	}
	return scheme + "://" + r.Host + path
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ---------- 访问码尝试次数（单实例内存即可） ----------

type lockEntry struct {
	fails int
	until time.Time
}

type lockTable struct {
	mu sync.Mutex
	m  map[string]*lockEntry
}

func newLockTable() *lockTable { return &lockTable{m: map[string]*lockEntry{}} }

func (l *lockTable) locked(key string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.m[key]
	if e == nil {
		return 0, false
	}
	if time.Now().Before(e.until) {
		return time.Until(e.until), true
	}
	if !e.until.IsZero() || e.fails == 0 {
		delete(l.m, key)
	}
	return 0, false
}

// fail 记录一次失败，达到 5 次后锁定 10 分钟。
func (l *lockTable) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.m[key]
	if e == nil {
		e = &lockEntry{}
		l.m[key] = e
	}
	e.fails++
	if e.fails >= 5 {
		e.until = time.Now().Add(10 * time.Minute)
		e.fails = 0
	}
}

func (l *lockTable) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, key)
}

func (l *lockTable) count(key string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e := l.m[key]; e != nil {
		return e.fails
	}
	return 0
}
