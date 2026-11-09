package gitx

import (
	"fmt"
	"os/exec"
	"strings"

	"kbapp/internal/index"
	"kbapp/internal/store"
)

func Git(args ...string) (string, int) {
	c := exec.Command("git", append([]string{"-C", store.Root}, args...)...)
	b, err := c.CombinedOutput()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = 1
		}
	}
	return string(b), code
}

func Commit(msg string) {
	Git("add", "-A")
	if out, code := Git("commit", "-qm", msg); code != 0 {
		low := strings.ToLower(out)
		if !strings.Contains(low, "nothing to commit") && !strings.Contains(low, "无文件要提交") {
			fmt.Println("  !! git commit 失败：" + strings.TrimSpace(FirstLine(out)))
		}
	}
}

func FirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func Push(quiet bool) int {
	out, code := Git("push", "-q", "origin", "HEAD")
	if code == 0 {
		if !quiet {
			fmt.Println("pushed -> origin")
		}
	} else {
		last := ""
		ls := strings.Split(strings.TrimSpace(out), "\n")
		if len(ls) > 0 {
			last = ls[len(ls)-1]
		}
		if len([]rune(last)) > 120 {
			last = string([]rune(last)[:120])
		}
		fmt.Println("push 失败（本地已提交，稍后 kb push 重试）：" + last)
	}
	return code
}

func Sync() int {
	Git("pull", "--rebase", "-q")
	index.Rebuild()
	Push(false)
	return 0
}
