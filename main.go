// kb — 本地知识库工具（Go 版）。.kbx = 收编产物，.md = 草稿。
// id = 路径每段前 2 位（36 进制）拼接；front matter 只由本工具生成：title / tags / updated。
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const b36 = "0123456789abcdefghijklmnopqrstuvwxyz"

var buildVersion = "dev"

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
  kb add <草稿.md> [--force]       收编新增：分配该层第一个空位前缀；元数据用 --category/--title/--tags 给
  kb update <id> <草稿.md> [--to <目录id>] [--title X] [--tags a,b]   改内容；带 --to 则搬家
  kb move <id> <目标目录id>         移动一个条目或一整棵子树
  kb delete <id> --force           删除
  kb version                       打印版本（构建时的 commit）
  kb config [set <key> <value> | unset <key>]   读写 ~/.config/kb/config.json
  kb help -w                       打印写入须知（指向 config 里的 write_guide 条目）
  kb index / lint / push / sync / help

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

func metaTags(m Meta) string { return m["tags"] }

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
	res := ""
	filepath.WalkDir(kbDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			if v, ok := identOf(rel(p)); ok && v == id {
				res = p
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), E) && d.Name() != TPL {
			if v, ok := identOf(rel(p)); ok && v == id {
				res = p
			}
		}
		return nil
	})
	return res
}

