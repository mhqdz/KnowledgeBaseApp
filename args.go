package main

import "strings"

// 会带一个值参数的 flag（其余 --xxx 视为布尔开关）
var valueFlags = map[string]bool{"--to": true, "--title": true, "--tags": true, "--category": true, "--depth": true, "--range": true, "--port": true, "--searxng": true, "--offset": true, "--max-chars": true, "--timeout": true, "--n": true, "-n": true, "--limit": true, "-limit": true}

// positional 取第 n 个位置参数（跳过 flag 与其值，flag 放在哪里都行）
func positional(args []string, n int) string {
	pos := []string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			if !strings.Contains(a, "=") && valueFlags[a] {
				i++
			}
			continue
		}
		pos = append(pos, a)
	}
	if n >= 0 && n < len(pos) {
		return pos[n]
	}
	return ""
}

func flagValue(args []string, name string) string {
	// 全量扫描：flag 可以出现在位置参数之后（kb update <id> --to <dir>）
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == name {
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		}
		if strings.HasPrefix(a, name+"=") {
			return strings.TrimPrefix(a, name+"=")
		}
	}
	return ""
}

func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == name {
			return true
		}
	}
	return false
}
