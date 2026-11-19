package store

// User 是摄影师账号。
type User struct {
	ID             int64
	Email          string
	PasswordHash   string
	FailedAttempts int
	LockedUntil    int64
	CreatedAt      int64
}

// Room 是一次活动的照片集合，也是分享与权限的最小单位。
type Room struct {
	ID                int64  `json:"id"`
	UserID            int64  `json:"-"`
	Name              string `json:"name"`
	EventDate         string `json:"event_date"`
	AccessCode        string `json:"access_code"`
	AllowDownload     bool   `json:"allow_download"`
	AllowGuestUpload  bool   `json:"allow_guest_upload"`
	GuestUploadReview bool   `json:"guest_upload_review"`
	Status            string `json:"status"`
	Note              string `json:"note"`
	CreatedAt         int64  `json:"created_at"`
	EndedAt           int64  `json:"ended_at"`

	PhotoCount      int `json:"photo_count"`
	SelectedCount   int `json:"selected_count"`
	ProcessingCount int `json:"processing_count"`
	PendingCount    int `json:"pending_count"`
}

// Photo 是一张原图及其派生缩略图。
type Photo struct {
	ID         int64  `json:"id"`
	RoomID     int64  `json:"room_id"`
	OrigName   string `json:"name"`
	StoredName string `json:"stored_name"`
	Size       int64  `json:"size"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	TakenAt    int64  `json:"taken_at"`
	UploadedAt int64  `json:"uploaded_at"`
	Status     string `json:"status"`
	Uploader   string `json:"uploader"`
	MarkedAt   int64  `json:"marked_at,omitempty"`
	Mime       string `json:"mime"`
	OrigKey    string `json:"-"`
	ThumbKey   string `json:"thumb_key"`
	PreviewKey string `json:"preview_key"`
}

// MarkRow 是导出选中清单时使用的一行。
type MarkRow struct {
	OrigName string
	TakenAt  int64
	MarkedAt int64
}
