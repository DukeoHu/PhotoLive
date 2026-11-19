package store

import "time"

// SetMark 设置某位客户对某张照片的选中状态。
func (s *Store) SetMark(roomID, photoID int64, guestID string, selected bool) error {
	if selected {
		q := s.ignoreConflict() + ` INTO marks (room_id, photo_id, guest_id, created_at) VALUES (?, ?, ?, ?)`
		_, err := s.DB.Exec(q, roomID, photoID, guestID, time.Now().Unix())
		return err
	}
	_, err := s.DB.Exec(
		`DELETE FROM marks WHERE room_id = ? AND photo_id = ? AND guest_id = ?`,
		roomID, photoID, guestID,
	)
	return err
}

// MarkedPhotoIDs 返回该客户在本房间内已标记的照片 ID。
func (s *Store) MarkedPhotoIDs(roomID int64, guestID string) (map[int64]bool, error) {
	rows, err := s.DB.Query(`SELECT photo_id FROM marks WHERE room_id = ? AND guest_id = ?`, roomID, guestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// SelectedCount 统计房间内被选中过的不同照片数。
func (s *Store) SelectedCount(roomID int64) (int, error) {
	var n int
	err := s.DB.QueryRow(
		`SELECT COUNT(DISTINCT m.photo_id) FROM marks m JOIN photos p ON p.id = m.photo_id
		 WHERE m.room_id = ? AND p.status = 'ready'`, roomID,
	).Scan(&n)
	return n, err
}