// slotTaken 判断目录里是否已有条目占用某个槽前缀（id 由每段路径前缀组成，槽不能重复）
func slotTaken(dir, slot string) bool {
	fs, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, f := range fs {
		if !f.IsDir() && strings.HasPrefix(f.Name(), slot+"-") {
			return true
		}
	}
	return false
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

// ---------- 相似 tag 检查 ----------
func editDist(a, b string) int {
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

func splitTags(s string) []string {
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

func existingTags() []string {
	set := map[string]bool{}
	for _, p := range walkEntries() {
		m, _, _ := readEntry(p)
		for _, t := range splitTags(m["tags"]) {
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
func similarTags(tags string, self string) (blocking []string, hints []string) {
	list := splitTags(tags)
	for a := 0; a < len(list); a++ {
		for b := a + 1; b < len(list); b++ {
			l1, l2 := strings.ToLower(list[a]), strings.ToLower(list[b])
			if strings.Contains(l1, l2) || strings.Contains(l2, l1) || editDist(l1, l2) <= 1 {
				blocking = append(blocking, fmt.Sprintf("同一条目里两个 tag 重复或近似：%q 与 %q —— 只留一个（一般留更通用的那个）", list[a], list[b]))
			}
		}
	}
	selfSet := map[string]bool{}
	if self != "" {
		if m, _, err := readEntry(self); err == nil {
			for _, t := range splitTags(m["tags"]) {
				selfSet[strings.ToLower(t)] = true
			}
		}
	}
	for _, nt := range list {
		l1 := strings.ToLower(nt)
		for _, et := range existingTags() {
			l2 := strings.ToLower(et)
			if l1 == l2 || selfSet[l2] {
				continue
			}
			n := len([]rune(l1))
			if m := len([]rune(l2)); m > n {
				n = m
			}
			d := editDist(l1, l2)
			if d > 0 && ((n >= 4 && d*3 <= n) || (n < 4 && d <= 1)) {
				blocking = append(blocking, fmt.Sprintf("新 tag 与已有 tag 拼写近似：%q ~ %q（是否改用已有的 %q？）", nt, et, et))
			} else if strings.Contains(l1, l2) || strings.Contains(l2, l1) {
				blocking = append(blocking, fmt.Sprintf("tag %q 与其它条目的 %q 有包含关系：tags 只写上层/通用词，建议改成 %q", nt, et, et))
			}
		}
	}
	return
}

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
      category TEXT, updated TEXT, tags TEXT, ident TEXT);
      CREATE TABLE IF NOT EXISTS tags(entry_id INT, tag TEXT);`)
	for _, col := range []string{"updated TEXT", "tags TEXT", "ident TEXT"} {
		db.Exec("ALTER TABLE entries ADD COLUMN " + col) // 旧库补列；已存在则报错被忽略
	}
	db.Exec("DROP TABLE IF EXISTS aliases") // 旧表名，已废弃（数据是派生的，直接丢）
	return db
}

func upsert(p string) {
	r := rel(p)
	m, body, _ := readEntry(p)
	db := openDB()
	defer db.Close()
	_, err := db.Exec(`INSERT INTO entries(path,kind,title,summary,category,updated,tags,ident)
      VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(path) DO UPDATE SET title=excluded.title,summary=excluded.summary,
      updated=excluded.updated,tags=excluded.tags,ident=excluded.ident`,
		r, "leaf", m["title"], summary(body), filepath.Dir(r), m["updated"], metaTags(m), mustID(r))
	if err != nil {
		return
	}
	var id int64
	if err := db.QueryRow("SELECT id FROM entries WHERE path=?", r).Scan(&id); err != nil || id == 0 {
		fmt.Printf("  !! 取条目 id 失败（path=%s）：%v —— tags 未写入，请跑 kb index 重建\n", r, err)
		return
	}
	db.Exec("DELETE FROM tags WHERE entry_id=?", id)
	for _, t := range strings.Split(metaTags(m), ",") {
		if s := strings.TrimSpace(t); s != "" {
			db.Exec("INSERT INTO tags VALUES(?,?)", id, s)
		}
	}
}

func mustID(r string) string { v, _ := identOf(r); return v }

func dropRow(r string) {
	db := openDB()
	defer db.Close()
	db.Exec("DELETE FROM tags WHERE entry_id IN (SELECT id FROM entries WHERE path=?)", r)
	db.Exec("DELETE FROM entries WHERE path=?", r)
}

func rebuild() {
	db := openDB()
	defer db.Close()
	db.Exec("DELETE FROM tags")
	db.Exec("DELETE FROM entries")
	n := 0
	for _, p := range walkEntries() {
		m, body, _ := readEntry(p)
		r := rel(p)
		res, err := db.Exec(`INSERT INTO entries(path,kind,title,summary,category,updated,tags,ident)
          VALUES(?,?,?,?,?,?,?,?)`, r, "leaf", m["title"], summary(body), filepath.Dir(r), m["updated"], metaTags(m), mustID(r))
		if err != nil {
			fmt.Println("  !! 写入数据库失败：" + err.Error())
			continue
		}
		id, _ := res.LastInsertId()
		for _, t := range strings.Split(metaTags(m), ",") {
			if s := strings.TrimSpace(t); s != "" {
				db.Exec("INSERT INTO tags VALUES(?,?)", id, s)
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

func commit(msg string) {
	git("add", "-A")
	if out, code := git("commit", "-qm", msg); code != 0 {
		low := strings.ToLower(out)
		if !strings.Contains(low, "nothing to commit") && !strings.Contains(low, "无文件要提交") {
			fmt.Println("  !! git commit 失败：" + strings.TrimSpace(firstLine(out)))
		}
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

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

func dbg(format string, a ...any) {
	if os.Getenv("KB_DEBUG") == "1" {
		fmt.Fprintf(os.Stderr, "[kb] "+format+"\n", a...)
	}
}

// 会带一个值参数的 flag（其余 --xxx 视为布尔开关）
var valueFlags = map[string]bool{"--to": true, "--title": true, "--tags": true, "--category": true, "--depth": true, "--range": true, "--port": true, "--searxng": true, "--offset": true, "--max-chars": true, "--timeout": true, "--n": true, "-n": true, "--limit": true, "-limit": true}

// positional 取第 n 个位置参数（跳过 flag 与其值，flag 放在哪里都行）
func positional(args []string, n int) string {
	pos := []string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "--") {
			if !strings.Contains(a, "=") && valueFlags[a] {
				i++
			}
			continue
		}
		pos = append(pos, a)
	}
	if n >= 0 && n < len(pos) {
		return pos[n]
	}
	return ""
}

// ---------- 配置（~/.config/kb/config.json）----------

func kbConfigPath() string {
	if v := os.Getenv("KB_CONFIG"); v != "" {
		return v
	}
	return filepath.Join(os.Getenv("HOME"), ".config", "kb", "config.json")
}

func loadConfig() map[string]string {
	m := map[string]string{}
	b, err := os.ReadFile(kbConfigPath())
	if err != nil {
		return m
	}
	var raw map[string]any
	if json.Unmarshal(b, &raw) != nil {
		return m
	}
	for k, v := range raw {
		if s, ok := v.(string); ok {
			m[k] = s
		}
	}
	return m
}

func saveConfig(m map[string]string) error {
	p := kbConfigPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(p, append(b, '\n'), 0o644)
}

// doConfig：kb config（打印）/ kb config set <key> <value> / kb config unset <key>
func doConfig(args []string) int {
	pos := []string{}
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			pos = append(pos, a)
		}
	}
	m := loadConfig()
	if len(pos) == 0 {
		fmt.Println("# " + kbConfigPath())
		b, _ := json.MarshalIndent(m, "", "  ")
		fmt.Println(string(b))
		return 0
	}
	switch pos[0] {
	case "set":
		if len(pos) < 3 {
			fmt.Println("用法: kb config set <key> <value>")
			return 4
		}
		m[pos[1]] = pos[2]
		if err := saveConfig(m); err != nil {
			fmt.Println("写配置失败：" + err.Error())
			return 1
		}
		fmt.Printf("已设置 %s = %s\n", pos[1], pos[2])
		return 0
	case "unset":
		if len(pos) < 2 {
			fmt.Println("用法: kb config unset <key>")
			return 4
		}
		delete(m, pos[1])
		if err := saveConfig(m); err != nil {
			fmt.Println("写配置失败：" + err.Error())
			return 1
		}
		fmt.Printf("已删除 %s\n", pos[1])
		return 0
	default:
		fmt.Println("用法: kb config | kb config set <key> <value> | kb config unset <key>")
		return 4
	}
}

// doHelpWrite：打印写入须知（由配置 write_guide 指向的条目）
func doHelpWrite() int {
	m := loadConfig()
	id := m["write_guide"]
	if id == "" {
		fmt.Print(`未设置写入须知条目。设置方法：
  kb config set write_guide <条目id>

要点（完整规则见条目）：
- 任何写入/修改都先把完整原文给用户看，等确认后再写
- .kbx 不得手改；要改先拷贝成草稿 .md
`)
		return 1
	}
	p := idToPath(id)
	if p == "" {
		fmt.Printf("配置的写入须知条目不存在：%s（用 kb config set write_guide <id> 修正）\n", id)
		return 1
	}
	b, err := os.ReadFile(p)
	if err != nil {
		fmt.Println("读取失败：" + err.Error())
		return 1
	}
	fmt.Printf("# 写入须知（%s  %s）\n\n%s", mustID(rel(p)), rel(p), string(b))
	return 0
}

func flagValue(args []string, name string) string {
	// 全量扫描：flag 可以出现在位置参数之后（kb update <id> --to <dir>）
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == name {
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		}
		if strings.HasPrefix(a, name+"=") {
			return strings.TrimPrefix(a, name+"=")
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

type treeNode struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"`
	Count    int         `json:"count"`
	IsEntry  bool        `json:"is_entry,omitempty"`
	Children []*treeNode `json:"children,omitempty"`
}

// doTree 打印目录概览：节点名 + 该节点下的条目数；默认隐藏条目（叶子），-a 显示
func doTree(argID string, depth int, showAll, asJSON bool) int {
	base := kbDir
	if argID != "" {
		p := idToPath(argID)
		if p == "" {
			fmt.Printf("没有这个 id：%s\n", argID)
			return 1
		}
		if st, err := os.Stat(p); err != nil || !st.IsDir() {
			fmt.Printf("不是目录：%s\n", argID)
			return 1
		}
		base = p
	}
	root := &treeNode{Name: filepath.Base(rel(base)), Path: rel(base)}
	if root.Path == "." {
		root.Name, root.Path = "/", ""
	}
	var insert func(n *treeNode, parts []string, full string)
	insert = func(n *treeNode, parts []string, full string) {
		if len(parts) == 1 {
			n.Children = append(n.Children, &treeNode{Name: parts[0], Path: full, IsEntry: true, Count: 1})
			return
		}
		var child *treeNode
		for _, c := range n.Children {
			if !c.IsEntry && c.Name == parts[0] {
				child = c
				break
			}
		}
		if child == nil {
			child = &treeNode{Name: parts[0], Path: filepath.Join(n.Path, parts[0])}
			n.Children = append(n.Children, child)
		}
		insert(child, parts[1:], full)
	}
	for _, e := range walkEntries() {
		r := rel(e)
		if root.Path != "" && !strings.HasPrefix(r, root.Path+string(filepath.Separator)) {
			continue
		}
		sub := strings.TrimPrefix(strings.TrimPrefix(r, root.Path), string(filepath.Separator))
		if sub == "" {
			continue
		}
		insert(root, strings.Split(sub, string(filepath.Separator)), r)
	}
	var countUp func(n *treeNode) int
	countUp = func(n *treeNode) int {
		c := 0
		for _, ch := range n.Children {
			if ch.IsEntry {
				c++
			} else {
				c += countUp(ch)
			}
		}
		n.Count = c
		return c
	}
	countUp(root)
	var nDirs, nEntries int
	var sortNode func(n *treeNode)
	sortNode = func(n *treeNode) {
		sort.Slice(n.Children, func(a, b int) bool { return n.Children[a].Name < n.Children[b].Name })
		for _, ch := range n.Children {
			if ch.IsEntry {
				nEntries++
			} else {
				nDirs++
				sortNode(ch)
			}
		}
	}
	sortNode(root)
	if asJSON {
		b, _ := json.MarshalIndent(root, "", "  ")
		fmt.Println(string(b))
		return 0
	}
	var maxDepth int
	if depth > 0 {
		maxDepth = depth
	}
	color := useColor()
	dirColor, entryColor, reset := "", "", ""
	if color {
		dirColor, entryColor, reset = "\033[36m", "\033[2m", "\033[0m"
	}
	var walk func(n *treeNode, level int, prefix string)
	walk = func(n *treeNode, level int, prefix string) {
		if maxDepth > 0 && level > maxDepth {
			return
		}
		for _, ch := range n.Children {
			if ch.IsEntry && !showAll {
				continue
			}
			if ch.IsEntry {
				name := strings.TrimSuffix(ch.Name, E)
				fmt.Printf("%s%s%s%s\n", prefix, entryColor, name, reset)
				continue
			}
			label := ch.Name + "/"
			if ch.Count > 0 {
				fmt.Printf("%s%s%s%s  (%d)\n", prefix, dirColor, label, reset, ch.Count)
			} else {
				fmt.Printf("%s%s%s%s\n", prefix, dirColor, label, reset)
			}
			walk(ch, level+1, prefix+"  ")
		}
	}
	fmt.Printf("%s  (%d 条)\n", showLabel(root.Name), root.Count)
	walk(root, 1, "  ")
	fmt.Printf("\n目录 %d 个 / 条目 %d 条\n", nDirs, nEntries)
	return 0
}

func useColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	st, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

func showLabel(s string) string {
	if s == "" {
		return "/"
	}
	return s
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
		if part == "." || part == ".." || filepath.IsAbs(part) || strings.ContainsAny(part, "\\") {
			fmt.Printf("非法目录段（不许 . / .. / 绝对路径 / 反斜杠）：%s\n", part)
			return "", fmt.Errorf("bad category")
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
	tags := flagValue(args, "--tags")
	if tags == "" {
		tags = metaTags(m)
	}
	if cat == "" || title == "" || tags == "" {
		fmt.Println("缺元数据：用 --category/--title/--tags 给；front matter 由 kb 生成")
		return 1
	}
	text := makeFM(title, tags) + strings.TrimSpace(body) + "\n"
	blocking, hints := similarTags(tags, "")
	probs := append(checkProblems(parse2(text), text), blocking...)
	for _, h := range hints {
		fmt.Println("  " + h)
	}
	if len(probs) > 0 && !force {
		fmt.Println("写入被拦下 —— 需要你【两次确认】：")
		for _, x := range probs {
			fmt.Println("  - " + x)
		}
		fmt.Println("流程：先把这些连同条目原文给用户看 → 用户确认 → 再加 --force 重跑")
		fmt.Println("写入须知：kb help -w")
		return 2
	}
	d, err := resolveCategory(cat)
	if err != nil || d == "" {
		fmt.Println("目录无效，已中止（未写入任何文件）")
		return 1
	}
	slot := freeSlot(d)
	if slot == "" {
		fmt.Println("该层前缀已用尽（1296 个）")
		return 1
	}
	name := strings.TrimSpace(reBad.ReplaceAllString(title, "-"))
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(draft), D)
	}
	for _, ex := range walkEntries() {
		if filepath.Dir(ex) != d {
			continue
		}
		en := filepath.Base(ex)
		if rePFX.MatchString(en) {
			en = en[3:]
		}
		if strings.EqualFold(strings.TrimSuffix(en, E), name) {
			fmt.Printf("已存在同名条目：%s —— 拒绝添加（请改用 kb update <id>）\n", rel(ex))
			return 1
		}
	}
	dest := filepath.Join(d, slot+"-"+name+E)
	if err := os.WriteFile(dest, []byte(text), 0o644); err != nil {
		fmt.Println("写入失败：" + err.Error())
		return 1
	}
	fmt.Printf("收编 %s → id %s\n", rel(dest), mustID(rel(dest)))
	os.Remove(draft)
	fmt.Println("草稿已删除（需要时用 kb get " + mustID(rel(dest)) + " 重建）")
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
	cur := Meta{}
	var curBody string
	cur, curBody, _ = readEntry(p)
	body := curBody
	if draft != "" {
		raw, err := os.ReadFile(draft)
		if err != nil {
			fmt.Printf("草稿不存在：%s\n", draft)
			return 1
		}
		_, body = parseFM(string(raw))
	}
	title := flagValue(args, "--title")
	if title == "" {
		title = cur["title"]
	}
	// 只有显式传了 --tags 才替换；没传就保持原样
	tags := metaTags(cur)
	if hasFlag(args, "--tags") {
		tags = flagValue(args, "--tags")
	}
	text := makeFM(title, tags) + strings.TrimSpace(body) + "\n"
	blocking, hints := similarTags(tags, p)
	probs := append(checkProblems(parse2(text), text), blocking...)
	for _, h := range hints {
		fmt.Println("  " + h)
	}
	if len(probs) > 0 && !hasFlag(args, "--force") {
		fmt.Println("写入被拦下 —— 需要你【两次确认】：")
		for _, x := range probs {
			fmt.Println("  - " + x)
		}
		fmt.Println("写入须知：kb help -w")
		return 2
	}
	// title ⇄ 文件名校验：前缀之后那段必须等于 title；不一致就改名（目标已存在则拒绝）
	base := reBad.ReplaceAllString(strings.TrimSpace(title), "-")
	if base == "" {
		fmt.Println("title 不能为空")
		return 1
	}
	pfx := ""
	if i := strings.Index(filepath.Base(p), "-"); rePFX.MatchString(filepath.Base(p)) {
		pfx = filepath.Base(p)[:i+1]
	}
	want := filepath.Join(filepath.Dir(p), pfx+base+E)
	if want != p {
		if _, err := os.Stat(want); err == nil {
			fmt.Printf("已存在同名条目：%s —— 拒绝改名（请先改标题或删掉那条）\n", rel(want))
			return 1
		}
	}
	oldRel := rel(p)
	dbg("update: id=%s to=%q p=%s", id, to, p)
	if to != "" {
		tgt := idToPath(to)
		if tgt == "" {
			// 也允许直接给目录路径（相对知识库根）
			cand := filepath.Join(kbDir, strings.TrimPrefix(strings.TrimSuffix(to, "/"), "./"))
			if st, err := os.Stat(cand); err == nil && st.IsDir() {
				tgt = cand
			} else {
				fmt.Printf("目标目录不存在：%s（可以用目录 id 或目录路径）\n", to)
				return 1
			}
		}
		if st, err := os.Stat(tgt); err != nil || !st.IsDir() {
			fmt.Printf("目标必须是目录 id（不能是条目）：%s\n", to)
			return 1
		}
		base := filepath.Base(p)
		name := base
		slot := freeSlot(tgt)
		if rePFX.MatchString(base) {
			name = base[3:]
			_ = base[:2] // 跨目录时槽一定会重新分配（id 随之变化，这是设计）
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
		// 移动后必须重算"按 title 应有的路径"，否则下面的改名会把条目搬回旧目录
		base2 := reBad.ReplaceAllString(strings.TrimSpace(title), "-")
		pfx2 := ""
		if i := strings.Index(filepath.Base(p), "-"); rePFX.MatchString(filepath.Base(p)) {
			pfx2 = filepath.Base(p)[:i+1]
		}
		want = filepath.Join(filepath.Dir(p), pfx2+base2+E)
		if want != p {
			if _, err := os.Stat(want); err == nil {
				fmt.Printf("已存在同名条目：%s —— 拒绝改名\n", rel(want))
				return 1
			}
		}
	}
	if want != p {
		oldRel = rel(p)
		if _, code := git("mv", rel(p), rel(want)); code != 0 {
			os.Rename(p, want)
		}
		p = want
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		fmt.Println("  !! 写文件失败：" + err.Error())
		return 1
	}
	if rel(p) != oldRel {
		dropRow(oldRel)
	}
	fmt.Printf("更新 %s（id %s）\n", rel(p), mustID(rel(p)))
	if draft != "" {
		os.Remove(draft)
		fmt.Println("草稿已删除（需要时用 kb get " + mustID(rel(p)) + " 重建）")
	}
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
	if st, err := os.Stat(tgt); err != nil || !st.IsDir() {
		fmt.Printf("目标必须是目录 id（不能是条目）：%s\n", target)
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
	rest := []string{}
	if len(args) > 1 {
		rest = args[1:]
	}
	// 自愈：数据库为空但磁盘有条目（首次切换或表结构迁移后）→ 自动重建
	if cmd != "index" {
		db := openDB()
		var n int
		db.QueryRow("SELECT COUNT(*) FROM entries").Scan(&n)
		db.Close()
		if n == 0 && len(walkEntries()) > 0 {
			rebuild()
		}
	}
	switch cmd {
	case "version", "-v", "--version":
		fmt.Println("kb " + buildVersion)
	case "help", "-h", "--help":
		if hasFlag(rest, "-w") || hasFlag(rest, "--write") {
			os.Exit(doHelpWrite())
		}
		if len(rest) > 0 {
			hit := false
			for _, l := range strings.Split(usageTx, "\n") {
				if strings.Contains(l, rest[0]) {
					fmt.Println(l)
					hit = true
				}
			}
			if !hit {
				fmt.Println("没有这个命令；不带参数看全部用法：kb help")
			}
			return
		}
		fmt.Println(usageTx)
	case "config":
		os.Exit(doConfig(rest))
	case "id":
		os.Exit(doID(positional(rest, 0)))
	case "get":
		os.Exit(doGet(positional(rest, 0)))
	case "ls":
		os.Exit(doLS(positional(rest, 0)))
	case "tree":
		depth := 3
		if v := flagValue(args, "--depth"); v != "" {
			fmt.Sscanf(v, "%d", &depth)
		}
		pos := []string{}
		for i := 0; i < len(rest); i++ {
			x := rest[i]
			if strings.HasPrefix(x, "-") {
				if x == "--depth" {
					i++ // 跳过后面的数值，别当成 id
				}
				continue
			}
			pos = append(pos, x)
		}
		id := ""
		if len(pos) > 0 {
			id = pos[0]
		}
		os.Exit(doTree(id, depth, hasFlag(args, "-a") || hasFlag(args, "--all"), hasFlag(args, "--json")))
	case "add":
		os.Exit(doAdd(positional(rest, 0), hasFlag(args, "--force"), args))
	case "update":
		to := flagValue(args, "--to")
		pos := []string{}
		for i := 0; i < len(rest); i++ {
			if strings.HasPrefix(rest[i], "--") {
				if rest[i] == "--to" || rest[i] == "--title" || rest[i] == "--tags" {
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
		os.Exit(doMove(positional(rest, 0), positional(rest, 1)))
	case "delete":
		os.Exit(doDelete(positional(rest, 0), hasFlag(args, "--force") || hasFlag(args, "--yes")))
	case "index":
		rebuild()
	case "lint":
		os.Exit(doLint())
	case "search":
		limit := 5
		if v := flagValue(rest, "-n"); v != "" {
			if x, err := strconv.Atoi(v); err == nil && x > 0 {
				limit = x
			}
		} else if v := flagValue(rest, "--limit"); v != "" {
			if x, err := strconv.Atoi(v); err == nil && x > 0 {
				limit = x
			}
		}
		os.Exit(doSearch(positional(rest, 0), limit, hasFlag(args, "--json")))
	case "push":
		os.Exit(push(false))
	case "sync":
		os.Exit(syncCmd())
	default:
		fmt.Println(usageTx)
		os.Exit(2)
	}
}

func firstPositional(a []string) string {
	for i := 0; i < len(a); i++ {
		if strings.HasPrefix(a[i], "--") {
			if a[i] == "--depth" || a[i] == "--to" || a[i] == "--title" || a[i] == "--tags" || a[i] == "--category" {
				i++
			}
			continue
		}
		return a[i]
	}
	return ""
}

func arg(a []string, i int) string {
	if i < len(a) {
		return a[i]
	}
	return ""
}
