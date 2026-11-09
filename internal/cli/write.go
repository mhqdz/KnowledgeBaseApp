package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"kbapp/core/entry"
	"kbapp/core/gitx"
	"kbapp/core/index"
	"kbapp/core/lint"
	"kbapp/core/store"
	"kbapp/core/tag"
)

func RunAdd(draft string, force bool, args []string) int {
	raw, err := os.ReadFile(draft)
	if err != nil {
		fmt.Printf("草稿不存在：%s\n", draft)
		return 1
	}
	m, body := entry.ParseFM(string(raw))
	cat := Value(args, "--category")
	if cat == "" {
		cat = m["category"]
	}
	title := Value(args, "--title")
	if title == "" {
		title = m["title"]
	}
	tags := Value(args, "--tags")
	if tags == "" {
		tags = entry.Tags(m)
	}
	if cat == "" || title == "" || tags == "" {
		fmt.Println("缺元数据：用 --category/--title/--tags 给；front matter 由 kb 生成")
		return 1
	}
	text := entry.MakeFM(title, tags) + strings.TrimSpace(body) + "\n"
	blocking, hints := tag.Similar(tags, "")
	probs := append(lint.Problems(ParseText(text), text), blocking...)
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
	d, err := store.ResolveCategory(cat)
	if err != nil || d == "" {
		fmt.Println("目录无效，已中止（未写入任何文件）")
		return 1
	}
	slot := store.FreeSlot(d)
	if slot == "" {
		fmt.Println("该层前缀已用尽（1296 个）")
		return 1
	}
	name := strings.TrimSpace(store.ReBad.ReplaceAllString(title, "-"))
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(draft), store.DraftExt)
	}
	for _, ex := range store.WalkEntries() {
		if filepath.Dir(ex) != d {
			continue
		}
		en := filepath.Base(ex)
		if store.RePFX.MatchString(en) {
			en = en[3:]
		}
		if strings.EqualFold(strings.TrimSuffix(en, store.Ext), name) {
			fmt.Printf("已存在同名条目：%s —— 拒绝添加（请改用 kb update <id>）\n", store.Rel(ex))
			return 1
		}
	}
	dest := filepath.Join(d, slot+"-"+name+store.Ext)
	if err := os.WriteFile(dest, []byte(text), 0o644); err != nil {
		fmt.Println("写入失败：" + err.Error())
		return 1
	}
	fmt.Printf("收编 %s → id %s\n", store.Rel(dest), store.MustID(store.Rel(dest)))
	os.Remove(draft)
	fmt.Println("草稿已删除（需要时用 kb get " + store.MustID(store.Rel(dest)) + " 重建）")
	index.Upsert(dest)
	gitx.Commit("kb: add " + title)
	gitx.Push(false)
	return 0
}

func ParseText(text string) entry.Meta { m, _ := entry.ParseFM(text); return m }

