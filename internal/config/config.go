package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"kbapp/internal/store"
)

// ---------- 配置（~/.config/kb/config.json）----------

func Path() string {
	if v := os.Getenv("KB_CONFIG"); v != "" {
		return v
	}
	return filepath.Join(os.Getenv("HOME"), ".config", "kb", "config.json")
}

func Load() map[string]string {
	m := map[string]string{}
	b, err := os.ReadFile(Path())
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

func Save(m map[string]string) error {
	p := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(p, append(b, '\n'), 0o644)
}

// RunConfig：kb config（打印）/ kb config set <key> <value> / kb config unset <key>
func RunConfig(args []string) int {
	pos := []string{}
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			pos = append(pos, a)
		}
	}
	m := Load()
	if len(pos) == 0 {
		fmt.Println("# " + Path())
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
		if err := Save(m); err != nil {
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
		if err := Save(m); err != nil {
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

// HelpWrite：打印写入须知（由配置 write_guide 指向的条目）
func HelpWrite() int {
	m := Load()
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
	p := store.IDToPath(id)
	if p == "" {
		fmt.Printf("配置的写入须知条目不存在：%s（用 kb config set write_guide <id> 修正）\n", id)
		return 1
	}
	b, err := os.ReadFile(p)
	if err != nil {
		fmt.Println("读取失败：" + err.Error())
		return 1
	}
	fmt.Printf("# 写入须知（%s  %s）\n\n%s", store.MustID(store.Rel(p)), store.Rel(p), string(b))
	return 0
}
