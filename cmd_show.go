package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

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
