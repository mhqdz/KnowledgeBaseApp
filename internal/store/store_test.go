package store

import "testing"

func TestIdentOf(t *testing.T) {
	cases := map[string]string{"00-a/01-b.kbx": "0001", "00-a/00-x/02-y.kbx": "000002"}
	for in, want := range cases {
		if got, ok := IdentOf(in); !ok || got != want {
			t.Errorf("IdentOf(%q) = %q,%v want %q", in, got, ok, want)
		}
	}
	if _, ok := IdentOf("kb/x.kbx"); ok {
		t.Error("没有 00- 前缀的路径不该算 kb 数据")
	}
}
