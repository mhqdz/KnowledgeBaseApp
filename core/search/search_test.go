package search

import "testing"

func TestTerms(t *testing.T) {
	got := Terms("arc 显存, Lutris")
	if len(got) != 3 || got[0] != "arc" || got[1] != "显存" || got[2] != "lutris" {
		t.Errorf("Terms = %v", got)
	}
}

// 中文按 2-gram 展开：词序不该影响命中
func TestGrams(t *testing.T) {
	got := Grams([]string{"测试重构"})
	want := map[string]bool{"测试": true, "试重": true, "重构": true}
	if len(got) != len(want) {
		t.Fatalf("Grams = %v", got)
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("多余的项 %q", g)
		}
	}
}

// 英文不展开（英文按空格分词，展开只会制造噪音）
func TestGramsAscii(t *testing.T) {
	if got := Grams([]string{"lutris"}); len(got) != 0 {
		t.Errorf("Grams = %v", got)
	}
}
