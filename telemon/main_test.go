package main

import (
	"testing"
)

func TestSeqRange(t *testing.T) {
	for i, tc := range []struct{
		max uint32
		seen uint32
		count uint32
		wantFrom uint32
		wantTo uint32
	} {
		{10, 0, 10, 1, 10}, 
		{10, 0, 5, 5, 10}, 
		{10, 0, 100, 1, 10}, 
		{10, 7, 5, 8, 10}, 
		{10, 9, 5, 10, 10}, 
		{10, 10, 5, 0, 0}, 
	} {
		gotFrom, gotTo := seqRange(tc.max, tc.seen, tc.count)
		if gotFrom != tc.wantFrom {
			t.Errorf("[%d] from want: %d got: %d", i, tc.wantFrom, gotFrom)
		}
		if gotTo != tc.wantTo {
			t.Errorf("[%d]   to want: %d got: %d", i, tc.wantTo, gotTo)
		}
	}
}
