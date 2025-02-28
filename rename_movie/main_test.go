package main

import (
	"testing"
)

func TestExtractYear(t *testing.T) {
	testCases := []struct {
		input string
		want string
	}{
		{"Being There (1979) BDRip.mkv", "1979"},
		{"Casino.1995.1080p.BluRay.x264.anoXmous", "1995"},
	}
	for _, tc := range testCases {
		got := extractYear(tc.input)
		if got != tc.want {
			t.Errorf("input=%q want=%q got=%q", tc.input, tc.want, got)
		}
	}
}

func TestExtractTitle(t *testing.T) {
	testCases := []struct {
		input string
		year string
		want string
	}{
		{"Being There (1979) BDRip.mkv", "1979", "being there"},
		{"Casino.1995.1080p.BluRay.x264.anoXmous", "1995", "casino"},
	}
	for _, tc := range testCases {
		got := extractTitle(tc.input, tc.year)
		if got != tc.want {
			t.Errorf("input=%q want=%q got=%q", tc.input, tc.want, got)
		}
	}
}

func TestNormalizetitle(t *testing.T) {
	testCases := []struct {
		input string
		want string
	}{
		{"Being There", "Being.There"},
		{"Me,.Myself.&.Irene", "Me.Myself.and.Irene"},
	}
	for _, tc := range testCases {
		got := normalizeTitle(tc.input)
		if got != tc.want {
			t.Errorf("input=%q want=%q got=%q", tc.input, tc.want, got)
		}
	}
}

func TestLookup(t *testing.T) {
	cand, err := lookup("Console Wars", "", "movie")
	if err != nil {
		t.Fatal(err)
	}

	if len(cand) == 0 {
		t.Fatal("expected to see some candidates")
	}

	if want := "Console.Wars.2020"; cand[0] != want {
		t.Errorf("want=%q got=%q", want, cand[0])
	}
}
