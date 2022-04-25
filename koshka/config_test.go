package koshka

import (
	//"fmt"
	"bytes"
	"log"
	"os"
	"testing"
)

func resetLog() {
	log.SetOutput(os.Stderr)
}

func Test_findConfig(t *testing.T) {
	testCases := make(map[string]map[string]string)
	testCases["s3://mybucket"] = map[string]string{
		"endpoint_url": "http://localhost:4566",
	}
	testCases["s3://mybucket/whatever.txt.gz"] = map[string]string{
		"endpoint_url": "http://localhost:4566",
	}
	testCases["https://example.com"] = map[string]string{
		"username": "secret",
		"password": "nonono",
		"alias": "example",
	}
	testCases["s3://thatswhatshesaid"] = map[string]string{
		"alias": "toolong",
	}

	for tc := range(testCases) {
		var buf bytes.Buffer
		log.SetOutput(&buf)

		actual, err := findConfig(tc, "sample.cfg")

		resetLog()
		if buf.String() != "" {
			t.Log(buf.String())
		}

		if err != nil {
			t.Fatalf("tc: %q expected err to be nil, got %q", tc, err)
		}

		expected := testCases[tc]
		for key := range expected {
			if expected[key] != actual[key] {
				t.Fatalf("tc: %q expected %q, got %q", tc, expected[key], actual[key])
			}
		}
	}
}

func TestLoadConfig(t *testing.T) {
	actual, err := LoadConfig("sample.cfg")
	if err != nil {
		t.Fatalf("unexpected err: %q", err)
	}
	expected := []CfgSection{
		{"s3://mybucket", map[string]string{"endpoint_url": "http://localhost:4566"}},
		{"https://example.com", map[string]string{"username": "secret", "password": "nonono", "alias": "example"}},
		{"s3://thatswhatshesaid", map[string]string{"alias": "toolong"}},
	}
	if len(actual) != len(expected) {
		t.Errorf("expected len() to be %d, got %d instead", len(expected), len(actual))
	}
	for i := range expected {
		if actual[i].name != expected[i].name {
			t.Errorf("actual[%d].name expected: %q actual: %q", i, expected, actual)
		}
		if len(actual[i].items) != len(expected[i].items) {
			t.Errorf(
				"len(actual[%d].items): expected: %q actual: %q",
				i,
				len(expected[i].items),
				len(actual[i].items),
			)
		}
		for key := range(expected[i].items) {
			if actual[i].items[key] != expected[i].items[key] {
				t.Errorf(
					"actual[%d].items[%q] expected: %q actual: %q",
					i,
					key,
					expected[i].items[key],
					actual[i].items[key],
				)
			}
		}
	}
}
