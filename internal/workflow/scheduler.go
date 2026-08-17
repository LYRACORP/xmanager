package workflow

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

// Scheduler runs cron-triggered workflows.
type Scheduler struct {
	eng  *Engine
	stop chan struct{}
}

func NewScheduler(eng *Engine) *Scheduler {
	return &Scheduler{eng: eng, stop: make(chan struct{})}
}

func (s *Scheduler) Start() {
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-t.C:
				s.tick()
			}
		}
	}()
}

func (s *Scheduler) Stop() {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
}

func (s *Scheduler) tick() {
	if s.eng == nil || s.eng.DB == nil {
		return
	}
	var list []storage.Workflow
	if err := s.eng.DB.Where("enabled = ? AND trigger = ?", true, "cron").Find(&list).Error; err != nil {
		return
	}
	now := time.Now()
	for _, wf := range list {
		if cronMatches(wf.CronExpr, now) {
			_, _ = s.eng.Run(context.Background(), wf.ID, "cron")
		}
	}
}

func cronMatches(expr string, now time.Time) bool {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return false
	}
	return matchField(fields[0], now.Minute(), 0, 59) &&
		matchField(fields[1], now.Hour(), 0, 23) &&
		matchField(fields[2], now.Day(), 1, 31) &&
		matchField(fields[3], int(now.Month()), 1, 12) &&
		matchField(fields[4], int(now.Weekday()), 0, 6)
}

func matchField(f string, v, min, max int) bool {
	if f == "*" {
		return true
	}
	if strings.HasPrefix(f, "*/") {
		n, err := strconv.Atoi(strings.TrimPrefix(f, "*/"))
		if err != nil || n <= 0 {
			return false
		}
		return v%n == 0
	}
	n, err := strconv.Atoi(f)
	if err != nil || n < min || n > max {
		return false
	}
	return n == v
}

func ChatMatches(db *gorm.DB, text string) []storage.Workflow {
	if db == nil {
		return nil
	}
	var list []storage.Workflow
	_ = db.Where("enabled = ? AND trigger = ? AND chat_phrase <> ''", true, "chat").Find(&list).Error
	var out []storage.Workflow
	low := strings.ToLower(text)
	for _, wf := range list {
		if strings.Contains(low, strings.ToLower(wf.ChatPhrase)) {
			out = append(out, wf)
		}
	}
	return out
}
