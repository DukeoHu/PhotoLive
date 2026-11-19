package server

import (
	"archive/zip"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"

	"photolive/internal/store"
)

func (s *Server) apiExport(w http.ResponseWriter, r *http.Request, u *store.User) {
	room, ok := s.ownedRoom(w, r, u)
	if !ok {
		return
	}
	rows, err := s.st.MarkedRows(room.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "导出失败")
		return
	}
	format := strings.ToLower(r.URL.Query().Get("format"))
	stamp := time.Now().Format("20060102-150405")
	if format == "txt" {
		setAttachment(w, room.Name+"_选中清单_"+stamp+".txt")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		var b strings.Builder
		for _, row := range rows {
			b.WriteString(row.OrigName)
			b.WriteString("\r\n")
		}
		_, _ = io.WriteString(w, b.String())
		return
	}

	setAttachment(w, room.Name+"_选中清单_"+stamp+".csv")
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"文件名", "拍摄时间", "标记时间"})
	for _, row := range rows {
		_ = cw.Write([]string{row.OrigName, formatTime(row.TakenAt), formatTime(row.MarkedAt)})
	}
	cw.Flush()
}

func (s *Server) apiDownload(w http.ResponseWriter, r *http.Request, u *store.User) {
	room, ok := s.ownedRoom(w, r, u)
	if !ok {
		return
	}
	photos, err := s.st.SelectedPhotos(room.ID, "")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "加载选中照片失败")
		return
	}
	if len(photos) == 0 {
		writeErr(w, http.StatusBadRequest, "还没有客户选中照片")
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

const maxZipPhotos = 500

// zipChunk 按 offset/limit 截取本次下载批次，避免单个 ZIP 过大导致超时。
func zipChunk(photos []*store.Photo, r *http.Request) ([]*store.Photo, int, int) {
	total := len(photos)
	offset := 0
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && v > 0 {
		offset = v
	}
	limit := maxZipPhotos
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v < maxZipPhotos {
		limit = v
	}
	if offset >= total {
		return nil, offset, total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return photos[offset:end], offset, total
}

func zipFilename(roomName string, offset, total int) string {
	if total <= maxZipPhotos {
		return roomName + "_已选.zip"
	}
	part := offset/maxZipPhotos + 1
	parts := (total + maxZipPhotos - 1) / maxZipPhotos
	return fmt.Sprintf("%s_已选_第%d批_共%d批.zip", roomName, part, parts)
}

func (s *Server) apiQR(w http.ResponseWriter, r *http.Request, u *store.User) {
	room, ok := s.ownedRoom(w, r, u)
	if !ok {
		return
	}
	link := s.absURL(r, "/r/"+room.AccessCode)
	png, err := qrcode.Encode(link, qrcode.Medium, 640)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "生成二维码失败")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Query().Get("download") == "1" {
		setAttachment(w, "photolive-"+room.AccessCode+"-qr.png")
	}
	_, _ = w.Write(png)
}

func (s *Server) writeZip(w http.ResponseWriter, r *http.Request, filename string, photos []*store.Photo) {
	setAttachment(w, filename)
	w.Header().Set("Content-Type", "application/zip")

	zw := zip.NewWriter(w)
	defer zw.Close()

	used := map[string]int{}
	for _, p := range photos {
		entry := p.OrigName
		if n := used[entry]; n > 0 {
			ext := filepath.Ext(entry)
			entry = fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(p.OrigName, ext), n, ext)
		}
		used[p.OrigName]++

		rc, err := s.storage.Open(r.Context(), p.OrigKey)
		if err != nil {
			log.Printf("打包下载时打开文件失败 %s: %v", p.OrigKey, err)
			continue
		}
		f, err := zw.Create(entry)
		if err != nil {
			rc.Close()
			log.Printf("创建压缩条目失败 %s: %v", entry, err)
			continue
		}
		if _, err := io.Copy(f, rc); err != nil {
			log.Printf("写入压缩条目失败 %s: %v", entry, err)
		}
		rc.Close()
	}
}

func formatTime(ts int64) string {
	if ts <= 0 {
		return ""
	}
	return time.Unix(ts, 0).Format("2006-01-02 15:04")
}

func setAttachment(w http.ResponseWriter, name string) {
	safe := strings.NewReplacer("/", "_", "\\", "_", "\"", "_", "\r", "", "\n", "").Replace(name)
	fallback := "photolive" + filepath.Ext(safe)
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=\"%s\"; filename*=UTF-8''%s", fallback, url.PathEscape(safe)))
}
