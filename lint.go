package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func checkProblems(m Meta, text string) []string {
	p := []string{}
	if strings.TrimSpace(m["title"]) == "" {
		p = append(p, "缺 title")
	}
	if strings.TrimSpace(metaTags(m)) == "" {
		p = append(p, "缺 tags")
	}
	if len(text) > 10240 {
		p = append(p, fmt.Sprintf("超过 10 KB（%d 字节）", len(text)))
	}
	if strings.Contains(text, "**") || strings.Contains(text, "__") {
		p = append(p, "有加粗标记")
	}
	return p
}

func doLint() int {
	fileN := len(walkEntries())
	ok, warn := 0, 0
	for _, p := range walkEntries() {
		m, _, _ := readEntry(p)
		text, _ := os.ReadFile(p)
		var msgs []string
		if strings.TrimSpace(m["title"]) == "" {
			msgs = append(msgs, "缺 title")
		}
		if strings.TrimSpace(metaTags(m)) == "" {
			msgs = append(msgs, "缺 tags")
		}
		if strings.TrimSpace(m["updated"]) == "" {
			msgs = append(msgs, "缺 updated")
		}
		if len(text) > 10240 {
			msgs = append(msgs, "超过 10 KB")
		}
		if strings.Contains(string(text), "**") || strings.Contains(string(text), "__") {
			msgs = append(msgs, "有加粗标记")
		}
		if !reID2.MatchString(filepath.Base(p)) {
			msgs = append(msgs, "文件名缺前缀")
		}
		// 文件名（前缀之后那段）必须等于 title
		base := filepath.Base(p)
		wantName := reBad.ReplaceAllString(strings.TrimSpace(m["title"]), "-") + E
		if rePFX.MatchString(base) {
			wantName = base[:3] + wantName
		}
		if base != wantName {
			msgs = append(msgs, "文件名与 title 不一致（应为 "+wantName+"）")
		}
		if len(msgs) > 0 {
			warn++
			fmt.Printf("warn  %-42s %s\n", rel(p), strings.Join(msgs, "；"))
		} else {
			ok++
			fmt.Printf("ok    %s\n", rel(p))
		}
	}
	// DB 与文件一致性：条目数 + 抽 3 条比对 tags
	db := openDB()
	var rowsN int
	db.QueryRow("SELECT COUNT(*) FROM entries").Scan(&rowsN)
	if rowsN != fileN {
		warn++
		fmt.Printf("warn  %-42s 数据库与文件不一致：DB %d 行 / 文件 %d 条 —— 跑 kb index 重建\n", "(全局)", rowsN, fileN)
	}
	checked := 0
	for _, p := range walkEntries() {
		if checked >= 3 {
			break
		}
		r := rel(p)
		m, _, err := readEntry(p)
		if err != nil {
			continue
		}
		var dbTags, dbIdent string
		if err := db.QueryRow("SELECT tags,ident FROM entries WHERE path=?", r).Scan(&dbTags, &dbIdent); err != nil {
			warn++
			fmt.Printf("warn  %-42s 数据库里查不到这一条 —— 跑 kb index 重建\n", r)
			continue
		}
		if strings.TrimSpace(dbTags) != strings.TrimSpace(metaTags(m)) || dbIdent != mustID(r) {
			warn++
			fmt.Printf("warn  %-42s 数据库与文件字段不一致（tags/ident）—— 跑 kb index 重建\n", r)
		}
		checked++
	}
	db.Close()
	if dr := walkDrafts(); len(dr) > 0 {
		var rs []string
		for _, x := range dr {
			rs = append(rs, rel(x))
		}
		fmt.Println("未收编的草稿（.md）：" + strings.Join(rs, "、"))
	}
	fmt.Printf("--- 条目 %d：ok %d / warn %d（warn 含全局检查）；只读\n", fileN, ok, warn)
	return 0
}
