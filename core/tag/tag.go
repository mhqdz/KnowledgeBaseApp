package tag

import (
	"fmt"
	"sort"
	"strings"

	"kbapp/core/entry"
	"kbapp/core/store"
)

// ---------- 相似 tag 检查 ----------
func Dist(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			m := prev[j] + 1
			if cur[j-1]+1 < m {
				m = cur[j-1] + 1
			}
			if prev[j-1]+cost < m {
				m = prev[j-1] + cost
			}
			cur[j] = m
		}
		prev = cur
	}
	return prev[len(rb)]
}

func Split(s string) []string {
	out := []string{}
	for _, t := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '，' || r == '、' || r == ';' || r == '；' || r == ' '
	}) {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func Existing() []string {
	set := map[string]bool{}
	for _, p := range store.WalkEntries() {
		m, _, _ := entry.Read(p)
		for _, t := range Split(m["tags"]) {
			set[t] = true
		}
	}
	out := []string{}
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// 规则：同一份 tags 内部有包含/近似 → 必须确认；跨条目：拼写近似必须确认，包含关系只提示
func Similar(tags string, self string) (blocking []string, hints []string) {
	list := Split(tags)
	for a := 0; a < len(list); a++ {
		for b := a + 1; b < len(list); b++ {
			l1, l2 := strings.ToLower(list[a]), strings.ToLower(list[b])
			if strings.Contains(l1, l2) || strings.Contains(l2, l1) || Dist(l1, l2) <= 1 {
				blocking = append(blocking, fmt.Sprintf("同一条目里两个 tag 重复或近似：%q 与 %q —— 只留一个（一般留更通用的那个）", list[a], list[b]))
			}
		}
	}
	selfSet := map[string]bool{}
	if self != "" {
		if m, _, err := entry.Read(self); err == nil {
			for _, t := range Split(m["tags"]) {
				selfSet[strings.ToLower(t)] = true
			}
		}
	}
	for _, nt := range list {
		l1 := strings.ToLower(nt)
		for _, et := range Existing() {
			l2 := strings.ToLower(et)
			if l1 == l2 || selfSet[l2] {
				continue
			}
			n := len([]rune(l1))
			if m := len([]rune(l2)); m > n {
				n = m
			}
			d := Dist(l1, l2)
			if d > 0 && ((n >= 4 && d*3 <= n) || (n < 4 && d <= 1)) {
				blocking = append(blocking, fmt.Sprintf("新 tag 与已有 tag 拼写近似：%q ~ %q（是否改用已有的 %q？）", nt, et, et))
			} else if strings.Contains(l1, l2) || strings.Contains(l2, l1) {
				blocking = append(blocking, fmt.Sprintf("tag %q 与其它条目的 %q 有包含关系：tags 只写上层/通用词，建议改成 %q", nt, et, et))
			}
		}
	}
	return
}
