package store

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

const roomColumns = `r.id, r.user_id, r.name, r.event_date, r.access_code, r.allow_download, r.allow_guest_upload, r.guest_upload_review, r.status, r.note, r.created_at, r.ended_at,
	(SELECT COUNT(*) FROM photos p WHERE p.room_id = r.id AND p.status = 'ready'),
	(SELECT COUNT(DISTINCT m.photo_id) FROM marks m JOIN photos p2 ON p2.id = m.photo_id WHERE m.room_id = r.id AND p2.status = 'ready'),
	(SELECT COUNT(*) FROM photos p3 WHERE p3.room_id = r.id AND p3.status = 'processing'),
	(SELECT COUNT(*) FROM photos p4 WHERE p4.room_id = r.id AND p4.status = 'pending')`

func scanRoom(row interface{ Scan(...any) error }) (*Room, error) {
	r := &Room{}
	var allowDownload, allowGuestUpload, guestUploadReview int
	err := row.Scan(&r.ID, &r.UserID, &r.Name, &r.EventDate, &r.AccessCode, &allowDownload, &allowGuestUpload, &guestUploadReview, &r.Status, &r.Note, &r.CreatedAt, &r.EndedAt,
		&r.PhotoCount, &r.SelectedCount, &r.ProcessingCount, &r.PendingCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	r.AllowDownload = allowDownload == 1
	r.AllowGuestUpload = allowGuestUpload == 1
	r.GuestUploadReview = guestUploadReview == 1
	return r, nil
}

func (s *Store) CreateRoom(userID int64, name, eventDate string, allowDownload bool, note string) (*Room, error) {
	code, err := s.uniqueAccessCode()
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	allow := 0
	if allowDownload {
		allow = 1
	}
	res, err := s.DB.Exec(
		`INSERT INTO rooms (user_id, name, event_date, access_code, allow_download, status, note, created_at)
		 VALUES (?, ?, ?, ?, ?, 'live', ?, ?)`,
		userID, name, eventDate, code, allow, note, now,
	)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.RoomByID(id)
}

func (s *Store) uniqueAccessCode() (string, error) {
	for i := 0; i < 40; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(900000))
		if err != nil {
			return "", err
		}
		code := fmt.Sprintf("%d", n.Int64()+100000)
		var exists int
		err = s.DB.QueryRow(`SELECT COUNT(*) FROM rooms WHERE access_code = ?`, code).Scan(&exists)
		if err != nil {
			return "", err
		}
		if exists == 0 {
			return code, nil
		}
	}
	return "", errors.New("生成访问码失败，请重试")
}

func (s *Store) RoomByID(id int64) (*Room, error) {
	return scanRoom(s.DB.QueryRow(`SELECT `+roomColumns+` FROM rooms r WHERE r.id = ?`, id))
}

func (s *Store) RoomByCode(code string) (*Room, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	return scanRoom(s.DB.QueryRow(`SELECT `+roomColumns+` FROM rooms r
		WHERE r.access_code = ? OR r.id = (SELECT room_id FROM room_code_aliases WHERE code = ?)`, code, code))
}

// rememberCode 记录旧访问码，保证已进入的客户继续通过旧链接访问。
func (s *Store) rememberCode(id int64, code string) {
	if code == "" {
		return
	}
	q := `INSERT OR IGNORE INTO room_code_aliases (code, room_id) VALUES (?, ?)`
	if s.Driver == "mysql" {
		q = `INSERT IGNORE INTO room_code_aliases (code, room_id) VALUES (?, ?)`
	}
	_, _ = s.DB.Exec(q, code, id)
}

func (s *Store) currentCode(id int64) string {
	var code string
	_ = s.DB.QueryRow(`SELECT access_code FROM rooms WHERE id = ?`, id).Scan(&code)
	return code
}

func (s *Store) RoomsByUser(userID int64) ([]*Room, error) {
	rows, err := s.DB.Query(`SELECT `+roomColumns+` FROM rooms r WHERE r.user_id = ? ORDER BY r.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Room{}
	for rows.Next() {
		r, err := scanRoom(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) UpdateRoomStatus(id int64, status string) error {
	if status == "ended" {
		_, err := s.DB.Exec(`UPDATE rooms SET status = ?, ended_at = ? WHERE id = ?`, status, time.Now().Unix(), id)
		return err
	}
	_, err := s.DB.Exec(`UPDATE rooms SET status = ?, ended_at = 0 WHERE id = ?`, status, id)
	return err
}

func (s *Store) UpdateRoomSettings(id int64, allowDownload bool) error {
	allow := 0
	if allowDownload {
		allow = 1
	}
	_, err := s.DB.Exec(`UPDATE rooms SET allow_download = ? WHERE id = ?`, allow, id)
	return err
}

func (s *Store) UpdateRoomGuestUpload(id int64, allow bool) error {
	v := 0
	if allow {
		v = 1
	}
	_, err := s.DB.Exec(`UPDATE rooms SET allow_guest_upload = ? WHERE id = ?`, v, id)
	return err
}

func (s *Store) UpdateRoomGuestReview(id int64, review bool) error {
	v := 0
	if review {
		v = 1
	}
	_, err := s.DB.Exec(`UPDATE rooms SET guest_upload_review = ? WHERE id = ?`, v, id)
	return err
}

// ResetAccessCode 生成并保存新的访问码，旧访问码立即失效。
func (s *Store) ResetAccessCode(id int64) (string, error) {
	code, err := s.uniqueAccessCode()
	if err != nil {
		return "", err
	}
	s.rememberCode(id, s.currentCode(id))
	if _, err := s.DB.Exec(`UPDATE rooms SET access_code = ? WHERE id = ?`, code, id); err != nil {
		return "", err
	}
	return code, nil
}

// SetAccessCode 保存摄影师自定义的访问码。
func (s *Store) SetAccessCode(id int64, code string) error {
	s.rememberCode(id, s.currentCode(id))
	_, err := s.DB.Exec(`UPDATE rooms SET access_code = ? WHERE id = ?`, code, id)
	return err
}

// DeleteRoom 删除房间及其照片、标记，并返回需要清理的文件键。
func (s *Store) DeleteRoom(id int64) ([]string, error) {
	keys, err := s.PhotoKeysByRoom(id)
	if err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM marks WHERE room_id = ?`, id); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`DELETE FROM photos WHERE room_id = ?`, id); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`DELETE FROM rooms WHERE id = ?`, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return keys, nil
}
