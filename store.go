package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const b36 = "0123456789abcdefghijklmnopqrstuvwxyz"

// 运行时根目录与索引库路径（由 main 初始化；KB_ROOT / KB_DB 可覆盖）
var (
	kbDir  string
	dbPath string
)

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

func mustID(r string) string { v, _ := identOf(r); return v }

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

func dbg(format string, a ...any) {
	if os.Getenv("KB_DEBUG") == "1" {
		fmt.Fprintf(os.Stderr, "[kb] "+format+"\n", a...)
	}
}
