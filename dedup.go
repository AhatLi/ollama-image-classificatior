package main

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// 중복 이미지 제거 (sha256 완전 일치).
//
// 시작할 때 library_path와 backfill_paths 아래 모든 .meta.jsonl에서 sha256 -> (폴더, 파일)을 읽어 메모리에 두고,
// 이미지를 태깅하기 전에 해시를 먼저 계산해 이미 기록된 이미지와 같으면(그리고 그 원본 파일이 실제로 있으면)
// 태깅하지 않고 duplicate_action에 따라 옮기거나 지운다.

type dupLoc struct {
	dir  int32 // DupIndex.dirs 인덱스
	file string
}

type DupIndex struct {
	action  string // "move" | "delete" | "keep"
	dupPath string
	dirs    []string
	dirIdx  map[string]int32
	bySHA   map[[32]byte]dupLoc
}

func shaKey(hexstr string) ([32]byte, bool) {
	var k [32]byte
	if len(hexstr) != 64 {
		return k, false
	}
	if _, err := hex.Decode(k[:], []byte(hexstr)); err != nil {
		return k, false
	}
	return k, true
}

func (d *DupIndex) enabled() bool { return d != nil && d.action != "keep" }

func (d *DupIndex) dirID(dir string) int32 {
	if id, ok := d.dirIdx[dir]; ok {
		return id
	}
	id := int32(len(d.dirs))
	d.dirs = append(d.dirs, dir)
	d.dirIdx[dir] = id
	return id
}

// Add는 메타를 기록한 이미지를 집합에 넣는다. 같은 해시가 이미 있으면 그 원본이 아직 있을 때는 그대로 둔다.
func (d *DupIndex) Add(sha, dir, file string) {
	if !d.enabled() {
		return
	}
	k, ok := shaKey(sha)
	if !ok {
		return
	}
	if old, ok := d.bySHA[k]; ok {
		if _, err := os.Lstat(filepath.Join(d.dirs[old.dir], old.file)); err == nil {
			return
		}
	}
	d.bySHA[k] = dupLoc{dir: d.dirID(dir), file: file}
}

// Lookup은 sha가 같은 "다른" 파일이 이미 기록돼 있고 실제로 존재하면 그 경로를 돌려준다.
func (d *DupIndex) Lookup(sha, selfPath string) (string, bool) {
	if !d.enabled() {
		return "", false
	}
	k, ok := shaKey(sha)
	if !ok {
		return "", false
	}
	loc, ok := d.bySHA[k]
	if !ok {
		return "", false
	}
	orig := filepath.Join(d.dirs[loc.dir], loc.file)
	if filepath.Clean(orig) == filepath.Clean(selfPath) {
		return "", false
	}
	if st, err := os.Lstat(orig); err != nil || st.IsDir() {
		return "", false // 원본이 사라졌으면 중복이 아니다 (정상 처리되고 Add가 새 위치로 덮어쓴다)
	}
	return orig, true
}

// loadDir는 폴더 하나의 .meta.jsonl을 읽어 집합에 넣는다 (err 줄 제외, 같은 file은 마지막 줄 기준).
func (d *DupIndex) loadDir(dir string) int {
	f, err := os.Open(filepath.Join(dir, metaFileName))
	if err != nil {
		return 0
	}
	defer f.Close()
	last := map[string]string{} // file -> sha256 ("" = err 줄)
	rd := bufio.NewReaderSize(f, 1<<20)
	var rec struct {
		File   string `json:"file"`
		SHA256 string `json:"sha256"`
		Err    string `json:"err"`
	}
	for {
		line, err := rd.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			rec.File, rec.SHA256, rec.Err = "", "", ""
			if json.Unmarshal(line, &rec) == nil && rec.File != "" {
				if rec.Err != "" {
					last[rec.File] = ""
				} else {
					last[rec.File] = rec.SHA256
				}
			}
		}
		if err != nil {
			if err != io.EOF {
				fmt.Printf("[dedup] 메타 읽기 오류 %s: %v\n", dir, err)
			}
			break
		}
	}
	n := 0
	id := d.dirID(dir)
	for file, sha := range last {
		k, ok := shaKey(sha)
		if !ok {
			continue
		}
		if old, exists := d.bySHA[k]; !exists {
			d.bySHA[k] = dupLoc{dir: id, file: file}
		} else if _, err := os.Lstat(filepath.Join(d.dirs[old.dir], old.file)); err != nil {
			// 같은 해시로 먼저 읽힌 파일이 지금은 없다(지워졌거나 옮겨짐) -> 실제로 있는 쪽을 원본으로 삼는다
			d.bySHA[k] = dupLoc{dir: id, file: file}
		}
		n++
	}
	return n
}

// NewDupIndex는 설정에 따라 집합을 만들고 기존 메타를 모두 읽어 들인다.
func NewDupIndex(config *Config) *DupIndex {
	d := &DupIndex{action: config.DuplicateAction, dupPath: config.DuplicatesPath,
		dirIdx: map[string]int32{}, bySHA: map[[32]byte]dupLoc{}}
	if !d.enabled() {
		fmt.Println("중복 제거: 끔 (duplicate_action=keep)")
		return d
	}
	start := time.Now()
	seen := map[string]bool{}
	records, dirs := 0, 0
	for _, root := range append([]string{config.LibraryPath}, config.BackfillPaths...) {
		root = filepath.Clean(strings.TrimSpace(root))
		if root == "." || seen[root] {
			continue
		}
		seen[root] = true
		_ = filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
			if err != nil {
				if e != nil && e.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if !e.IsDir() {
				return nil
			}
			if path != root && strings.HasPrefix(e.Name(), ".") {
				return filepath.SkipDir
			}
			if n := d.loadDir(path); n > 0 {
				records += n
				dirs++
			}
			return nil
		})
	}
	fmt.Printf("중복 제거: %s (대상 폴더 %s). 기존 메타 %d개 폴더 %d장에서 sha256 %d개 로드 (%.1fs)\n",
		d.action, d.dupPath, dirs, records, len(d.bySHA), time.Since(start).Seconds())
	return d
}

// Handle은 중복 파일을 duplicate_action대로 처리한다. relDir는 duplicates_path 아래에 둘 하위 경로다
// (소스는 소스 이름, backfill은 backfill 루트 기준 상대 폴더).
func (d *DupIndex) Handle(path, orig, sha, relDir string) error {
	where := "삭제"
	switch d.action {
	case "delete":
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("중복 파일 삭제 실패: %w", err)
		}
	default: // move
		destDir := filepath.Join(d.dupPath, relDir)
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return fmt.Errorf("중복 폴더 생성 실패: %w", err)
		}
		dest := uniqueDest(destDir, filepath.Base(path))
		if err := os.Rename(path, dest); err != nil {
			return fmt.Errorf("중복 파일 이동 실패: %w", err)
		}
		where = "-> " + dest
	}
	fmt.Printf("  중복(sha256 일치): %s %s (원본: %s)\n", path, where, orig)
	if err := os.MkdirAll(d.dupPath, 0755); err == nil {
		if f, err := os.OpenFile(filepath.Join(d.dupPath, "duplicates.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err == nil {
			fmt.Fprintf(f, "%s\t%s\t%s\t%s\n", time.Now().In(kst).Format(time.RFC3339), path, orig, sha)
			f.Close()
		}
	}
	return nil
}
