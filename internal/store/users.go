package store

import (
	"database/sql"
	"errors"
	"time"
)

var ErrNotFound = errors.New("记录不存在")

// CreateUser 创建摄影师账号，邮箱冲突时返回 ErrDuplicate。
func (s *Store) CreateUser(email, hash string) (*User, error) {
	now := time.Now().Unix()
	res, err := s.DB.Exec(
		`INSERT INTO users (email, password_hash, failed_attempts, locked_until, created_at) VALUES (?, ?, 0, 0, ?)`,
		email, hash, now,
	)
	if err != nil {
		if isDuplicateErr(err) {
			return nil, ErrDuplicate
		}
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &User{ID: id, Email: email, PasswordHash: hash, CreatedAt: now}, nil
}

var ErrDuplicate = errors.New("已存在")

func (s *Store) UserByEmail(email string) (*User, error) {
	u := &User{}
	err := s.DB.QueryRow(
		`SELECT id, email, password_hash, failed_attempts, locked_until, created_at FROM users WHERE email = ?`,
		email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.FailedAttempts, &u.LockedUntil, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Store) UserByID(id int64) (*User, error) {
	u := &User{}
	err := s.DB.QueryRow(
		`SELECT id, email, password_hash, failed_attempts, locked_until, created_at FROM users WHERE id = ?`,
		id,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.FailedAttempts, &u.LockedUntil, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Store) SetLoginFailure(id int64, attempts, lockedUntil int64) error {
	_, err := s.DB.Exec(`UPDATE users SET failed_attempts = ?, locked_until = ? WHERE id = ?`, attempts, lockedUntil, id)
	return err
}

func (s *Store) ResetLoginFailure(id int64) error {
	_, err := s.DB.Exec(`UPDATE users SET failed_attempts = 0, locked_until = 0 WHERE id = ?`, id)
	return err
}
