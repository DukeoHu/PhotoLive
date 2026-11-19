package store

// MarkedPhotoSet 返回房间内所有被任意客户标记过的照片 ID 集合。
func (s *Store) MarkedPhotoSet(roomID int64) (map[int64]bool, error) {
	rows, err := s.DB.Query(`SELECT DISTINCT photo_id FROM marks WHERE room_id = ?`, roomID)
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

// MarkedAtByRoom 返回房间内每张照片最早被标记的时间。
func (s *Store) MarkedAtByRoom(roomID int64) (map[int64]int64, error) {
	rows, err := s.DB.Query(`SELECT photo_id, MIN(created_at) FROM marks WHERE room_id = ? GROUP BY photo_id`, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var id, at int64
		if err := rows.Scan(&id, &at); err != nil {
			return nil, err
		}
		out[id] = at
	}
	return out, rows.Err()
}
