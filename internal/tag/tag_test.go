package tag

import "testing"

func TestDist(t *testing.T) {
	if d := Dist("lutirs", "lutris"); d != 2 {
		t.Errorf("Dist = %d want 2", d)
	}
	if d := Dist("abc", "abc"); d != 0 {
		t.Errorf("Dist same = %d want 0", d)
	}
}

func TestSplit(t *testing.T) {
	got := Split("a, b，c、d; e")
	if len(got) != 5 {
		t.Errorf("Split = %v", got)
	}
}
