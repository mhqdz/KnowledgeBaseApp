package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

var (
	E     = ".kbx"
	D     = ".md"
	TPL   = "TEMPLATE.md"
	reFM  = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n`)
	rePFX = regexp.MustCompile(`^[0-9a-z]{2}-`)
	reID2 = regexp.MustCompile(`^[0-9a-z]{2}`)
	reBad = regexp.MustCompile(`[\\/\x00-\x1f]`)
)

type Meta map[string]string

func parseFM(text string) (Meta, string) {
	m := Meta{}
	loc := reFM.FindStringSubmatchIndex(text)
	if loc == nil {
		return m, text
	}
	for _, line := range strings.Split(text[loc[2]:loc[3]], "\n") {
		if strings.HasPrefix(line, " ") || !strings.Contains(line, ":") {
			continue
		}
		kv := strings.SplitN(line, ":", 2)
		k := strings.TrimSpace(kv[0])
		v := strings.TrimSpace(kv[1])
		v = strings.TrimSuffix(strings.TrimPrefix(v, "["), "]")
		m[k] = v
	}
	return m, text[loc[1]:]
}

func readEntry(p string) (Meta, string, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, "", err
	}
	m, body := parseFM(string(b))
	return m, body, nil
}

func metaTags(m Meta) string { return m["tags"] }

func makeFM(title, tags string) string {
	return fmt.Sprintf("---\ntitle: %s\ntags: [%s]\nupdated: %s\n---\n\n", title, tags, time.Now().Format("2006-01-02"))
}

func summary(body string) string {
	for _, l := range strings.Split(body, "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "#") && !strings.HasPrefix(l, "|") {
			s := strings.ReplaceAll(l, "**", "")
			if len([]rune(s)) > 100 {
				s = string([]rune(s)[:100])
			}
			return s
		}
	}
	return ""
}
