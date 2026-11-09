package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ---------- 检索分词 ----------
func queryTerms(q string) []string {
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

func doSearch(q string, limit int, asJSON bool) int {
	db := openDB()
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
	tm := queryTerms(q)
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
		score, matched := 0, 0
		// 整串子串命中（老行为）也算一部分分
		if strings.Contains(tg, lq) || strings.Contains(ti, lq) {
			score += 6
		} else if strings.Contains(sm, lq) {
			score += 2
		}
		for _, t := range tm {
			got := 0
			if strings.Contains(tg, t) {
				got = 3
			} else if strings.Contains(ti, t) {
				got = 2
			} else if strings.Contains(sm, t) {
				got = 1
			} else { // 近似：tags/标题里有没有编辑距离很近的
				for _, et := range splitTags(tg + "," + ti) {
					if d := editDist(et, t); d > 0 && (d <= 1 || (len([]rune(t)) >= 4 && d*3 <= len([]rune(t)))) {
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
