package model

import "time"

type TemporaryMedia struct {
	ID            int64  `json:"id" gorm:"primaryKey"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
	MediaID       string `json:"media_id" gorm:"type:varchar(64);uniqueIndex"`
	TaskID        string `json:"task_id" gorm:"type:varchar(64);uniqueIndex:idx_temporary_media_task_position;index"`
	Position      int    `json:"position" gorm:"uniqueIndex:idx_temporary_media_task_position"`
	UserID        int    `json:"user_id" gorm:"index"`
	FileName      string `json:"-" gorm:"type:varchar(255)"`
	ContentType   string `json:"content_type" gorm:"type:varchar(100)"`
	Size          int64  `json:"size"`
	SHA256        string `json:"sha256" gorm:"type:varchar(64)"`
	RevisedPrompt string `json:"revised_prompt" gorm:"type:text"`
	ExpiresAt     int64  `json:"expires_at" gorm:"index"`
	DownloadedAt  int64  `json:"downloaded_at"`
	DeletedAt     int64  `json:"deleted_at" gorm:"index"`
}

func (m *TemporaryMedia) Insert() error {
	return DB.Create(m).Error
}

func (m *TemporaryMedia) Update() error {
	return DB.Save(m).Error
}

func GetTemporaryMediaByTaskPosition(taskID string, position int) (*TemporaryMedia, bool, error) {
	var media TemporaryMedia
	err := DB.Where("task_id = ? AND position = ?", taskID, position).First(&media).Error
	exists, err := RecordExist(err)
	return &media, exists, err
}

func GetTemporaryMediaForUser(userID int, taskID, mediaID string) (*TemporaryMedia, bool, error) {
	var media TemporaryMedia
	err := DB.Where("user_id = ? AND task_id = ? AND media_id = ?", userID, taskID, mediaID).
		First(&media).Error
	exists, err := RecordExist(err)
	return &media, exists, err
}

func GetTemporaryMediaByTask(taskID string) ([]*TemporaryMedia, error) {
	var media []*TemporaryMedia
	err := DB.Where("task_id = ?", taskID).Order("position").Find(&media).Error
	return media, err
}

func RefreshTemporaryMediaExpiry(taskID string, expiresAt int64) error {
	return DB.Model(&TemporaryMedia{}).
		Where("task_id = ? AND deleted_at = ?", taskID, 0).
		Updates(map[string]any{"expires_at": expiresAt, "updated_at": time.Now().Unix()}).Error
}

func GetExpiredTemporaryMedia(now int64, limit int) ([]*TemporaryMedia, error) {
	var media []*TemporaryMedia
	err := DB.Where("deleted_at = ? AND expires_at > ? AND expires_at <= ?", 0, 0, now).
		Order("expires_at").Limit(limit).Find(&media).Error
	return media, err
}

func MarkTemporaryMediaDownloaded(id int64, downloadedAt int64) error {
	return DB.Model(&TemporaryMedia{}).
		Where("id = ? AND downloaded_at = ?", id, 0).
		Updates(map[string]any{"downloaded_at": downloadedAt, "updated_at": downloadedAt}).Error
}

func MarkTemporaryMediaDeleted(id int64, deletedAt int64) error {
	return DB.Model(&TemporaryMedia{}).
		Where("id = ?", id).
		Updates(map[string]any{"deleted_at": deletedAt, "updated_at": deletedAt}).Error
}

func DeleteTemporaryMediaMetadataBefore(cutoff int64, limit int) (int64, error) {
	var ids []int64
	if err := DB.Model(&TemporaryMedia{}).
		Where("deleted_at > ? AND deleted_at < ?", 0, cutoff).
		Order("deleted_at").Limit(limit).Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	result := DB.Where("id IN ?", ids).Delete(&TemporaryMedia{})
	return result.RowsAffected, result.Error
}
