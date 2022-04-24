package main

import (
	"testing"
	"time"
)

func TestToMonday(t *testing.T) {
	var testcases = []struct {
		input string
		expected string
	}{
		{"2022-04-24", "2022-04-25"},
		{"2022-04-25", "2022-04-25"},
		{"2022-04-26", "2022-05-02"},
		{"2022-04-27", "2022-05-02"},
		{"2022-04-28", "2022-05-02"},
		{"2022-04-29", "2022-05-02"},
		{"2022-04-30", "2022-05-02"},
		{"2022-05-01", "2022-05-02"},
	}
	for _, tc := range testcases {
		input, _ := time.Parse("2006-01-02", tc.input)
		expected, _ := time.Parse("2006-01-02", tc.expected)
		actual := toMonday(input)
		if actual != expected {
			t.Errorf("input: %s expected: %s actual: %s", input, expected, actual)
		}
	}
}
