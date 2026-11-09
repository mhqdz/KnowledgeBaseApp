package entry

import "testing"

func TestParseFM(t *testing.T) {
	m, body := ParseFM("---\ntitle: T\ntags: [a, b]\nupdated: 2026-10-12\n---\n\n正文\n")
	if m["title"] != "T" || m["tags"] != "a, b" || m["updated"] != "2026-10-12" {
		t.Errorf("ParseFM meta = %v", m)
	}
	if body != "\n正文\n" {
		t.Errorf("ParseFM body = %q", body)
	}
}
