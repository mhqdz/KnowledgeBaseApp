package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"kbapp/core/config"
	"kbapp/core/gitx"
	"kbapp/core/index"
	"kbapp/core/lint"
	"kbapp/core/search"
	"kbapp/core/store"
)

// Usage 是 `kb help` 打印的用法；新增子命令时同步改这里。
var Usage = `kb — 本地知识库

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

// Die 打印一行并退出（只用于"连路径都定不下来"这类没法继续的情况）。
func Die(f string, a ...any) {
	fmt.Printf(f+"\n", a...)
	os.Exit(1)
}

// Run 是全部子命令的分发入口：main 只负责把 os.Args 交进来。
// 返回值就是退出码（方便测试；真正退出由 main 做）。
func Run(args []string, version string) int {
	store.Root = os.Getenv("KB_ROOT")
	if store.Root == "" {
		home, _ := os.UserHomeDir()
		store.Root = filepath.Join(home, ".data", ".kb")
	}
	store.DBPath = os.Getenv("KB_DB")
	if store.DBPath == "" {
		home, _ := os.UserHomeDir()
		store.DBPath = filepath.Join(home, ".cache", "kb.sqlite")
	}
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
		db := index.Open()
		var n int
		db.QueryRow("SELECT COUNT(*) FROM entries").Scan(&n)
		db.Close()
		if n == 0 && len(store.WalkEntries()) > 0 {
			index.Rebuild()
		}
	}
	switch cmd {
	case "version", "-v", "--version":
		fmt.Println("kb " + version)
		return 0
	case "help", "-h", "--help":
		if Has(rest, "-w") || Has(rest, "--write") {
			return config.HelpWrite()
		}
		if len(rest) > 0 {
			hit := false
			for _, l := range strings.Split(Usage, "\n") {
				if strings.Contains(l, rest[0]) {
					fmt.Println(l)
					hit = true
				}
			}
			if !hit {
				fmt.Println("没有这个命令；不带参数看全部用法：kb help")
			}
			return 0
		}
		fmt.Println(Usage)
		return 0
	case "config":
		return config.RunConfig(rest)
	case "id":
		return RunID(Positional(rest, 0))
	case "get":
		return RunGet(Positional(rest, 0))
	case "ls":
		return RunLS(Positional(rest, 0))
	case "tree":
		depth := 3
		if v := Value(args, "--depth"); v != "" {
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
		return RunTree(id, depth, Has(args, "-a") || Has(args, "--all"), Has(args, "--json"))
	case "add":
		return RunAdd(Positional(rest, 0), Has(args, "--force"), args)
	case "update":
		to := Value(args, "--to")
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
		return RunUpdate(id, draft, to, args)
	case "move":
		return RunMove(Positional(rest, 0), Positional(rest, 1))
	case "delete":
		return RunDelete(Positional(rest, 0), Has(args, "--force") || Has(args, "--yes"))
	case "index":
		index.Rebuild()
		return 0
	case "lint":
		return lint.RunLint()
	case "search":
		limit := 5
		if v := Value(rest, "-n"); v != "" {
			if x, err := strconv.Atoi(v); err == nil && x > 0 {
				limit = x
			}
		} else if v := Value(rest, "--limit"); v != "" {
			if x, err := strconv.Atoi(v); err == nil && x > 0 {
				limit = x
			}
		}
		return search.Run(Positional(rest, 0), limit, Has(args, "--json"))
	case "push":
		return gitx.Push(false)
	case "sync":
		return gitx.Sync()
	default:
		fmt.Println(Usage)
		return 2
	}
}
