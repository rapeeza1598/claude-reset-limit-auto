package main

import "testing"

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
	want := "SHELL=/app/reset\n0 6,11,16,21 * * * /app/reset\n"
	if got := buildCrontab("6,11,16,21"); got != want {
		t.Errorf("buildCrontab(%q) = %q, want %q", "6,11,16,21", got, want)
	}
}
