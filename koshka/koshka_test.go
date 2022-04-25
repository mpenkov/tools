package koshka

import (
	"testing"
)

func Test_s3_split(t *testing.T) {
	bucket, key := s3_split("s3://bucket/key")
	if bucket != "bucket" {
		t.Errorf("expected bucket, got %q", bucket)
	}
	if key != "key" {
		t.Errorf("expected key, got %q", key)
	}
}
