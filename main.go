// kb — 本地知识库工具（Go 版）。.kbx = 收编产物，.md = 草稿。
// id = 路径每段前 2 位（36 进制）拼接；front matter 只由本工具生成：title / tags / updated。
package main

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const b36 = "0123456789abcdefghijklmnopqrstuvwxyz"

var (
	E       = ".kbx"
	D       = ".md"
	TPL     = "TEMPLATE.md"
	reFM    = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n`)
	rePFX   = regexp.MustCompile(`^[0-9a-z]{2}-`)
	reID2   = regexp.MustCompile(`^[0-9a-z]{2}`)
	reBad   = regexp.MustCompile(`[\\/\x00-\x1f]`)
	kbDir   string
	dbPath  string
	usageTx = `kb — 本地知识库

  kb id <路径>                     路径 → id（工具负责推导，不要自己回溯）
  kb get <id>                      打印条目全文（含 front matter）
  kb ls <id>                       列出该目录的直接子项
  kb tree [<id>] [--depth N]       打印结构，默认 3 层
  kb add <草稿.md> [--force]       收编新增：分配该层第一个空位前缀；元数据用 --category/--title/--alias 给
  kb update <id> <草稿.md> [--to <目录id>] [--title X] [--alias a,b]   改内容；带 --to 则搬家
  kb move <id> <目标目录id>         移动一个条目或一整棵子树
  kb delete <id> --force           删除
  kb retag                         把库里所有条目的 front matter 重写为 title/tags/updated
  kb index / lint / dup / push / sync / help

front matter 由 kb 生成，字段只有 title / tags / updated。`
)

type Meta map[string]string

func die(f string, a ...any) { fmt.Printf(f+"\n", a...); os.Exit(1) }

func parseFM(text string) (Meta, string) {
	m := Meta{}
	loc := reFM.FindStringSubmatchIndex(text)
	if loc == nil {
		return m, text
	}
	for _, line := range strings.Split(text[loc[2]:loc[3]], "\n") {
		if strings.HasPrefix(line, " ") || !strings.Contains(line, ":") {
			continue
		}
		kv := strings.SplitN(line, ":", 2)
		k := strings.TrimSpace(kv[0])
		v := strings.TrimSpace(kv[1])
		v = strings.TrimSuffix(strings.TrimPrefix(v, "["), "]")
		m[k] = v
	}
	return m, text[loc[1]:]
}

func readEntry(p string) (Meta, string, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, "", err
	}
	m, body := parseFM(string(b))
	return m, body, nil
}

// tags 兼容旧的 aliases 字段
func metaTags(m Meta) string {
	if v, ok := m["tags"]; ok {
		return v
	}
	return m["aliases"]
}

func rel(p string) string {
	r, _ := filepath.Rel(kbDir, p)
	return r
}

func identOf(r string) (string, bool) {
	parts := strings.Split(filepath.ToSlash(r), "/")
	out := []string{}
	for _, s := range parts {
		if !rePFX.MatchString(s) {
			return "", false
		}
		out = append(out, s[:2])
	}
	return strings.Join(out, ""), true
}

func walkEntries() []string {
	out := []string{}
	filepath.WalkDir(kbDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), E) && d.Name() != TPL {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func walkDrafts() []string {
	out := []string{}
	filepath.WalkDir(kbDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), D) && d.Name() != TPL {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func idToPath(id string) string {
	for _, p := range walkEntries() {
		if v, ok := identOf(rel(p)); ok && v == id {
			return p
		}
	}
	var found string
	filepath.WalkDir(kbDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() || d.Name() == ".git" {
			return nil
		}
		if v, ok := identOf(rel(p)); ok && v == id {
			found = p
		}
		return nil
	})
	return found
}

func freeSlot(dir string) string {
	used := map[string]bool{}
	des, _ := os.ReadDir(dir)
	for _, d := range des {
		if d.Name() == TPL {
			continue
		}
		if m := reID2.FindString(d.Name()); m != "" {
			used[m] = true
		}
	}
	for n := 0; n < 36*36; n++ {
		s := string(b36[n/36]) + string(b36[n%36])
		if !used[s] {
			return s
		}
	}
	return ""
}

func makeFM(title, tags string) string {
	return fmt.Sprintf("---\ntitle: %s\ntags: [%s]\nupdated: %s\n---\n\n", title, tags, time.Now().Format("2006-01-02"))
}

func summary(body string) string {
	for _, l := range strings.Split(body, "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "#") && !strings.HasPrefix(l, "|") {
			s := strings.ReplaceAll(l, "**", "")
			if len([]rune(s)) > 100 {
				s = string([]rune(s)[:100])
			}
			return s
		}
	}
	return ""
}

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

func openDB() *sql.DB {
	os.MkdirAll(filepath.Dir(dbPath), 0o755)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		die("打开数据库失败: %v", err)
	}
	db.Exec(`CREATE TABLE IF NOT EXISTS entries(
      id INTEGER PRIMARY KEY, path TEXT UNIQUE, kind TEXT, title TEXT, summary TEXT,
      category TEXT, updated TEXT, tags TEXT, body TEXT, ident TEXT);
      CREATE TABLE IF NOT EXISTS aliases(entry_id INT, alias TEXT);`)
	for _, col := range []string{"updated TEXT", "tags TEXT", "ident TEXT"} {
		db.Exec("ALTER TABLE entries ADD COLUMN " + col)   // 旧库补列；已存在则报错被忽略
	}
	return db
}

func upsert(p string) {
	r := rel(p)
	m, body, _ := readEntry(p)
	db := openDB()
	defer db.Close()
	res, err := db.Exec(`INSERT INTO entries(path,kind,title,summary,category,updated,tags,body,ident)
      VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(path) DO UPDATE SET title=excluded.title,summary=excluded.summary,
      updated=excluded.updated,tags=excluded.tags,body=excluded.body,ident=excluded.ident`,
		r, "leaf", m["title"], summary(body), filepath.Dir(r), m["updated"], metaTags(m), body, mustID(r))
	if err != nil {
		return
	}
	id, _ := res.LastInsertId()
	db.Exec("DELETE FROM aliases WHERE entry_id=?", id)
	for _, t := range strings.Split(metaTags(m), ",") {
		if s := strings.TrimSpace(t); s != "" {
			db.Exec("INSERT INTO aliases VALUES(?,?)", id, s)
		}
	}
}

func mustID(r string) string { v, _ := identOf(r); return v }

func dropRow(r string) {
	db := openDB()
	defer db.Close()
	db.Exec("DELETE FROM aliases WHERE entry_id IN (SELECT id FROM entries WHERE path=?)", r)
	db.Exec("DELETE FROM entries WHERE path=?", r)
}

func rebuild() {
	db := openDB()
	defer db.Close()
	db.Exec("DELETE FROM aliases")
	db.Exec("DELETE FROM entries")
	n := 0
	for _, p := range walkEntries() {
		m, body, _ := readEntry(p)
		r := rel(p)
		res, err := db.Exec(`INSERT INTO entries(path,kind,title,summary,category,updated,tags,body,ident)
          VALUES(?,?,?,?,?,?,?,?,?)`, r, "leaf", m["title"], summary(body), filepath.Dir(r), m["updated"], metaTags(m), body, mustID(r))
		if err != nil {
			continue
		}
		id, _ := res.LastInsertId()
		for _, t := range strings.Split(metaTags(m), ",") {
			if s := strings.TrimSpace(t); s != "" {
				db.Exec("INSERT INTO aliases VALUES(?,?)", id, s)
			}
		}
		n++
	}
	fmt.Printf("indexed %d entries -> %s\n", n, dbPath)
}

func git(args ...string) (string, int) {
	c := exec.Command("git", append([]string{"-C", kbDir}, args...)...)
	b, err := c.CombinedOutput()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = 1
		}
	}
	return string(b), code
}

func commit(msg string) { git("add", "-A"); git("commit", "-qm", msg) }

func push(quiet bool) int {
	out, code := git("push", "-q", "origin", "HEAD")
	if code == 0 {
		if !quiet {
			fmt.Println("pushed -> origin")
		}
	} else {
		last := ""
		ls := strings.Split(strings.TrimSpace(out), "\n")
		if len(ls) > 0 {
			last = ls[len(ls)-1]
		}
		if len([]rune(last)) > 120 {
			last = string([]rune(last)[:120])
		}
		fmt.Println("push 失败（本地已提交，稍后 kb push 重试）：" + last)
	}
	return code
}

func flagValue(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == name {
			return true
		}
	}
	return false
}

func doID(path string) int {
	v, ok := identOf(filepath.ToSlash(path))
	if !ok {
		fmt.Printf("路径的段名前两位必须是 36 进制：%s\n", path)
		return 1
	}
	fmt.Println(v)
	return 0
}

func doGet(id string) int {
	p := idToPath(id)
	if p == "" {
		fmt.Printf("没有这个 id：%s\n", id)
		return 1
	}
	if st, err := os.Stat(p); err == nil && st.IsDir() {
		fmt.Println("（这是目录 id，下面是它的子项）")
		return doLS(id)
	}
	b, _ := os.ReadFile(p)
	fmt.Print(string(b))
	return 0
}

func doLS(id string) int {
	p := idToPath(id)
	if p == "" {
		fmt.Printf("没有这个 id：%s\n", id)
		return 1
	}
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		p = filepath.Dir(p)
	}
	des, _ := os.ReadDir(p)
	n := 0
	for _, d := range des {
		if d.Name() == TPL || strings.HasPrefix(d.Name(), ".") {
			continue
		}
		if !d.IsDir() && !strings.HasSuffix(d.Name(), E) {
			continue
		}
		r := rel(filepath.Join(p, d.Name()))
		code, _ := identOf(r)
		if d.IsDir() {
			fmt.Printf("%s  %-46s （目录）\n", code, r+"/")
		} else {
			m, body, _ := readEntry(filepath.Join(p, d.Name()))
			fmt.Printf("%s  %-46s %s\n", code, r, m["title"])
			_ = body
		}
		n++
	}
	if n == 0 {
		fmt.Println("（空）")
	}
	return 0
}

func doTree(id string, depth int) int {
	base := kbDir
	if id != "" {
		p := idToPath(id)
		if p == "" {
			fmt.Printf("没有这个 id：%s\n", id)
			return 1
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			p = filepath.Dir(p)
		}
		base = p
	}
	head := "~"
	if r := rel(base); r != "." {
		head = r
	}
	fmt.Printf("%s/  (%s)\n", head, orDash(id))
	var walk func(string, int)
	walk = func(dir string, lvl int) {
		if lvl > depth {
			return
		}
		des, _ := os.ReadDir(dir)
		for _, d := range des {
			if d.Name() == TPL || strings.HasPrefix(d.Name(), ".") {
				continue
			}
			if !d.IsDir() && !strings.HasSuffix(d.Name(), E) {
				continue
			}
			code, _ := identOf(rel(filepath.Join(dir, d.Name())))
			short := code
			if len(code) >= 2 {
				short = code[len(code)-2:]
			}
			pad := strings.Repeat("  ", lvl-1)
			if d.IsDir() {
				fmt.Printf("%s%s  %s/\n", pad, short, d.Name())
				walk(filepath.Join(dir, d.Name()), lvl+1)
			} else {
				m, _, _ := readEntry(filepath.Join(dir, d.Name()))
				fmt.Printf("%s%s  %s  — %s\n", pad, short, d.Name(), m["title"])
			}
		}
	}
	walk(base, 1)
	return 0
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func resolveCategory(cat string) (string, error) {
	cur := kbDir
	for _, part := range strings.Split(cat, "/") {
		if part == "" {
			continue
		}
		want := part
		if rePFX.MatchString(part) {
			want = part[3:]
		}
		found := ""
		if des, err := os.ReadDir(cur); err == nil {
			for _, d := range des {
				if !d.IsDir() {
					continue
				}
				if d.Name() == part || (rePFX.MatchString(d.Name()) && d.Name()[3:] == want) {
					found = filepath.Join(cur, d.Name())
					break
				}
			}
		}
		if found == "" {
			os.MkdirAll(cur, 0o755)
			slot := freeSlot(cur)
			nd := filepath.Join(cur, slot+"-"+want)
			os.MkdirAll(nd, 0o755)
			fmt.Printf("新建目录 %s\n", rel(nd))
			found = nd
		}
		cur = found
	}
	return cur, nil
}

func doAdd(draft string, force bool, args []string) int {
	raw, err := os.ReadFile(draft)
	if err != nil {
		fmt.Printf("草稿不存在：%s\n", draft)
		return 1
	}
	m, body := parseFM(string(raw))
	cat := flagValue(args, "--category")
	if cat == "" {
		cat = m["category"]
	}
	title := flagValue(args, "--title")
	if title == "" {
		title = m["title"]
	}
	tags := flagValue(args, "--alias")
	if tags == "" {
		tags = metaTags(m)
	}
	if cat == "" || title == "" || tags == "" {
		fmt.Println("缺元数据：用 --category/--title/--alias 给；front matter 由 kb 生成")
		return 1
	}
	text := makeFM(title, tags) + strings.TrimSpace(body) + "\n"
	if probs := checkProblems(parse2(text), text); len(probs) > 0 && !force {
		fmt.Println("写入被拦下 —— 需要你【两次确认】：")
		for _, x := range probs {
			fmt.Println("  - " + x)
		}
		fmt.Println("流程：先把这些连同条目原文给用户看 → 用户确认 → 再加 --force 重跑")
		return 2
	}
	d, _ := resolveCategory(cat)
	slot := freeSlot(d)
	name := strings.TrimSpace(reBad.ReplaceAllString(title, "-"))
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(draft), D)
	}
	dest := filepath.Join(d, slot+"-"+name+E)
	if err := os.WriteFile(dest, []byte(text), 0o644); err != nil {
		fmt.Println("写入失败：" + err.Error())
		return 1
	}
	fmt.Printf("收编 %s → id %s\n", rel(dest), mustID(rel(dest)))
	upsert(dest)
	commit("kb: add " + title)
	push(false)
	return 0
}

func parse2(text string) Meta { m, _ := parseFM(text); return m }

func doUpdate(id, draft, to string, args []string) int {
	p := idToPath(id)
	if p == "" {
		fmt.Printf("没有这个 id：%s\n", id)
		return 1
	}
	raw, err := os.ReadFile(draft)
	if err != nil {
		fmt.Printf("草稿不存在：%s\n", draft)
		return 1
	}
	cur := Meta{}
	cur, _, _ = readEntry(p)
	_, body := parseFM(string(raw))
	title := flagValue(args, "--title")
	if title == "" {
		title = cur["title"]
	}
	tags := flagValue(args, "--alias")
	if tags == "" {
		tags = metaTags(cur)
	}
	text := makeFM(title, tags) + strings.TrimSpace(body) + "\n"
	if probs := checkProblems(parse2(text), text); len(probs) > 0 && !hasFlag(args, "--force") {
		fmt.Println("写入被拦下 —— 需要你【两次确认】：")
		for _, x := range probs {
			fmt.Println("  - " + x)
		}
		return 2
	}
	oldRel := rel(p)
	if to != "" {
		tgt := idToPath(to)
		if tgt == "" {
			fmt.Printf("目标目录不存在：%s\n", to)
			return 1
		}
		slot := freeSlot(tgt)
		base := filepath.Base(p)
		name := base
		if rePFX.MatchString(base) {
			name = base[3:]
		}
		np := filepath.Join(tgt, slot+"-"+name)
		if _, err := os.Stat(np); err == nil {
			fmt.Printf("目标已存在：%s\n", rel(np))
			return 1
		}
		if _, code := git("mv", oldRel, rel(np)); code != 0 {
			os.Rename(p, np)
		}
		p = np
	}
	os.WriteFile(p, []byte(text), 0o644)
	if rel(p) != oldRel {
		dropRow(oldRel)
	}
	fmt.Printf("更新 %s（id %s）\n", rel(p), mustID(rel(p)))
	upsert(p)
	commit("kb: update " + title)
	push(false)
	return 0
}

func doMove(id, target string) int {
	p := idToPath(id)
	if p == "" {
		fmt.Printf("没有这个 id：%s\n", id)
		return 1
	}
	tgt := idToPath(target)
	if tgt == "" {
		fmt.Printf("目标目录不存在：%s\n", target)
		return 1
	}
	if filepath.Dir(p) == tgt {
		fmt.Println("已经在目标目录里了")
		return 0
	}
	slot := freeSlot(tgt)
	base := filepath.Base(p)
	name := base
	if rePFX.MatchString(base) {
		name = base[3:]
	}
	np := filepath.Join(tgt, slot+"-"+name)
	if _, err := os.Stat(np); err == nil {
		fmt.Printf("目标已存在：%s\n", rel(np))
		return 1
	}
	if _, code := git("mv", rel(p), rel(np)); code != 0 {
		os.Rename(p, np)
	}
	fmt.Printf("移动 %s → %s\n", rel(p), rel(np))
	rebuild()
	commit("kb: move " + rel(np))
	push(false)
	return 0
}

func doDelete(id string, force bool) int {
	p := idToPath(id)
	if p == "" {
		fmt.Printf("没有这个 id：%s\n", id)
		return 1
	}
	if !force {
		fmt.Printf("删除是破坏性操作：确认后加 --force 重跑（%s）\n", rel(p))
		return 1
	}
	if st, err := os.Stat(p); err == nil && st.IsDir() {
		des, _ := os.ReadDir(p)
		if len(des) > 0 {
			fmt.Printf("目录非空，先删里面：%s\n", rel(p))
			return 1
		}
		os.Remove(p)
	} else {
		if _, code := git("rm", "-q", rel(p)); code != 0 {
			os.Remove(p)
		}
		dropRow(rel(p))
	}
	fmt.Printf("已删除 %s（别人 id 不变）\n", rel(p))
	commit("kb: delete " + rel(p))
	push(false)
	return 0
}

func doSearch(q string, limit int, asJSON bool) int {
	db := openDB()
	defer db.Close()
	rows, err := db.Query(`SELECT path,title,summary,updated,tags,ident,body FROM entries`)
	if err != nil {
		fmt.Println("no hit")
		return 0
	}
	defer rows.Close()
	type hit struct {
		score        int
		path, title  string
		sum, upd, tg string
		ident, body  string
	}
	var hits []hit
	lq := strings.ToLower(q)
	for rows.Next() {
		var h hit
		rows.Scan(&h.path, &h.title, &h.sum, &h.upd, &h.tg, &h.ident, &h.body)
		if q == "" {
			h.score = 1
		} else {
			switch {
			case strings.Contains(strings.ToLower(h.tg), lq):
				h.score = 3
			case strings.Contains(strings.ToLower(h.title), lq):
				h.score = 2
			case strings.Contains(strings.ToLower(h.sum), lq):
				h.score = 1
			case strings.Contains(strings.ToLower(h.body), lq):
				h.score = 1
			}
		}
		if h.score > 0 {
			hits = append(hits, h)
		}
	}
	sort.Slice(hits, func(i, j int) bool {
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
		for _, h := range hits {
			fmt.Printf("{\"id\":\"%s\",\"path\":\"%s\",\"title\":\"%s\"}\n", h.ident, h.path, h.title)
		}
		return 0
	}
	for _, h := range hits {
		tg := h.tg
		if tg == "" {
			tg = "-"
		}
		up := h.upd
		if up == "" {
			up = "-"
		}
		fmt.Printf("[%s] %s\n    %s | tags: %s | updated: %s | %s\n", h.ident, h.path, h.title, tg, up, h.sum)
	}
	return 0
}

func doDup() int {
	db := openDB()
	defer db.Close()
	rows, _ := db.Query(`SELECT e.path, e.title, e.tags FROM entries e`)
	seen := map[string]string{}
	for rows.Next() {
		var p, t, tg string
		rows.Scan(&p, &t, &tg)
		keys := append([]string{t}, strings.Split(tg, ",")...)
		for _, k := range keys {
			k = strings.ToLower(strings.TrimSpace(k))
			if k == "" {
				continue
			}
			if v, ok := seen[k]; ok && v != p {
				fmt.Printf("dup: %s -> %s / %s\n", k, v, p)
			}
			seen[k] = p
		}
	}
	rows.Close()
	return 0
}

func doLint() int {
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
		if len(msgs) > 0 {
			warn++
			fmt.Printf("warn  %-42s %s\n", rel(p), strings.Join(msgs, "；"))
		} else {
			ok++
			fmt.Printf("ok    %s\n", rel(p))
		}
	}
	if dr := walkDrafts(); len(dr) > 0 {
		var rs []string
		for _, x := range dr {
			rs = append(rs, rel(x))
		}
		fmt.Println("未收编的草稿（.md）：" + strings.Join(rs, "、"))
	}
	fmt.Printf("--- 合计 %d：ok %d / warn %d；只读检查\n", ok+warn, ok, warn)
	return 0
}

// retag：把所有条目的 front matter 重写为 title/tags/updated（aliases → tags 迁移）
func doRetag() int {
	n := 0
	for _, p := range walkEntries() {
		m, body, _ := readEntry(p)
		os.WriteFile(p, []byte(makeFM(m["title"], metaTags(m))+strings.TrimSpace(body)+"\n"), 0o644)
		n++
	}
	rebuild()
	commit("kb: front matter 重写为 title/tags/updated（aliases → tags）")
	push(false)
	fmt.Printf("retag: 重写 %d 条\n", n)
	return 0
}

func syncCmd() int {
	git("pull", "--rebase", "-q")
	rebuild()
	push(false)
	return 0
}

func main() {
	kbDir = os.Getenv("KB_ROOT")
	if kbDir == "" {
		home, _ := os.UserHomeDir()
		kbDir = filepath.Join(home, ".data", ".kb")
	}
	dbPath = os.Getenv("KB_DB")
	if dbPath == "" {
		home, _ := os.UserHomeDir()
		dbPath = filepath.Join(home, ".cache", "kb.sqlite")
	}
	args := os.Args[1:]
	cmd := "help"
	if len(args) > 0 {
		cmd = args[0]
	}
	rest := args[1:]
	// 自愈：数据库为空但磁盘有条目（首次切换或表结构迁移后）→ 自动重建
	if cmd != "index" && cmd != "retag" {
		db := openDB()
		var n int
		db.QueryRow("SELECT COUNT(*) FROM entries").Scan(&n)
		db.Close()
		if n == 0 && len(walkEntries()) > 0 {
			rebuild()
		}
	}
	switch cmd {
	case "help", "-h", "--help":
		fmt.Println(usageTx)
	case "id":
		os.Exit(doID(arg(rest, 0)))
	case "get":
		os.Exit(doGet(arg(rest, 0)))
	case "ls":
		os.Exit(doLS(arg(rest, 0)))
	case "tree":
		depth := 3
		if v := flagValue(args, "--depth"); v != "" {
			fmt.Sscanf(v, "%d", &depth)
		}
		pos := []string{}
		for _, x := range rest {
			if !strings.HasPrefix(x, "--") {
				pos = append(pos, x)
			}
		}
		id := ""
		if len(pos) > 0 {
			id = pos[0]
		}
		os.Exit(doTree(id, depth))
	case "add":
		os.Exit(doAdd(arg(rest, 0), hasFlag(args, "--force"), args))
	case "update":
		to := flagValue(args, "--to")
		pos := []string{}
		for i := 0; i < len(rest); i++ {
			if strings.HasPrefix(rest[i], "--") {
				if rest[i] == "--to" || rest[i] == "--title" || rest[i] == "--alias" {
					i++
				}
				continue
			}
			pos = append(pos, rest[i])
		}
		id, draft := "", ""
		if len(pos) > 0 {
			id = pos[0]
		}
		if len(pos) > 1 {
			draft = pos[1]
		}
		os.Exit(doUpdate(id, draft, to, args))
	case "move":
		os.Exit(doMove(arg(rest, 0), arg(rest, 1)))
	case "delete":
		os.Exit(doDelete(arg(rest, 0), hasFlag(args, "--force") || hasFlag(args, "--yes")))
	case "index":
		rebuild()
	case "retag":
		os.Exit(doRetag())
	case "lint":
		os.Exit(doLint())
	case "dup":
		os.Exit(doDup())
	case "search":
		os.Exit(doSearch(arg(rest, 0), 5, hasFlag(args, "--json")))
	case "push":
		os.Exit(push(false))
	case "sync":
		os.Exit(syncCmd())
	default:
		fmt.Println(usageTx)
		os.Exit(2)
	}
}

func arg(a []string, i int) string {
	if i < len(a) {
		return a[i]
	}
	return ""
}
