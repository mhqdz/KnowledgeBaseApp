package main

import (
	"fmt"
	"os/exec"
	"strings"
)

func git(args ...string) (string, int) {
	c := exec.Command("git", append([]string{"-C", kbDir}, args...)...)
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

func commit(msg string) {
	git("add", "-A")
	if out, code := git("commit", "-qm", msg); code != 0 {
		low := strings.ToLower(out)
		if !strings.Contains(low, "nothing to commit") && !strings.Contains(low, "无文件要提交") {
			fmt.Println("  !! git commit 失败：" + strings.TrimSpace(firstLine(out)))
		}
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func push(quiet bool) int {
	out, code := git("push", "-q", "origin", "HEAD")
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

func syncCmd() int {
	git("pull", "--rebase", "-q")
	rebuild()
	push(false)
	return 0
}