func RunUpdate(id, draft, to string, args []string) int {
	p := store.IDToPath(id)
	if p == "" {
		fmt.Printf("没有这个 id：%s\n", id)
		return 1
	}
	cur := entry.Meta{}
	var curBody string
	cur, curBody, _ = entry.Read(p)
	body := curBody
	if draft != "" {
		raw, err := os.ReadFile(draft)
		if err != nil {
			fmt.Printf("草稿不存在：%s\n", draft)
			return 1
		}
		_, body = entry.ParseFM(string(raw))
	}
	title := Value(args, "--title")
	if title == "" {
		title = cur["title"]
	}
	// 只有显式传了 --tags 才替换；没传就保持原样
	tags := entry.Tags(cur)
	if Has(args, "--tags") {
		tags = Value(args, "--tags")
	}
	text := entry.MakeFM(title, tags) + strings.TrimSpace(body) + "\n"
	blocking, hints := tag.Similar(tags, p)
	probs := append(lint.Problems(ParseText(text), text), blocking...)
	for _, h := range hints {
		fmt.Println("  " + h)
	}
	if len(probs) > 0 && !Has(args, "--force") {
		fmt.Println("写入被拦下 —— 需要你【两次确认】：")
		for _, x := range probs {
			fmt.Println("  - " + x)
		}
		fmt.Println("写入须知：kb help -w")
		return 2
	}
	// title ⇄ 文件名校验：前缀之后那段必须等于 title；不一致就改名（目标已存在则拒绝）
	base := store.ReBad.ReplaceAllString(strings.TrimSpace(title), "-")
	if base == "" {
		fmt.Println("title 不能为空")
		return 1
	}
	pfx := ""
	if i := strings.Index(filepath.Base(p), "-"); store.RePFX.MatchString(filepath.Base(p)) {
		pfx = filepath.Base(p)[:i+1]
	}
	want := filepath.Join(filepath.Dir(p), pfx+base+store.Ext)
	if want != p {
		if _, err := os.Stat(want); err == nil {
			fmt.Printf("已存在同名条目：%s —— 拒绝改名（请先改标题或删掉那条）\n", store.Rel(want))
			return 1
		}
	}
	oldRel := store.Rel(p)
	store.Dbg("update: id=%s to=%q p=%s", id, to, p)
	if to != "" {
		tgt := store.IDToPath(to)
		if tgt == "" {
			// 也允许直接给目录路径（相对知识库根）
			cand := filepath.Join(store.Root, strings.TrimPrefix(strings.TrimSuffix(to, "/"), "./"))
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
		slot := store.FreeSlot(tgt)
		if store.RePFX.MatchString(base) {
			name = base[3:]
			_ = base[:2] // 跨目录时槽一定会重新分配（id 随之变化，这是设计）
		}
		np := filepath.Join(tgt, slot+"-"+name)
		if _, err := os.Stat(np); err == nil {
			fmt.Printf("目标已存在：%s\n", store.Rel(np))
			return 1
		}
		if _, code := gitx.Git("mv", oldRel, store.Rel(np)); code != 0 {
			os.Rename(p, np)
		}
		p = np
		// 移动后必须重算"按 title 应有的路径"，否则下面的改名会把条目搬回旧目录
		base2 := store.ReBad.ReplaceAllString(strings.TrimSpace(title), "-")
		pfx2 := ""
		if i := strings.Index(filepath.Base(p), "-"); store.RePFX.MatchString(filepath.Base(p)) {
			pfx2 = filepath.Base(p)[:i+1]
		}
		want = filepath.Join(filepath.Dir(p), pfx2+base2+store.Ext)
		if want != p {
			if _, err := os.Stat(want); err == nil {
				fmt.Printf("已存在同名条目：%s —— 拒绝改名\n", store.Rel(want))
				return 1
			}
		}
	}
	if want != p {
		oldRel = store.Rel(p)
		if _, code := gitx.Git("mv", store.Rel(p), store.Rel(want)); code != 0 {
			os.Rename(p, want)
		}
		p = want
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		fmt.Println("  !! 写文件失败：" + err.Error())
		return 1
	}
	if store.Rel(p) != oldRel {
		index.DropRow(oldRel)
	}
	fmt.Printf("更新 %s（id %s）\n", store.Rel(p), store.MustID(store.Rel(p)))
	if draft != "" {
		os.Remove(draft)
		fmt.Println("草稿已删除（需要时用 kb get " + store.MustID(store.Rel(p)) + " 重建）")
	}
	index.Upsert(p)
	gitx.Commit("kb: update " + title)
	gitx.Push(false)
	return 0
}

func RunMove(id, target string) int {
	p := store.IDToPath(id)
	if p == "" {
		fmt.Printf("没有这个 id：%s\n", id)
		return 1
	}
	tgt := store.IDToPath(target)
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
	slot := store.FreeSlot(tgt)
	base := filepath.Base(p)
	name := base
	if store.RePFX.MatchString(base) {
		name = base[3:]
	}
	np := filepath.Join(tgt, slot+"-"+name)
	if _, err := os.Stat(np); err == nil {
		fmt.Printf("目标已存在：%s\n", store.Rel(np))
		return 1
	}
	if _, code := gitx.Git("mv", store.Rel(p), store.Rel(np)); code != 0 {
		os.Rename(p, np)
	}
	fmt.Printf("移动 %s → %s\n", store.Rel(p), store.Rel(np))
	index.Rebuild()
	gitx.Commit("kb: move " + store.Rel(np))
	gitx.Push(false)
	return 0
}

func RunDelete(id string, force bool) int {
	p := store.IDToPath(id)
	if p == "" {
		fmt.Printf("没有这个 id：%s\n", id)
		return 1
	}
	if !force {
		fmt.Printf("删除是破坏性操作：确认后加 --force 重跑（%s）\n", store.Rel(p))
		return 1
	}
	if st, err := os.Stat(p); err == nil && st.IsDir() {
		des, _ := os.ReadDir(p)
		if len(des) > 0 {
			fmt.Printf("目录非空，先删里面：%s\n", store.Rel(p))
			return 1
		}
		os.Remove(p)
	} else {
		if _, code := gitx.Git("rm", "-q", store.Rel(p)); code != 0 {
			os.Remove(p)
		}
		index.DropRow(store.Rel(p))
		// 删完条目的目录常常就空了，但 kb 不会替你删目录（前缀是 id 的一部分，
		// 误删会连带改掉别人的 id）。提示一下，想删就显式删目录。
		if dir := filepath.Dir(p); dir != store.Root {
			if des, err := os.ReadDir(dir); err == nil && len(des) == 0 {
				fmt.Printf("目录已空：%s（要删目录用 kb delete <目录id> --force）\n", store.Rel(dir))
			}
		}
	}
	fmt.Printf("已删除 %s（别人 id 不变）\n", store.Rel(p))
	gitx.Commit("kb: delete " + store.Rel(p))
	gitx.Push(false)
	return 0
}
