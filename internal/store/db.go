package store

import (
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"

	"photolive/internal/config"
)

// Store 包装 database/sql，并通过 Driver 区分少量方言差异（自增主键、忽略重复插入）。
type Store struct {
	DB     *sql.DB
	Driver string
}

func Open(cfg config.Config) (*Store, error) {
	driver := cfg.DBDriver
	dsn := cfg.DSN
	var sqlDriver string
	switch driver {
	case "sqlite":
		sqlDriver = "sqlite"
	case "mysql":
		sqlDriver = "mysql"
		if dsn == "" {
			return nil, fmt.Errorf("使用 MySQL 时必须设置 PHOTOLIVE_DB_DSN")
		}
	default:
		return nil, fmt.Errorf("不支持的数据库驱动: %s", driver)
	}

	db, err := sql.Open(sqlDriver, dsn)
	if err != nil {
		return nil, err
	}
	if driver == "sqlite" {
		db.SetMaxOpenConns(4)
	} else {
		db.SetMaxOpenConns(20)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}

	s := &Store{DB: db, Driver: driver}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	var stmts []string
	if s.Driver == "mysql" {
		stmts = mysqlSchema
	} else {
		stmts = sqliteSchema
	}
	for _, q := range stmts {
		if _, err := s.DB.Exec(q); err != nil {
			return fmt.Errorf("执行迁移失败: %w\nSQL: %s", err, q)
		}
	}
	return s.ensureRoomUploadColumns()
}

// ensureRoomUploadColumns 为已有数据库补上访客上传相关字段，重复执行时忽略“列已存在”错误。
func (s *Store) ensureRoomUploadColumns() error {
	var stmts []string
	if s.Driver == "mysql" {
		stmts = []string{
			`ALTER TABLE rooms ADD COLUMN allow_guest_upload TINYINT NOT NULL DEFAULT 0`,
			`ALTER TABLE rooms ADD COLUMN guest_upload_review TINYINT NOT NULL DEFAULT 0`,
			`ALTER TABLE rooms ADD COLUMN ended_at BIGINT NOT NULL DEFAULT 0`,
		}
	} else {
		stmts = []string{
			`ALTER TABLE rooms ADD COLUMN allow_guest_upload INTEGER NOT NULL DEFAULT 0`,
			`ALTER TABLE rooms ADD COLUMN guest_upload_review INTEGER NOT NULL DEFAULT 0`,
			`ALTER TABLE rooms ADD COLUMN ended_at INTEGER NOT NULL DEFAULT 0`,
		}
	}
	for _, ddl := range stmts {
		if _, err := s.DB.Exec(ddl); err != nil {
			msg := strings.ToLower(err.Error())
			if strings.Contains(msg, "duplicate column") || strings.Contains(msg, "1060") {
				continue
			}
			return fmt.Errorf("补充访客上传字段失败: %w", err)
		}
	}
	return nil
}

// ignoreConflict 返回“重复则忽略”的插入前缀。
func (s *Store) ignoreConflict() string {
	if s.Driver == "mysql" {
		return "INSERT IGNORE"
	}
	return "INSERT OR IGNORE"
}

var sqliteSchema = []string{
	`CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		email TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		failed_attempts INTEGER NOT NULL DEFAULT 0,
		locked_until INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS sessions (
		token TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL,
		expires_at INTEGER NOT NULL,
		created_at INTEGER NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS rooms (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		event_date TEXT NOT NULL,
		access_code TEXT NOT NULL UNIQUE,
		allow_download INTEGER NOT NULL DEFAULT 1,
		status TEXT NOT NULL DEFAULT 'live',
		note TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL,
		ended_at INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE TABLE IF NOT EXISTS room_code_aliases (
		code TEXT PRIMARY KEY,
		room_id INTEGER NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS photos (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		room_id INTEGER NOT NULL,
		orig_name TEXT NOT NULL,
		stored_name TEXT NOT NULL,
		size INTEGER NOT NULL,
		width INTEGER NOT NULL,
		height INTEGER NOT NULL,
		taken_at INTEGER NOT NULL,
		uploaded_at INTEGER NOT NULL,
		status TEXT NOT NULL,
		uploader TEXT NOT NULL,
		mime TEXT NOT NULL,
		orig_key TEXT NOT NULL,
		thumb_key TEXT NOT NULL,
		preview_key TEXT NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_photos_room ON photos(room_id)`,
	`CREATE TABLE IF NOT EXISTS marks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		room_id INTEGER NOT NULL,
		photo_id INTEGER NOT NULL,
		guest_id TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		UNIQUE(room_id, photo_id, guest_id)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_marks_room ON marks(room_id)`,
	`CREATE INDEX IF NOT EXISTS idx_marks_guest ON marks(room_id, guest_id)`,
	`CREATE INDEX IF NOT EXISTS idx_sessions_expiry ON sessions(expires_at)`,
}

var mysqlSchema = []string{
	`CREATE TABLE IF NOT EXISTS users (
		id BIGINT AUTO_INCREMENT PRIMARY KEY,
		email VARCHAR(190) NOT NULL UNIQUE,
		password_hash VARCHAR(255) NOT NULL,
		failed_attempts INT NOT NULL DEFAULT 0,
		locked_until BIGINT NOT NULL DEFAULT 0,
		created_at BIGINT NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS sessions (
		token VARCHAR(64) PRIMARY KEY,
		user_id BIGINT NOT NULL,
		expires_at BIGINT NOT NULL,
		created_at BIGINT NOT NULL,
		INDEX idx_sessions_expiry (expires_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS rooms (
		id BIGINT AUTO_INCREMENT PRIMARY KEY,
		user_id BIGINT NOT NULL,
		name VARCHAR(120) NOT NULL,
		event_date VARCHAR(20) NOT NULL,
		access_code VARCHAR(16) NOT NULL UNIQUE,
		allow_download TINYINT NOT NULL DEFAULT 1,
		status VARCHAR(16) NOT NULL DEFAULT 'live',
		note VARCHAR(500) NOT NULL DEFAULT '',
		created_at BIGINT NOT NULL,
		ended_at BIGINT NOT NULL DEFAULT 0
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS room_code_aliases (
		code VARCHAR(16) PRIMARY KEY,
		room_id BIGINT NOT NULL,
		INDEX idx_alias_room (room_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS photos (
		id BIGINT AUTO_INCREMENT PRIMARY KEY,
		room_id BIGINT NOT NULL,
		orig_name VARCHAR(255) NOT NULL,
		stored_name VARCHAR(255) NOT NULL,
		size BIGINT NOT NULL,
		width INT NOT NULL,
		height INT NOT NULL,
		taken_at BIGINT NOT NULL,
		uploaded_at BIGINT NOT NULL,
		status VARCHAR(16) NOT NULL,
		uploader VARCHAR(16) NOT NULL,
		mime VARCHAR(80) NOT NULL,
		orig_key VARCHAR(255) NOT NULL,
		thumb_key VARCHAR(255) NOT NULL,
		preview_key VARCHAR(255) NOT NULL,
		INDEX idx_photos_room (room_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS marks (
		id BIGINT AUTO_INCREMENT PRIMARY KEY,
		room_id BIGINT NOT NULL,
		photo_id BIGINT NOT NULL,
		guest_id VARCHAR(64) NOT NULL,
		created_at BIGINT NOT NULL,
		UNIQUE KEY uniq_mark (room_id, photo_id, guest_id),
		INDEX idx_marks_room (room_id),
		INDEX idx_marks_guest (room_id, guest_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
}

func isDuplicateErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate")
}
