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

func TestTranslate(t *testing.T) {
	table := map[string]string{
		`(.+) さんは、\s*(\d+時\d+分)\s*(.+を通過しました)。`: "{1}は{2}に{3}",
		`山田　(.+)さんは(\d+:\d+)に(..されました)。`:        "{1}は{2}に{3}",
	}
	first := `Alice さんは、
17時13分
東玄関を通過しました。`
	for _, tc := range []struct {
		input string
		want  string
	}{
		{first, "Aliceは17時13分に東玄関を通過しました"},
		{
			`山田　タロウさんは8:57に入室されました。`,
			`タロウは8:57に入室されました`,
		},
		{
			`山田　タロウさんは18:57に退室されました。`,
			`タロウは18:57に退室されました`,
		},
	} {
		got := translate(tc.input, table, false)
		if tc.want != got {
			t.Errorf("want: %q got: %q", tc.want, got)
		}
	}
}
