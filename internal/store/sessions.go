package store

import (
	"database/sql"
	"errors"
	"time"
)

func (s *Store) CreateSession(token string, userID int64, expiresAt int64) error {
	_, err := s.DB.Exec(
		`INSERT INTO sessions (token, user_id, expires_at, created_at) VALUES (?, ?, ?, ?)`,
		token, userID, expiresAt, time.Now().Unix(),
	)
	return err
}

func (s *Store) UserIDBySession(token string) (int64, error) {
	var userID, expiresAt int64
	err := s.DB.QueryRow(`SELECT user_id, expires_at FROM sessions WHERE token = ?`, token).Scan(&userID, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if expiresAt < time.Now().Unix() {
		_ = s.DeleteSession(token)
		return 0, ErrNotFound
	}
	return userID, nil
}

func (s *Store) DeleteSession(token string) error {
	_, err := s.DB.Exec(`DELETE FROM sessions WHERE token = ?`, token)
	return err
}

func (s *Store) CleanupSessions() {
	_, _ = s.DB.Exec(`DELETE FROM sessions WHERE expires_at < ?`, time.Now().Unix())
}
