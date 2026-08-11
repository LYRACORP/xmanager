package cron

import "testing"

func TestExpressionFromHuman(t *testing.T) {
	cases := []struct {
		name string
		h    HumanSchedule
		want string
	}{
		{"every minute", HumanSchedule{Freq: "minutes", Interval: 1}, "* * * * *"},
		{"every 5 min", HumanSchedule{Freq: "minutes", Interval: 5}, "*/5 * * * *"},
		{"every hour", HumanSchedule{Freq: "hours", Interval: 1, Minute: 0}, "0 * * * *"},
		{"every 6 hours", HumanSchedule{Freq: "hours", Interval: 6, Minute: 15}, "15 */6 * * *"},
		{"daily 2am", HumanSchedule{Freq: "daily", Hour: 2, Minute: 0}, "0 2 * * *"},
		{"weekdays 9am", HumanSchedule{Freq: "weekdays", Hour: 9, Minute: 0}, "0 9 * * 1-5"},
		{"weekly mon", HumanSchedule{Freq: "weekly", Hour: 9, Minute: 30, Weekday: 1}, "30 9 * * 1"},
		{"monthly 1st", HumanSchedule{Freq: "monthly", Hour: 3, Minute: 0, MonthDay: 1}, "0 3 1 * *"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExpressionFromHuman(tc.h)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
			if _, err := parseExpr(got); err != nil {
				t.Fatalf("invalid expression %q: %v", got, err)
			}
		})
	}
}
