package koshka

import (
	//"fmt"
	"bytes"
	"log"
	"os"
	"testing"
)

func Test_s3_split(t *testing.T) {
	bucket, key := s3_split("s3://bucket/key")
	if bucket != "bucket" {
		t.Fatalf("expected bucket, got %q", bucket)
	}
	if key != "key" {
		t.Fatalf("expected key, got %q", key)
	}
}

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
