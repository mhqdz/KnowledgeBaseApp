package main

import "testing"

func TestIdentOf(t *testing.T) {
	cases := map[string]string{"00-a/01-b.kbx": "0001", "00-a/00-x/02-y.kbx": "000002"}
	for in, want := range cases {
		if got, ok := identOf(in); !ok || got != want {
			t.Errorf("identOf(%q) = %q,%v want %q", in, got, ok, want)
		}
	}
	if _, ok := identOf("kb/x.kbx"); ok {
		t.Error("没有 00- 前缀的路径不该算 kb 数据")
	}
}

func TestEditDist(t *testing.T) {
	if d := editDist("lutirs", "lutris"); d != 2 {
		t.Errorf("editDist = %d want 2", d)
	}
	if d := editDist("abc", "abc"); d != 0 {
		t.Errorf("editDist same = %d want 0", d)
	}
}

func TestQueryTerms(t *testing.T) {
	got := queryTerms("arc 显存, Lutris")
	if len(got) != 3 || got[0] != "arc" || got[1] != "显存" || got[2] != "lutris" {
		t.Errorf("queryTerms = %v", got)
	}
}

func TestParseFM(t *testing.T) {
	m, body := parseFM("---\ntitle: T\ntags: [a, b]\nupdated: 2026-10-12\n---\n\n正文\n")
	if m["title"] != "T" || m["tags"] != "a, b" || m["updated"] != "2026-10-12" {
		t.Errorf("parseFM meta = %v", m)
	}
	if body != "\n正文\n" {
		t.Errorf("parseFM body = %q", body)
	}
}

func TestSplitTags(t *testing.T) {
	got := splitTags("a, b，c、d")
	if len(got) != 4 {
		t.Errorf("splitTags = %v", got)
	}
}
