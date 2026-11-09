package store

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	// Base36 是 id 前缀用的字母表（两位 = 1296 个槽）
	Base36 = "0123456789abcdefghijklmnopqrstuvwxyz"

	Ext      = ".kbx"        // 收编产物
	DraftExt = ".md"         // 草稿
	TPL      = "TEMPLATE.md" // 模板文件，不算条目
)

// 格式约定：front matter、id 前缀、文件名前缀、非法字符
var (
	ReFM  = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n`)
	RePFX = regexp.MustCompile(`^[0-9a-z]{2}-`)
	ReID2 = regexp.MustCompile(`^[0-9a-z]{2}`)
	ReBad = regexp.MustCompile(`[\\/\x00-\x1f]`)
)

// 运行时根目录与索引库路径（由 main 初始化；KB_ROOT / KB_DB 可覆盖）
var (
	Root   string
	DBPath string
)

func Rel(p string) string {
	r, _ := filepath.Rel(Root, p)
	return r
}

func IdentOf(r string) (string, bool) {
	parts := strings.Split(filepath.ToSlash(r), "/")
	out := []string{}
	for _, s := range parts {
		if !RePFX.MatchString(s) {
			return "", false
		}
		out = append(out, s[:2])
	}
	return strings.Join(out, ""), true
}

func WalkEntries() []string {
	out := []string{}
	filepath.WalkDir(Root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), Ext) && d.Name() != TPL {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func WalkDrafts() []string {
	out := []string{}
	filepath.WalkDir(Root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), DraftExt) && d.Name() != TPL {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func IDToPath(id string) string {
	res := ""
	filepath.WalkDir(Root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			if v, ok := IdentOf(Rel(p)); ok && v == id {
				res = p
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), Ext) && d.Name() != TPL {
			if v, ok := IdentOf(Rel(p)); ok && v == id {
				res = p
			}
		}
		return nil
	})
	return res
}

// SlotTaken 判断目录里是否已有条目占用某个槽前缀（id 由每段路径前缀组成，槽不能重复）
func SlotTaken(dir, slot string) bool {
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

func FreeSlot(dir string) string {
	used := map[string]bool{}
	des, _ := os.ReadDir(dir)
	for _, d := range des {
		if d.Name() == TPL {
			continue
		}
		if m := ReID2.FindString(d.Name()); m != "" {
			used[m] = true
		}
	}
	for n := 0; n < 36*36; n++ {
		s := string(Base36[n/36]) + string(Base36[n%36])
		if !used[s] {
			return s
		}
	}
	return ""
}

func MustID(r string) string { v, _ := IdentOf(r); return v }

func ResolveCategory(cat string) (string, error) {
	cur := Root
	for _, part := range strings.Split(cat, "/") {
		if part == "" {
			continue
		}
		if part == "." || part == ".." || filepath.IsAbs(part) || strings.ContainsAny(part, "\\") {
			fmt.Printf("非法目录段（不许 . / .. / 绝对路径 / 反斜杠）：%s\n", part)
			return "", fmt.Errorf("bad category")
		}
		// 匹配：剥掉前缀再比，所以 `99-测试` 和 `03-99-测试` 都能找到同一个目录。
		// 前缀只在"查找"时剥 —— 新建时用户给的整段是名字的一部分，
		// 例如 `--category 99-重构测试` 应该建出 `03-99-重构测试`，而不是 `03-重构测试`。
		norm := func(s string) string {
			if RePFX.MatchString(s) {
				return s[3:]
			}
			return s
		}
		match := norm(part)
		found := ""
		if des, err := os.ReadDir(cur); err == nil {
			for _, d := range des {
				if !d.IsDir() {
					continue
				}
				// 两边都剥一层前缀再比：`99-测试`、`03-99-测试`、`测试` 指向同一个目录
				if d.Name() == part || (RePFX.MatchString(d.Name()) && norm(d.Name()[3:]) == match) {
					found = filepath.Join(cur, d.Name())
					break
				}
			}
		}
		if found == "" {
			os.MkdirAll(cur, 0o755)
			slot := FreeSlot(cur)
			nd := filepath.Join(cur, slot+"-"+part)
			os.MkdirAll(nd, 0o755)
			fmt.Printf("新建目录 %s\n", Rel(nd))
			found = nd
		}
		cur = found
	}
	return cur, nil
}

func Dbg(format string, a ...any) {
	if os.Getenv("KB_DEBUG") == "1" {
		fmt.Fprintf(os.Stderr, "[kb] "+format+"\n", a...)
	}
}
