package main

import (
	"testing"
)

func TestSeqRange(t *testing.T) {
	for i, tc := range []struct {
		max      uint32
		count    uint32
		wantFrom uint32
		wantTo   uint32
	}{
		{10, 10, 1, 10},
		{10, 5, 5, 10},
		{10, 100, 1, 10},
	} {
		gotFrom, gotTo := seqRange(tc.max, tc.count)
		if gotFrom != tc.wantFrom {
			t.Errorf("[%d] from want: %d got: %d", i, tc.wantFrom, gotFrom)
		}
		if gotTo != tc.wantTo {
			t.Errorf("[%d]   to want: %d got: %d", i, tc.wantTo, gotTo)
		}
	}
}
