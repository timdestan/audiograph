package main

import (
	"testing"
	"time"
)

func TestParsePeriod(t *testing.T) {
	t.Run("all-time variants", func(t *testing.T) {
		for _, input := range []string{"", "all"} {
			got, label, err := parsePeriod(input)
			if err != nil {
				t.Errorf("parsePeriod(%q): unexpected error: %v", input, err)
			}
			if label != "all" {
				t.Errorf("parsePeriod(%q) label = %q, want \"all\"", input, label)
			}
			if !got.IsZero() {
				t.Errorf("parsePeriod(%q) time = %v, want zero", input, got)
			}
		}
	})

	t.Run("valid durations", func(t *testing.T) {
		cases := []struct {
			input    string
			label    string
			expected func() time.Time
		}{
			{"1d", "1d", func() time.Time { return time.Now().AddDate(0, 0, -1) }},
			{"7d", "7d", func() time.Time { return time.Now().AddDate(0, 0, -7) }},
			{"27d", "27d", func() time.Time { return time.Now().AddDate(0, 0, -27) }},
			{"2w", "2w", func() time.Time { return time.Now().AddDate(0, 0, -14) }},
			{"4w", "4w", func() time.Time { return time.Now().AddDate(0, 0, -28) }},
			{"1m", "1m", func() time.Time { return time.Now().AddDate(0, -1, 0) }},
			{"3m", "3m", func() time.Time { return time.Now().AddDate(0, -3, 0) }},
			{"1y", "1y", func() time.Time { return time.Now().AddDate(-1, 0, 0) }},
			{"2y", "2y", func() time.Time { return time.Now().AddDate(-2, 0, 0) }},
		}
		for _, tc := range cases {
			want := tc.expected()
			got, label, err := parsePeriod(tc.input)
			if err != nil {
				t.Errorf("parsePeriod(%q): unexpected error: %v", tc.input, err)
				continue
			}
			if label != tc.label {
				t.Errorf("parsePeriod(%q) label = %q, want %q", tc.input, label, tc.label)
			}
			if diff := got.Sub(want); diff > time.Second || diff < -time.Second {
				t.Errorf("parsePeriod(%q) time off by %v", tc.input, diff)
			}
		}
	})

	t.Run("errors", func(t *testing.T) {
		for _, input := range []string{
			"0d",   // zero is not a positive number
			"-5d",  // negative
			"d",    // missing number
			"5x",   // unknown unit
			"abc",  // not a number at all
			"1",    // missing unit
			"week", // words not supported
		} {
			_, _, err := parsePeriod(input)
			if err == nil {
				t.Errorf("parsePeriod(%q): expected error, got nil", input)
			}
		}
	})
}
