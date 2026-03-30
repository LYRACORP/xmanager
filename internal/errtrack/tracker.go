package errtrack

import (
	"time"

	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

type Tracker struct {
	db *gorm.DB
}

func NewTracker(db *gorm.DB) *Tracker {
	return &Tracker{db: db}
}

func (t *Tracker) Record(serverID uint, service, message, stackTrace, severity string) error {
	fp := Fingerprint(service, message)

	var existing storage.ErrorEvent
	err := t.db.Where("server_id = ? AND fingerprint = ? AND resolved = ?", serverID, fp, false).
		First(&existing).Error

	if err == nil {
		return t.db.Model(&existing).Updates(map[string]interface{}{
			"count":     existing.Count + 1,
			"last_seen": time.Now(),
			"message":   message,
		}).Error
	}

	event := storage.ErrorEvent{
		ServerID:    serverID,
		Service:     service,
		Fingerprint: fp,
		Message:     message,
		StackTrace:  stackTrace,
		Severity:    severity,
		Count:       1,
		FirstSeen:   time.Now(),
		LastSeen:    time.Now(),
	}
	return t.db.Create(&event).Error
}

func (t *Tracker) GetUnresolved(serverID uint) ([]storage.ErrorEvent, error) {
	var events []storage.ErrorEvent
	err := t.db.Where("server_id = ? AND resolved = ?", serverID, false).
		Order("last_seen DESC").Find(&events).Error
	return events, err
}

func (t *Tracker) GetAll(serverID uint) ([]storage.ErrorEvent, error) {
	var events []storage.ErrorEvent
	err := t.db.Where("server_id = ?", serverID).
		Order("last_seen DESC").Find(&events).Error
	return events, err
}

func (t *Tracker) Resolve(id uint) error {
	return t.db.Model(&storage.ErrorEvent{}).Where("id = ?", id).
		Update("resolved", true).Error
}

func (t *Tracker) Mute(id uint) error {
	return t.db.Model(&storage.ErrorEvent{}).Where("id = ?", id).
		Update("muted", true).Error
}

func (t *Tracker) Unmute(id uint) error {
	return t.db.Model(&storage.ErrorEvent{}).Where("id = ?", id).
		Update("muted", false).Error
}

func (t *Tracker) Delete(id uint) error {
	return t.db.Delete(&storage.ErrorEvent{}, id).Error
}
