package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

func openDB() *sql.DB {
	os.MkdirAll(filepath.Dir(dbPath), 0o755)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		die("打开数据库失败: %v", err)
	}
	db.Exec(`CREATE TABLE IF NOT EXISTS entries(
      id INTEGER PRIMARY KEY, path TEXT UNIQUE, kind TEXT, title TEXT, summary TEXT,
      category TEXT, updated TEXT, tags TEXT, ident TEXT);
      CREATE TABLE IF NOT EXISTS tags(entry_id INT, tag TEXT);`)
	for _, col := range []string{"updated TEXT", "tags TEXT", "ident TEXT"} {
		db.Exec("ALTER TABLE entries ADD COLUMN " + col) // 旧库补列；已存在则报错被忽略
	}
	db.Exec("DROP TABLE IF EXISTS aliases") // 旧表名，已废弃（数据是派生的，直接丢）
	return db
}

func upsert(p string) {
	r := rel(p)
	m, body, _ := readEntry(p)
	db := openDB()
	defer db.Close()
	_, err := db.Exec(`INSERT INTO entries(path,kind,title,summary,category,updated,tags,ident)
      VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(path) DO UPDATE SET title=excluded.title,summary=excluded.summary,
      updated=excluded.updated,tags=excluded.tags,ident=excluded.ident`,
		r, "leaf", m["title"], summary(body), filepath.Dir(r), m["updated"], metaTags(m), mustID(r))
	if err != nil {
		return
	}
	var id int64
	if err := db.QueryRow("SELECT id FROM entries WHERE path=?", r).Scan(&id); err != nil || id == 0 {
		fmt.Printf("  !! 取条目 id 失败（path=%s）：%v —— tags 未写入，请跑 kb index 重建\n", r, err)
		return
	}
	db.Exec("DELETE FROM tags WHERE entry_id=?", id)
	for _, t := range strings.Split(metaTags(m), ",") {
		if s := strings.TrimSpace(t); s != "" {
			db.Exec("INSERT INTO tags VALUES(?,?)", id, s)
		}
	}
}

func dropRow(r string) {
	db := openDB()
	defer db.Close()
	db.Exec("DELETE FROM tags WHERE entry_id IN (SELECT id FROM entries WHERE path=?)", r)
	db.Exec("DELETE FROM entries WHERE path=?", r)
}

func rebuild() {
	db := openDB()
	defer db.Close()
	db.Exec("DELETE FROM tags")
	db.Exec("DELETE FROM entries")
	n := 0
	for _, p := range walkEntries() {
		m, body, _ := readEntry(p)
		r := rel(p)
		res, err := db.Exec(`INSERT INTO entries(path,kind,title,summary,category,updated,tags,ident)
          VALUES(?,?,?,?,?,?,?,?)`, r, "leaf", m["title"], summary(body), filepath.Dir(r), m["updated"], metaTags(m), mustID(r))
		if err != nil {
			fmt.Println("  !! 写入数据库失败：" + err.Error())
			continue
		}
		id, _ := res.LastInsertId()
		for _, t := range strings.Split(metaTags(m), ",") {
			if s := strings.TrimSpace(t); s != "" {
				db.Exec("INSERT INTO tags VALUES(?,?)", id, s)
			}
		}
		n++
	}
	fmt.Printf("indexed %d entries -> %s\n", n, dbPath)
}
