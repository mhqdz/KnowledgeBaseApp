package search

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"kbapp/internal/index"
	"kbapp/internal/tag"
)

// ---------- 检索分词 ----------
func Terms(q string) []string {
	out := []string{}
	for _, t := range strings.FieldsFunc(q, func(r rune) bool {
		return r == ' ' || r == ',' || r == '，' || r == '、' || r == '/' || r == '|' || r == ';' || r == '；'
	}) {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, strings.ToLower(t))
		}
	}
	return out
}

// Grams 把中文词展开成 2-gram。中文没有空格分词，整段当一个词做子串匹配的话，
// 词序一变就搜不到（tags 是「重构测试」，搜「测试重构」原本 no hit）。
// 只在"整词一个都没命中"时作为退路使用，并且要求命中率够高 —— 否则
// 「阿尔法不存在」会因为里面的「阿尔」误命中。
func Grams(words []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, w := range words {
		if isASCII(w) {
			continue
		}
		r := []rune(w)
		for i := 0; i+1 < len(r); i++ {
			g := string(r[i : i+2])
			if !seen[g] {
				seen[g] = true
				out = append(out, g)
			}
		}
	}
	return out
}

// scoreOf 按词表给一条记录打分：tags 命中 3 分、标题 2 分、摘要 1 分，
// 都不中是编辑距离近似（1 分）。
func scoreOf(tg, ti, sm string, terms []string) (score, matched int) {
	for _, t := range terms {
		got := 0
		if strings.Contains(tg, t) {
			got = 3
		} else if strings.Contains(ti, t) {
			got = 2
		} else if strings.Contains(sm, t) {
			got = 1
		} else {
			for _, et := range tag.Split(tg + "," + ti) {
				if d := tag.Dist(et, t); d > 0 && (d <= 1 || (len([]rune(t)) >= 4 && d*3 <= len([]rune(t)))) {
					got = 1
					break
				}
			}
		}
		if got > 0 {
			score += got
			matched++
		}
	}
	return
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}

func Run(q string, limit int, asJSON bool) int {
	db := index.Open()
	defer db.Close()
	rows, err := db.Query(`SELECT path,title,summary,updated,tags,ident FROM entries`)
	if err != nil {
		fmt.Println("no hit")
		return 0
	}
	defer rows.Close()
	type hit struct {
		score        int
		matched      int
		path, title  string
		sum, upd, tg string
		ident        string
	}
	tm := Terms(q)
	lq := strings.ToLower(strings.TrimSpace(q))
	var hits []hit
	for rows.Next() {
		var h hit
		rows.Scan(&h.path, &h.title, &h.sum, &h.upd, &h.tg, &h.ident)
		if q == "" {
			h.score, h.matched = 1, 1
			hits = append(hits, h)
			continue
		}
		tg := strings.ToLower(h.tg)
		ti := strings.ToLower(h.title)
		sm := strings.ToLower(h.sum)
		score, matched := scoreOf(tg, ti, sm, tm)
		// 整串子串命中（老行为）也算一部分分
		if strings.Contains(tg, lq) || strings.Contains(ti, lq) {
			score += 6
		} else if strings.Contains(sm, lq) {
			score += 2
		}
		if matched == 0 {
			// 整词一个都没中：退一步用 2-gram（只解决中文词序），
			// 命中率不到 60% 就当没中，避免长串里的两个字误命中。
			if gs := Grams(tm); len(gs) > 0 {
				if gScore, gHit := scoreOf(tg, ti, sm, gs); float64(gHit)/float64(len(gs)) >= 0.6 {
					score, matched = gScore/2, gHit
				}
			}
		}
		if matched > 0 {
			h.score, h.matched = score, matched
			hits = append(hits, h)
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].matched != hits[j].matched {
			return hits[i].matched > hits[j].matched
		}
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].ident < hits[j].ident
	})
	if len(hits) == 0 {
		fmt.Println("no hit")
		return 0
	}
	if len(hits) > limit {
		hits = hits[:limit]
	}
	if asJSON {
		out := []map[string]string{}
		for _, h := range hits {
			out = append(out, map[string]string{"id": h.ident, "path": h.path, "title": h.title, "tags": h.tg, "updated": h.upd, "summary": h.sum})
		}
		b, _ := json.MarshalIndent(out, "", " ")
		fmt.Println(string(b))
		return 0
	}
	for _, h := range hits {
		tg, up := h.tg, h.upd
		if tg == "" {
			tg = "-"
		}
		if up == "" {
			up = "-"
		}
		fmt.Printf("[%s] %s\n    %s | tags: %s | updated: %s | %s\n", h.ident, h.path, h.title, tg, up, h.sum)
	}
	return 0
}
