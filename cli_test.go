package main

// 端到端 CLI 测试：编译出真二进制，在临时知识库里跑命令，断言输出与文件结构。
import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var kbBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "kbbin")
	if err != nil {
		panic(err)
	}
	kbBin = filepath.Join(dir, "kb")
	if out, err := exec.Command("go", "build", "-o", kbBin, ".").CombinedOutput(); err != nil {
		panic("build failed: " + string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type env struct {
	root string
	t    *testing.T
}

func newEnv(t *testing.T) *env {
	t.Helper()
	root := t.TempDir()
	// 初始化 git 仓库，让 kb 的 commit/push 走正常路径（没有 remote，push 失败是预期的）
	exec.Command("git", "init", "-q", root).Run()
	exec.Command("git", "-C", root, "config", "user.email", "t@t").Run()
	exec.Command("git", "-C", root, "config", "user.name", "t").Run()
	return &env{root: root, t: t}
}

func (e *env) run(args ...string) (string, int) {
	e.t.Helper()
	cmd := exec.Command(kbBin, args...)
	cmd.Env = append(os.Environ(),
		"KB_ROOT="+e.root,
		"KB_DB="+filepath.Join(e.root, "..", "kb-test.sqlite"),
		"HOME="+e.root)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	}
	return string(out), code
}

func (e *env) write(name, body string) string {
	e.t.Helper()
	p := filepath.Join(e.root, name)
	os.WriteFile(p, []byte(body), 0o644)
	return p
}

func (e *env) list() string {
	out, _ := e.run("ls", "00")
	return out
}

const bodyA = "## 概述\n甲条目正文，关键词 阿尔法。\n"
const bodyB = "## 概述\n乙条目正文，关键词 贝塔。\n"

// ---------- 基础命令 ----------

func TestAddAndGet(t *testing.T) {
	e := newEnv(t)
	f := e.write("a.md", bodyA)
	out, code := e.run("add", f, "--category", "00-甲", "--title", "甲条目", "--tags", "测试,阿尔法")
	if code != 0 {
		t.Fatalf("add 失败(%d): %s", code, out)
	}
	if !strings.Contains(out, "0000") {
		t.Errorf("add 未分配 id 0000: %s", out)
	}
	out, code = e.run("get", "0000")
	if code != 0 || !strings.Contains(out, "甲条目正文") {
		t.Errorf("get 0000 异常(%d): %s", code, out)
	}
}

func TestAddDuplicateNameRejected(t *testing.T) {
	e := newEnv(t)
	e.run("add", e.write("a.md", bodyA), "--category", "00-甲", "--title", "同名", "--tags", "测试")
	f2 := e.write("b.md", bodyB)
	out, code := e.run("add", f2, "--category", "00-甲", "--title", "同名", "--tags", "测试")
	if code == 0 {
		t.Errorf("同名条目应当被拒绝，却成功了: %s", out)
	}
}

func TestUpdateTagsOnlyKeepsBody(t *testing.T) {
	e := newEnv(t)
	e.run("add", e.write("a.md", bodyA), "--category", "00-甲", "--title", "甲条目", "--tags", "测试,阿尔法")
	out, code := e.run("update", "0000", "--tags", "测试,改过") // 不传草稿
	if code != 0 {
		t.Fatalf("只改 tags 失败(%d): %s", code, out)
	}
	got, _ := e.run("get", "0000")
	if !strings.Contains(got, "tags: [测试,改过]") {
		t.Errorf("tags 未更新: %s", got)
	}
	if !strings.Contains(got, "甲条目正文") {
		t.Errorf("正文被破坏: %s", got)
	}
}

func TestUpdateTitleRenamesFile(t *testing.T) {
	e := newEnv(t)
	e.run("add", e.write("a.md", bodyA), "--category", "00-甲", "--title", "甲条目", "--tags", "测试")
	if _, code := e.run("update", "0000", "--title", "改名后"); code != 0 {
		t.Fatalf("改 title 失败")
	}
	if _, err := os.Stat(filepath.Join(e.root, "00-甲", "00-改名后.kbx")); err != nil {
		t.Errorf("文件名未同步: %v", err)
	}
	if out, _ := e.run("id", "00-甲/00-改名后.kbx"); !strings.Contains(out, "0000") {
		t.Errorf("改 title 后 id 应保持 0000，实得: %s", out)
	}
}

// flag 出现在位置参数之后（曾经的 bug：flagValue 遇到位置参数就停）
func TestFlagAfterPositional(t *testing.T) {
	e := newEnv(t)
	e.run("add", e.write("a.md", bodyA), "--category", "00-甲", "--title", "甲条目", "--tags", "测试")
	if out, code := e.run("update", "0000", "--tags", "位置参数之后"); code != 0 {
		t.Fatalf("flag 后置失败: %s", out)
	}
	if got, _ := e.run("get", "0000"); !strings.Contains(got, "位置参数之后") {
		t.Errorf("后置 flag 未生效: %s", got)
	}
}

func TestUpdateMoveByPath(t *testing.T) {
	e := newEnv(t)
	e.run("add", e.write("a.md", bodyA), "--category", "00-甲", "--title", "甲条目", "--tags", "测试")
	e.run("add", e.write("b.md", bodyB), "--category", "01-乙", "--title", "乙条目", "--tags", "测试")
	out, code := e.run("update", "0000", "--to", "01-乙")
	if code != 0 {
		t.Fatalf("移动失败(%d): %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(e.root, "01-乙")); err != nil {
		t.Fatalf("目标目录不存在")
	}
	matches, _ := filepath.Glob(filepath.Join(e.root, "01-乙", "*-甲条目.kbx"))
	if len(matches) == 0 {
		t.Errorf("条目没被移动到 01-乙；当前结构:\n%s", treeOf(e.root))
	}
}

func TestUpdateMoveById(t *testing.T) {
	e := newEnv(t)
	e.run("add", e.write("a.md", bodyA), "--category", "00-甲", "--title", "甲条目", "--tags", "测试")
	e.run("add", e.write("b.md", bodyB), "--category", "01-乙", "--title", "乙条目", "--tags", "测试")
	if out, code := e.run("update", "0000", "--to", "01"); code != 0 {
		t.Fatalf("按目录 id 移动失败(%d): %s", code, out)
	}
	matches, _ := filepath.Glob(filepath.Join(e.root, "01-乙", "*-甲条目.kbx"))
	if len(matches) == 0 {
		t.Errorf("按 id 移动没生效:\n%s", treeOf(e.root))
	}
}

func TestMoveCommand(t *testing.T) {
	e := newEnv(t)
	e.run("add", e.write("a.md", bodyA), "--category", "00-甲", "--title", "甲条目", "--tags", "测试")
	e.run("add", e.write("b.md", bodyB), "--category", "01-乙", "--title", "乙条目", "--tags", "测试")
	if out, code := e.run("move", "0000", "01"); code != 0 {
		t.Fatalf("move 失败: %s", out)
	}
	matches, _ := filepath.Glob(filepath.Join(e.root, "01-乙", "*-甲条目.kbx"))
	if len(matches) == 0 {
		t.Errorf("move 没生效:\n%s", treeOf(e.root))
	}
}

// ---------- 易错点 ----------

func TestTreeDepthFlag(t *testing.T) {
	e := newEnv(t)
	e.run("add", e.write("a.md", bodyA), "--category", "00-甲", "--title", "甲条目", "--tags", "测试")
	out, code := e.run("tree", "--depth", "2")
	if code != 0 || strings.Contains(out, "没有这个 id") {
		t.Errorf("tree --depth 解析错误(%d): %s", code, out)
	}
}

func TestSearchJSONShape(t *testing.T) {
	e := newEnv(t)
	e.run("add", e.write("a.md", bodyA), "--category", "00-甲", "--title", "甲条目", "--tags", "测试,阿尔法")
	out, _ := e.run("search", "阿尔法", "--json")
	if !strings.Contains(out, `"path"`) {
		t.Fatalf("search --json 输出异常: %s", out)
	}
	if !strings.Contains(out, "0000") {
		t.Errorf("search --json 应给出条目 id 0000，实得: %s", out)
	}
}

func TestSearchMultiTerm(t *testing.T) {
	e := newEnv(t)
	e.run("add", e.write("a.md", bodyA), "--category", "00-甲", "--title", "甲条目", "--tags", "测试,阿尔法")
	if out, _ := e.run("search", "阿尔法 甲"); !strings.Contains(out, "0000") {
		t.Errorf("多词搜索失败: %s", out)
	}
	if out, _ := e.run("search", "阿尔法不存在"); strings.Contains(out, "0000") {
		t.Errorf("不该命中: %s", out)
	}
}

func TestLintCatchesFilenameMismatch(t *testing.T) {
	e := newEnv(t)
	e.run("add", e.write("a.md", bodyA), "--category", "00-甲", "--title", "甲条目", "--tags", "测试")
	p := filepath.Join(e.root, "00-甲", "00-甲条目.kbx")
	os.Rename(p, filepath.Join(e.root, "00-甲", "00-不一致.kbx"))
	out, _ := e.run("lint")
	if !strings.Contains(out, "文件名与 title 不一致") {
		t.Errorf("lint 未检出文件名/title 不一致: %s", out)
	}
}

func TestDeleteLeavesHole(t *testing.T) {
	e := newEnv(t)
	e.run("add", e.write("a.md", bodyA), "--category", "00-甲", "--title", "甲条目", "--tags", "测试")
	e.run("add", e.write("b.md", bodyB), "--category", "00-甲", "--title", "乙条目", "--tags", "测试")
	if out, code := e.run("delete", "0000", "--force"); code != 0 {
		t.Fatalf("delete 失败: %s", out)
	}
	out, _ := e.run("add", e.write("c.md", "## 概述\n丙条目。\n"), "--category", "00-甲", "--title", "丙条目", "--tags", "测试")
	if !strings.Contains(out, "0000") {
		t.Errorf("删除后新条目应补上空洞 0000（稀疏槽不重排），实得: %s", out)
	}
}

func treeOf(root string) string {
	var b strings.Builder
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && p != root && !strings.Contains(p, "/.git") {
			rel, _ := filepath.Rel(root, p)
			b.WriteString("  " + rel + "\n")
		}
		return nil
	})
	return b.String()
}

