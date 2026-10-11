# kb — 本地知识库工具（Go）

## 功能
- 一条目 = 一个 `.kbx` 文件；目录 = 类目；id = 文件/目录名前 2 位（36 进制），完整 id = 路径各段前缀拼接
- 检索：`kb search <关键词>`（tags 优先，其次标题、摘要、正文）
- 读写：`kb id / get / ls / tree / add / update / move / delete`
- 维护：`kb index`（重建 SQLite 派生缓存）、`kb lint`（只读体检）、`kb push / sync`
- front matter 由本工具生成，字段只有 **title / tags / updated**（category 由路径表达，不重复存）

## 编译
```bash
export PATH=$PATH:/usr/local/go/bin
cd ~/Workspace/KnowledgeBase
go build -o ~/.local/bin/kb .        # 依赖 modernc.org/sqlite（纯 Go，无 cgo）
# 拉不到依赖时：go env -w GOPROXY=https://goproxy.cn,direct
```

## 使用方法（简）
```bash
kb search 关键词        # 查（关键词，不要用整句）
kb get <id>             # 取条目全文（id 形如 010202）
kb tree                 # 看结构
kb add 草稿.md --category 00-本机 --title "标题" --tags "词1,词2"   # 新增：元数据走参数
kb update <id> 草稿.md  # 修改；搬家：kb move <id> <目标目录id>
kb lint                 # 体检（只读）；kb index 重建缓存
```

详细规则见知识库：`kb get 010202`（写入约定）、`kb get 010201`（设计逻辑）、`kb get 010203`（模板）
