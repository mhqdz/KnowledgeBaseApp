package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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