// flag 放在位置参数之前（曾经的 bug：arg(rest,0) 会把 --category 当成草稿名）
func TestFlagsBeforePositional(t *testing.T) {
	e := newEnv(t)
	f := e.write("a.md", bodyA)
	out, code := e.run("add", "--category", "00-甲", "--title", "甲条目", "--tags", "测试", f)
	if code != 0 {
		t.Fatalf("flag 在前的 add 失败(%d): %s", code, out)
	}
	if !strings.Contains(out, "0000") {
		t.Errorf("未分配 id: %s", out)
	}
	if got, _ := e.run("get", "0000"); !strings.Contains(got, "甲条目正文") {
		t.Errorf("get 失败: %s", got)
	}
}

func TestDeleteFlagBeforeId(t *testing.T) {
	e := newEnv(t)
	e.run("add", e.write("a.md", bodyA), "--category", "00-甲", "--title", "甲条目", "--tags", "测试")
	if out, code := e.run("delete", "--force", "0000"); code != 0 {
		t.Fatalf("delete --force 0000 失败(%d): %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(e.root, "00-甲", "00-甲条目.kbx")); err == nil {
		t.Errorf("条目未被删除")
	}
}

func TestTreeDepthBeforeId(t *testing.T) {
	e := newEnv(t)
	e.run("add", e.write("a.md", bodyA), "--category", "00-甲", "--title", "甲条目", "--tags", "测试")
	if out, code := e.run("tree", "--depth", "2", "00"); code != 0 {
		t.Errorf("tree --depth 2 00 失败(%d): %s", code, out)
	}
}

func TestTreeCountsAndLeaves(t *testing.T) {
	e := newEnv(t)
	e.run("add", e.write("a.md", bodyA), "--category", "00-甲/00-子", "--title", "甲条目", "--tags", "测试")
	e.run("add", e.write("b.md", bodyB), "--category", "00-甲", "--title", "乙条目", "--tags", "测试")
	out, _ := e.run("tree")
	if !strings.Contains(out, "00-甲") || !strings.Contains(out, "(2)") {
		t.Errorf("tree 未显示目录计数: %s", out)
	}
	if strings.Contains(out, ".kbx") {
		t.Errorf("默认不应显示叶子: %s", out)
	}
	out2, _ := e.run("tree", "-a")
	if !strings.Contains(out2, "甲条目") {
		t.Errorf("-a 应显示叶子: %s", out2)
	}
	out3, _ := e.run("tree", "--json")
	if !strings.Contains(out3, `"count"`) {
		t.Errorf("--json 输出异常: %s", out3)
	}
}

func TestConfigAndHelpWrite(t *testing.T) {
	e := newEnv(t)
	cfg := filepath.Join(e.root, "kb-config.json")
	e.run("add", e.write("a.md", bodyA), "--category", "00-甲", "--title", "写入须知", "--tags", "测试")
	// 设置指针（通过 KB_CONFIG 环境变量隔离配置）
	cmd := exec.Command(kbBin, "config", "set", "write_guide", "0000")
	cmd.Env = append(os.Environ(), "KB_ROOT="+e.root,
		"KB_DB="+filepath.Join(e.root, "..", "kb-test.sqlite"),
		"KB_CONFIG="+cfg, "HOME="+e.root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("config set 失败: %s", out)
	}
	cmd2 := exec.Command(kbBin, "help", "-w")
	cmd2.Env = append(os.Environ(), "KB_ROOT="+e.root,
		"KB_DB="+filepath.Join(e.root, "..", "kb-test.sqlite"),
		"KB_CONFIG="+cfg, "HOME="+e.root)
	out, err := cmd2.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "写入须知") {
		t.Errorf("help -w 异常: %v %s", err, out)
	}
}

func TestNoArgsDoesNotPanic(t *testing.T) {
	e := newEnv(t)
	out, code := e.run()
	if code != 0 || strings.Contains(out, "panic") {
		t.Errorf("无参数不应 panic: code=%d out=%s", code, out)
	}
	if !strings.Contains(out, "kb") {
		t.Errorf("无参数应打印用法: %s", out)
	}
}
