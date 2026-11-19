package store

import (
	"database/sql"
	"errors"
)

const photoColumns = `id, room_id, orig_name, stored_name, size, width, height, taken_at, uploaded_at,
	status, uploader, mime, orig_key, thumb_key, preview_key`

func scanPhoto(row interface{ Scan(...any) error }) (*Photo, error) {
	p := &Photo{}
	err := row.Scan(&p.ID, &p.RoomID, &p.OrigName, &p.StoredName, &p.Size, &p.Width, &p.Height,
		&p.TakenAt, &p.UploadedAt, &p.Status, &p.Uploader, &p.Mime, &p.OrigKey, &p.ThumbKey, &p.PreviewKey)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Store) InsertPhoto(p *Photo) error {
	res, err := s.DB.Exec(
		`INSERT INTO photos (room_id, orig_name, stored_name, size, width, height, taken_at, uploaded_at,
			status, uploader, mime, orig_key, thumb_key, preview_key)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.RoomID, p.OrigName, p.StoredName, p.Size, p.Width, p.Height, p.TakenAt, p.UploadedAt,
		p.Status, p.Uploader, p.Mime, p.OrigKey, p.ThumbKey, p.PreviewKey,
	)
	if err != nil {
		return err
	}
	p.ID, err = res.LastInsertId()
	return err
}

func (s *Store) PhotoByID(id int64) (*Photo, error) {
	return scanPhoto(s.DB.QueryRow(`SELECT `+photoColumns+` FROM photos WHERE id = ?`, id))
}

func (s *Store) PhotosByRoom(roomID int64) ([]*Photo, error) {
	rows, err := s.DB.Query(
		`SELECT `+photoColumns+` FROM photos WHERE room_id = ? AND status = 'ready' ORDER BY taken_at ASC, id ASC`,
		roomID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Photo{}
	for rows.Next() {
		p, err := scanPhoto(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) CountPhotos(roomID int64) (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM photos WHERE room_id = ? AND status = 'ready'`, roomID).Scan(&n)
	return n, err
}

// CountAllPhotos 统计房间内全部照片（含待审核），用于容量上限判断。
func (s *Store) CountAllPhotos(roomID int64) (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM photos WHERE room_id = ?`, roomID).Scan(&n)
	return n, err
}

// PendingPhotos 返回等待摄影师审核的访客照片。
func (s *Store) PendingPhotos(roomID int64) ([]*Photo, error) {
	rows, err := s.DB.Query(
		`SELECT `+photoColumns+` FROM photos WHERE room_id = ? AND status = 'pending' ORDER BY uploaded_at ASC, id ASC`,
		roomID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Photo{}
	for rows.Next() {
		p, err := scanPhoto(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) SetPhotoStatus(photoID int64, status string) error {
	_, err := s.DB.Exec(`UPDATE photos SET status = ? WHERE id = ?`, status, photoID)
	return err
}

// PhotosToExpire 返回已结束且超过保留期的房间中的照片。
func (s *Store) PhotosToExpire(cutoff int64) ([]*Photo, error) {
	rows, err := s.DB.Query(
		`SELECT `+photoColumns+` FROM photos
		 WHERE status IN ('ready', 'pending')
		   AND room_id IN (SELECT id FROM rooms WHERE status = 'ended' AND ended_at > 0 AND ended_at < ?)`,
		cutoff,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Photo{}
	for rows.Next() {
		p, err := scanPhoto(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) MarkPhotoExpired(photoID int64) error {
	_, err := s.DB.Exec(`UPDATE photos SET status = 'expired' WHERE id = ?`, photoID)
	return err
}

func (s *Store) PhotoNameExists(roomID int64, name string) (bool, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM photos WHERE room_id = ? AND orig_name = ?`, roomID, name).Scan(&n)
	return n > 0, err
}

func (s *Store) FindDuplicate(roomID int64, name string, size int64) (bool, error) {
	var n int
	err := s.DB.QueryRow(
		`SELECT COUNT(*) FROM photos WHERE room_id = ? AND orig_name = ? AND size = ?`,
		roomID, name, size,
	).Scan(&n)
	return n > 0, err
}

// DeletePhoto 删除单张照片与其标记，返回需要清理的文件键。
func (s *Store) DeletePhoto(photoID int64) ([]string, error) {
	p, err := s.PhotoByID(photoID)
	if err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM marks WHERE photo_id = ?`, photoID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`DELETE FROM photos WHERE id = ?`, photoID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return []string{p.OrigKey, p.ThumbKey, p.PreviewKey}, nil
}

func (s *Store) PhotoKeysByRoom(roomID int64) ([]string, error) {
	rows, err := s.DB.Query(`SELECT orig_key, thumb_key, preview_key FROM photos WHERE room_id = ?`, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var a, b, c string
		if err := rows.Scan(&a, &b, &c); err != nil {
			return nil, err
		}
		keys = append(keys, a, b, c)
	}
	return keys, rows.Err()
}

// SelectedPhotos 返回房间内被标记过的照片；guestID 为空时返回任意客户标记过的照片。
func (s *Store) SelectedPhotos(roomID int64, guestID string) ([]*Photo, error) {
	var rows *sql.Rows
	var err error
	if guestID == "" {
		rows, err = s.DB.Query(
			`SELECT `+photoColumns+` FROM photos p
			 WHERE p.room_id = ? AND p.status = 'ready'
			   AND EXISTS (SELECT 1 FROM marks m WHERE m.photo_id = p.id)
			 ORDER BY p.taken_at ASC, p.id ASC`, roomID)
	} else {
		rows, err = s.DB.Query(
			`SELECT `+photoColumns+` FROM photos p
			 WHERE p.room_id = ? AND p.status = 'ready'
			   AND EXISTS (SELECT 1 FROM marks m WHERE m.photo_id = p.id AND m.guest_id = ?)
			 ORDER BY p.taken_at ASC, p.id ASC`, roomID, guestID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Photo{}
	for rows.Next() {
		p, err := scanPhoto(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// MarkedRows 返回去重后的选中清单，含最早标记时间，用于 CSV / TXT 导出。
func (s *Store) MarkedRows(roomID int64) ([]MarkRow, error) {
	rows, err := s.DB.Query(
		`SELECT p.orig_name, p.taken_at, MIN(m.created_at) AS marked_at
		 FROM photos p JOIN marks m ON m.photo_id = p.id
		 WHERE p.room_id = ? AND p.status = 'ready'
		 GROUP BY p.id, p.orig_name, p.taken_at
		 ORDER BY marked_at ASC, p.id ASC`, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MarkRow{}
	for rows.Next() {
		var r MarkRow
		if err := rows.Scan(&r.OrigName, &r.TakenAt, &r.MarkedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
