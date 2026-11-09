package entry

import (
	"fmt"
	"os"
	"strings"
	"time"

	"kbapp/internal/store"
)

type Meta map[string]string

func ParseFM(text string) (Meta, string) {
	m := Meta{}
	loc := store.ReFM.FindStringSubmatchIndex(text)
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

func Read(p string) (Meta, string, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, "", err
	}
	m, body := ParseFM(string(b))
	return m, body, nil
}

func Tags(m Meta) string { return m["tags"] }

func MakeFM(title, tags string) string {
	return fmt.Sprintf("---\ntitle: %s\ntags: [%s]\nupdated: %s\n---\n\n", title, tags, time.Now().Format("2006-01-02"))
}

func Summary(body string) string {
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
