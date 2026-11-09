// kb — 本地知识库工具（Go 版）。.kbx = 收编产物，.md = 草稿。
// id = 路径每段前 2 位（36 进制）拼接；front matter 只由本工具生成：title / tags / updated。
//
// 文件划分（每个文件一个职责）：
//
//	store.go     知识库根目录、id ⇄ 路径、目录遍历与槽位分配
//	entry.go     .kbx 的格式：front matter、正文、摘要
//	index.go     SQLite 派生索引（entries / tags）
//	tags.go      tag 规范检查（近似/包含）
//	search.go    检索评分
//	lint.go      只读体检
//	git.go       commit / push / sync
//	config.go    ~/.config/kb/config.json 与写入须知
//	args.go      位置参数与 flag 解析
//	cmd_show.go  id / get / ls / tree
//	cmd_write.go add / update / move / delete
//	main.go      仅入口：初始化 + 分发
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var buildVersion = "dev"

var usageTx = `kb — 本地知识库

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

func die(f string, a ...any) { fmt.Printf(f+"\n", a...); os.Exit(1) }

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
