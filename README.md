# kb — 本地知识库工具（Go）

## 功能
- 一条目 = 一个 `.kbx` 文件；目录 = 类目；id = 文件/目录名前 2 位（36 进制），完整 id = 路径各段前缀拼接
- 检索：`kb search <关键词>`（tags 优先，其次标题、摘要、正文）
- 读写：`kb id / get / ls / tree / add / update / move / delete`
- 维护：`kb index`（重建 SQLite 派生缓存）、`kb lint`（只读体检）、`kb push / sync`
- front matter 由本工具生成，字段只有 **title / tags / updated**（category 由路径表达，不重复存）

> `kb.py.legacy` 是移植前的 Python 实现，仅作行为对照，不要运行或修改。

## 编译
```bash
export PATH=$PATH:/usr/local/go/bin
cd ~/Workspace/KnowledgeBase
COMMIT=$(git rev-parse --short HEAD)
go build -ldflags "-X main.buildVersion=$COMMIT" -o ~/.local/bin/kb ./cmd/kb   # core/ 是独立 module（kbapp/core）   # 依赖 modernc.org/sqlite（纯 Go，无 cgo）
kb version                                                             # 应打印出这个 commit
# 拉不到依赖时：go env -w GOPROXY=https://goproxy.cn,direct
```

## 代码结构

`main` 只做入口，逻辑在 `core/`（**独立 module `kbapp/core`**，agentd 的记忆层直接 import 它，
所以格式约定只有一份）：

```
kbapp          ← 根 module：cmd/kb + internal/cli（唯一的输出层）
kbapp/core     ← 独立 module：store / entry / index / tag / search / lint / gitx / config
                 根 module 用 replace 指向 ./core；其它仓库 require klbapp/core 即可
```

```
cmd/kb/main.go            入口：注入版本号，把 os.Args 交给 cli.Run
internal/cli/         子命令分发 + 参数解析 + 各命令实现（唯一的输出层）
  cli.go                    Run()：初始化路径、自愈、switch 分发、usage
  args.go                   positional / flagValue / hasFlag（flag 任意位置）
  show.go                   id / get / ls / tree
  write.go                  add / update / move / delete
core/store/           根目录、id ⇄ 路径、遍历、槽位、类目解析（最底层，无内部依赖）
core/entry/           .kbx 格式：front matter、正文、摘要
core/index/           SQLite 派生索引（entries / tags）
core/tag/             tag 规范检查（近似/包含）
core/search/          检索分词、中文 2-gram、评分
core/lint/            只读体检（文件 + DB 一致性）
core/gitx/            commit / push / sync
core/config/          ~/.config/kb/config.json 与写入须知
```

依赖方向单向：`cli → {search,lint,index,tag,config,gitx,store} → {entry,store}`，
没有循环。全局状态只有 `store.Root` / `store.DBPath` 两个变量（由 `cli.Run` 初始化）。

## 使用方法（简）
```bash
kb search 关键词        # 查（关键词，不要用整句）
kb get <id>             # 取条目全文（id 形如 010202）
kb tree                 # 看结构
kb add 草稿.md --category 00-本机 --title "标题" --tags "词1,词2"   # 新增：元数据走参数
kb update <id> 草稿.md  # 修改；搬家：kb move <id> <目标目录id>
kb lint                 # 体检（只读）；kb index 重建缓存
kb version              # 看装的是哪一版（构建时嵌入的 commit）
# 注：收编成功后草稿会被自动删除（需要时用 kb get <id> 取回全文重建）
```

详细规则见知识库：`kb get 010202`（写入约定）、`kb get 010201`（设计逻辑）、`kb get 010203`（模板）
