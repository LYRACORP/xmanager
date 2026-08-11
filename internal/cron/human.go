package cron

import (
	"fmt"
	"strconv"
	"strings"
)

// HumanSchedule is a plain-language schedule that converts to a 5-field cron expression.
type HumanSchedule struct {
	// Freq is one of: minutes, hours, daily, weekly, monthly, weekdays
	Freq     string
	Interval int    // every N minutes/hours (default 1)
	Hour     int    // 0-23 for daily/weekly/monthly/weekdays
	Minute   int    // 0-59
	Weekday  int    // 0=Sun … 6=Sat (weekly)
	MonthDay int    // 1-28 for monthly
}

// ExpressionFromHuman builds a 5-field cron expression from human-friendly fields.
func ExpressionFromHuman(h HumanSchedule) (string, error) {
	freq := strings.ToLower(strings.TrimSpace(h.Freq))
	if h.Interval <= 0 {
		h.Interval = 1
	}
	if h.Minute < 0 || h.Minute > 59 {
		return "", fmt.Errorf("minute must be 0–59")
	}
	if h.Hour < 0 || h.Hour > 23 {
		return "", fmt.Errorf("hour must be 0–23")
	}

	switch freq {
	case "minutes":
		if h.Interval > 59 {
			return "", fmt.Errorf("minute interval must be 1–59")
		}
		if h.Interval == 1 {
			return "* * * * *", nil
		}
		return fmt.Sprintf("*/%d * * * *", h.Interval), nil
	case "hours":
		if h.Interval > 23 {
			return "", fmt.Errorf("hour interval must be 1–23")
		}
		if h.Interval == 1 {
			return fmt.Sprintf("%d * * * *", h.Minute), nil
		}
		return fmt.Sprintf("%d */%d * * *", h.Minute, h.Interval), nil
	case "daily":
		return fmt.Sprintf("%d %d * * *", h.Minute, h.Hour), nil
	case "weekdays":
		return fmt.Sprintf("%d %d * * 1-5", h.Minute, h.Hour), nil
	case "weekly":
		if h.Weekday < 0 || h.Weekday > 6 {
			return "", fmt.Errorf("weekday must be 0 (Sun) through 6 (Sat)")
		}
		return fmt.Sprintf("%d %d * * %d", h.Minute, h.Hour, h.Weekday), nil
	case "monthly":
		if h.MonthDay < 1 || h.MonthDay > 28 {
			return "", fmt.Errorf("day of month must be 1–28")
		}
		return fmt.Sprintf("%d %d %d * *", h.Minute, h.Hour, h.MonthDay), nil
	default:
		return "", fmt.Errorf("unknown schedule type %q", h.Freq)
	}
}

// ParseHumanForm reads common form fields used by the web "easy schedule" UI.
func ParseHumanForm(freq, interval, hour, minute, weekday, monthDay string) (HumanSchedule, error) {
	h := HumanSchedule{Freq: freq}
	var err error
	if interval != "" {
		h.Interval, err = strconv.Atoi(interval)
		if err != nil {
			return h, fmt.Errorf("invalid interval")
		}
	}
	if hour != "" {
		h.Hour, err = strconv.Atoi(hour)
		if err != nil {
			return h, fmt.Errorf("invalid hour")
		}
	}
	if minute != "" {
		h.Minute, err = strconv.Atoi(minute)
		if err != nil {
			return h, fmt.Errorf("invalid minute")
		}
	}
	if weekday != "" {
		h.Weekday, err = strconv.Atoi(weekday)
		if err != nil {
			return h, fmt.Errorf("invalid weekday")
		}
	}
	if monthDay != "" {
		h.MonthDay, err = strconv.Atoi(monthDay)
		if err != nil {
			return h, fmt.Errorf("invalid day of month")
		}
	}
	return h, nil
}
