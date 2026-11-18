package main

import (
	"slices"
	"testing"
	"time"
)

func TestShouldPing(t *testing.T) {
	cases := []struct {
		name          string
		lastSlot, key string
		want          bool
	}{
		{"no prior state", "", "2026-08-24_06", true},
		{"same slot", "2026-08-24_06", "2026-08-24_06", false},
		{"different slot", "2026-08-24_06", "2026-08-24_11", true},
		{"different day, same hour", "2026-08-24_06", "2026-08-25_06", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shouldPing(c.lastSlot, c.key); got != c.want {
				t.Errorf("shouldPing(%q, %q) = %v, want %v", c.lastSlot, c.key, got, c.want)
			}
		})
	}
}

func TestBuildCrontab(t *testing.T) {
	want := "SHELL=/app/reset\n0 6,11,16,21 * * * /app/reset -scheduled\n"
	if got := buildCrontab([]int{6, 11, 16, 21}); got != want {
		t.Errorf("buildCrontab = %q, want %q", got, want)
	}
}

func TestParseHours(t *testing.T) {
	if got, err := parseHours(" 6, 11,16,21"); err != nil || !slices.Equal(got, []int{6, 11, 16, 21}) {
		t.Errorf("parseHours(valid) = %v, %v", got, err)
	}
	for _, bad := range []string{"", "24", "-1", "6,,11", "6;11", "a"} {
		if _, err := parseHours(bad); err == nil {
			t.Errorf("parseHours(%q) accepted a bad value", bad)
		}
	}
}

// Fire times from real logs: the Mac in Sep 2026 (fired late after waking
// from sleep) and the Windows host in Oct-Nov 2026 (fired a few seconds
// early).
func TestOnTime(t *testing.T) {
	hours := []int{6, 11, 16, 21}
	cases := []struct {
		at   string
		want bool
	}{
		{"11:00:00", true},
		{"10:59:57", true}, // Windows, 3s early
		{"05:59:58", true},
		{"06:00:05", true},
		{"16:04:35", true},
		{"21:09:59", true},
		{"10:58:59", false}, // more than earlyGrace early
		{"21:10:00", false},
		{"21:43:50", false}, // Mac, 43m late
		{"23:10:41", false}, // Mac, after waking
		{"14:18:52", false},
	}
	for _, c := range cases {
		now, _ := time.Parse("15:04:05", c.at)
		if got := onTime(now, hours); got != c.want {
			t.Errorf("onTime(%s) = %v, want %v", c.at, got, c.want)
		}
	}
}

// The Windows host's logs labelled an 11:00 ping as slot "..._10" because the
// key came from the early fire time.
func TestSlotStart(t *testing.T) {
	now := time.Date(2026, 10, 30, 10, 59, 57, 0, time.UTC)
	if got := slotStart(now).Format("2006-01-02_15"); got != "2026-10-30_11" {
		t.Errorf("slotStart(10:59:57) = %s, want 2026-10-30_11", got)
	}
}
