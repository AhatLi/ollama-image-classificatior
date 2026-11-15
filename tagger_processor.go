package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// TaggerProcessor는 tagger 백엔드의 처리 루프다.
//
//   - 소스 폴더의 이미지: 태깅 → library_path/YYYY/MM/DD/ 로 이동 → 그 폴더의 .meta.jsonl에 기록.
//     카테고리 판정은 하지 않는다 (보는 쪽에서 점수로 필터링).
//   - backfill_paths 아래의 기존 이미지: 옮기지 않고 태깅해서 그 자리 폴더에 메타만 기록.
//     소스에 처리할 것이 없을 때만 backfill_batch 장씩 진행한다.
type TaggerProcessor struct {
	config *Config
	client *TaggerClient
	scan   *ImageProcessor // ScanImages / IsImageFile 재사용
	bf     *backfillState
}

func runTaggerBackend(config *Config) error {
	fmt.Printf("백엔드: tagger (ONNX, 점수만 기록)\n")
	fmt.Printf("저장 루트: %s (YYYY/MM/DD)\n", config.LibraryPath)
	fmt.Printf("에러 폴더: %s\n", config.ErrorPath)
	if len(config.BackfillPaths) > 0 {
		fmt.Printf("backfill 대상(%d장씩):\n  %s\n", config.BackfillBatch, strings.Join(config.BackfillPaths, "\n  "))
	}
	if config.WatchMode {
		fmt.Printf("watch 모드: 켬 (비어 있으면 %d초마다 재검사, 종료하지 않음)\n", config.PollIntervalSec)
	} else {
		fmt.Println("watch 모드: 끔 (소스와 backfill을 모두 처리하면 종료)")
	}
	fmt.Println()

	srv := NewTaggerServer(config)
	if err := srv.Start(); err != nil {
		return err
	}
	defer srv.Stop()
	stopMon := make(chan struct{})
	defer close(stopMon)
	go srv.Monitor(time.Duration(config.MonitorIntervalSec)*time.Second, stopMon)

	// 종료 신호를 받으면 자식(태거 서비스)도 함께 정리한다
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		s := <-sig
		fmt.Printf("\n신호 %v 수신 -> 태거 서비스 종료 후 끝냄\n", s)
		srv.Stop()
		os.Exit(0)
	}()

	tp := &TaggerProcessor{
		config: config,
		client: NewTaggerClient(srv, time.Duration(config.RequestTimeoutSec)*time.Second),
		scan:   NewImageProcessor(config, nil),
		bf:     newBackfillState(config),
	}
	return tp.Run()
}

func fileSHA256(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// uniqueDest는 dir 안에서 name과 겹치지 않는 경로를 돌려준다 (겹치면 name_1.ext, name_2.ext ...).
func uniqueDest(dir, name string) string {
	dest := filepath.Join(dir, name)
	if _, err := os.Lstat(dest); os.IsNotExist(err) {
		return dest
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		dest = filepath.Join(dir, fmt.Sprintf("%s_%d%s", base, i, ext))
		if _, err := os.Lstat(dest); os.IsNotExist(err) {
			return dest
		}
	}
}

func summarize(r *TagResult) string {
	content := strings.Trim(string(r.ContentTop), "[]")
	if i := strings.Index(content, "],"); i >= 0 {
		content = content[:i]
	}
	content = strings.NewReplacer("[", "", "]", "", "\"", "").Replace(content)
	return fmt.Sprintf("content=%s medium=%s(%.2f) nsfw=%.2f rating=%s", content, r.Medium, r.MediumP, r.NSFW, r.Rating)
}

// processSourceImage는 소스 이미지 하나를 태깅하고 날짜 폴더로 옮긴 뒤 메타를 기록한다.
func (tp *TaggerProcessor) processSourceImage(imagePath, src string) error {
	fmt.Printf("처리 중: %s\n", imagePath)
	res, err := tp.client.Classify(imagePath)
	if err != nil {
		if isBadImage(err) {
			if moveErr := MoveFileToErrorFolder(imagePath, tp.config.ErrorPath); moveErr != nil {
				return fmt.Errorf("태깅 실패 및 에러 폴더 이동 실패: %w (이동 오류: %v)", err, moveErr)
			}
			fmt.Printf("  에러 폴더로 이동: %s -> %s\n", imagePath, tp.config.ErrorPath)
		}
		// 서비스 쪽 오류면 파일은 소스에 그대로 두고 다음 라운드에 다시 시도한다
		return fmt.Errorf("태깅 실패: %w", err)
	}

	sha, size, err := fileSHA256(imagePath)
	if err != nil {
		return fmt.Errorf("해시 계산 실패: %w", err)
	}

	now := time.Now()
	destDir := filepath.Join(tp.config.LibraryPath, now.In(kst).Format("2006/01/02"))
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("저장 폴더 생성 실패: %w", err)
	}
	dest := uniqueDest(destDir, filepath.Base(imagePath))
	if err := os.Rename(imagePath, dest); err != nil {
		return fmt.Errorf("파일 이동 실패: %w", err)
	}
	rec := NewMetaRecord(filepath.Base(dest), sha, size, now, src, res)
	if err := AppendMeta(destDir, rec, res.Emb); err != nil {
		// 파일은 이미 옮겨졌다. 메타가 빠진 파일은 backfill이 library_path를 훑을 때 다시 채운다.
		return fmt.Errorf("메타 기록 실패(파일은 %s 로 이동됨): %w", dest, err)
	}
	fmt.Printf("  %s tags=%d\n", summarize(res), strings.Count(string(res.Tags), "],")+btoi(len(res.Tags) > 2))
	fmt.Printf("  이동 완료: -> %s\n", dest)
	return nil
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// processBatch는 소스 하나에서 최대 batch_size개를 처리하고 "성공적으로 처리한" 개수를 돌려준다.
func (tp *TaggerProcessor) processBatch(source string) (int, error) {
	images, err := tp.scan.ScanImages(source)
	if err != nil {
		if os.IsNotExist(err) || strings.Contains(err.Error(), "no such file") {
			return 0, nil
		}
		return 0, err
	}
	if len(images) == 0 {
		return 0, nil
	}
	total := len(images)
	if total > tp.config.BatchSize {
		images = images[:tp.config.BatchSize]
	}
	src := filepath.Base(source)
	fmt.Printf("\n=== [%s] 대기 %d개 중 %d개 처리 ===\n", src, total, len(images))

	moved := 0
	for i, imagePath := range images {
		start := time.Now()
		fmt.Printf("[%s %d/%d] (%s) ", src, i+1, len(images), start.In(kst).Format("2006-01-02 15:04:05"))
		if err := tp.processSourceImage(imagePath, src); err != nil {
			fmt.Printf("error: %v (took %.1fs)\n", err, time.Since(start).Seconds())
			if isBadImage(err) {
				moved++ // 에러 폴더로 빠졌으므로 진행된 것으로 본다
				continue
			}
			// 서비스 문제: 같은 오류가 배치 내내 반복되지 않게 이 소스는 다음 라운드로 미룬다
			break
		}
		moved++
		fmt.Printf("  took %.1fs\n", time.Since(start).Seconds())
	}
	return moved, nil
}

// Run은 소스를 라운드로빈으로 처리하고, 소스가 비면 backfill을 한 묶음 진행한다.
func (tp *TaggerProcessor) Run() error {
	idle := 0
	for {
		processed := 0
		for _, source := range tp.config.SourcePaths {
			n, err := tp.processBatch(source)
			if err != nil {
				fmt.Printf("경고: %s 처리 중 오류: %v\n", source, err)
				continue
			}
			processed += n
		}
		if processed > 0 {
			idle = 0
			continue
		}

		// 소스에 처리할 것이 없다 -> backfill 한 묶음
		if n := tp.backfillBatch(); n > 0 {
			idle = 0
			continue
		}

		if !tp.config.WatchMode {
			return nil
		}
		if idle%20 == 0 {
			fmt.Printf("[watch] (%s) 처리할 이미지 없음, %d초마다 재검사 중...\n",
				time.Now().In(kst).Format("2006-01-02 15:04:05"), tp.config.PollIntervalSec)
		}
		idle++
		time.Sleep(time.Duration(tp.config.PollIntervalSec) * time.Second)
	}
}

// ---------------------------------------------------------------------------
// backfill
// ---------------------------------------------------------------------------

const backfillRewalkInterval = 30 * time.Minute

type backfillState struct {
	roots []string

	dirs   []string // 이번 순회에서 볼 폴더 목록 (정렬됨)
	di     int      // 다음에 열 폴더
	walked bool
	doneAt time.Time            // 마지막으로 전체 순회를 끝낸 시각
	clean  map[string]time.Time // 메타가 다 채워진 폴더 -> 그때의 폴더 mtime (바뀌지 않았으면 다시 읽지 않는다)

	cur     string   // 현재 폴더
	pending []string // 현재 폴더에서 아직 메타가 없는 파일명 (정렬됨)
	pi      int

	total     int // 이번 실행에서 backfill로 처리한 누적 장수
	announced int
	errors    int
}

func newBackfillState(config *Config) *backfillState {
	seen := map[string]bool{}
	var roots []string
	// library_path도 훑는다: 이동 직후 메타 기록에 실패한 파일을 다시 채우기 위해 (평소엔 할 일이 없다)
	for _, p := range append(append([]string{}, config.BackfillPaths...), config.LibraryPath) {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		p = filepath.Clean(p)
		if !seen[p] {
			seen[p] = true
			roots = append(roots, p)
		}
	}
	return &backfillState{roots: roots, clean: map[string]time.Time{}}
}

func (b *backfillState) walk() {
	b.dirs = b.dirs[:0]
	for _, root := range b.roots {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if d != nil && d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.IsDir() {
				return nil
			}
			if path != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			b.dirs = append(b.dirs, path)
			return nil
		})
	}
	sort.Strings(b.dirs)
	b.di = 0
	b.walked = true
}

// openNextDir는 메타가 없는 이미지가 남은 다음 폴더를 연다. 더 없으면 false.
func (b *backfillState) openNextDir(tp *TaggerProcessor) bool {
	for b.di < len(b.dirs) {
		dir := b.dirs[b.di]
		b.di++
		st, err := os.Stat(dir)
		if err != nil {
			continue
		}
		if t, ok := b.clean[dir]; ok && t.Equal(st.ModTime()) {
			continue // 지난번에 다 채웠고 그 뒤로 파일이 추가/삭제되지 않았다
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			fmt.Printf("[backfill] 폴더 읽기 실패 %s: %v\n", dir, err)
			continue
		}
		var images []string
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || strings.HasPrefix(name, ".") || !tp.scan.IsImageFile(name) {
				continue
			}
			images = append(images, name)
		}
		if len(images) == 0 {
			b.clean[dir] = st.ModTime()
			continue
		}
		done, err := LoadMetaFiles(dir)
		if err != nil {
			fmt.Printf("[backfill] 메타 읽기 실패 %s: %v\n", dir, err)
			continue
		}
		pending := images[:0]
		for _, name := range images {
			if _, ok := done[name]; !ok {
				pending = append(pending, name)
			}
		}
		if len(pending) == 0 {
			b.clean[dir] = st.ModTime()
			continue
		}
		b.cur, b.pending, b.pi = dir, pending, 0
		fmt.Printf("[backfill] 폴더 시작: %s (메타 없음 %d장 / 이미지 %d장)\n", dir, len(pending), len(images))
		return true
	}
	return false
}

// backfillBatch는 메타가 없는 기존 이미지를 최대 backfill_batch 장 태깅하고 처리한 수를 돌려준다.
func (tp *TaggerProcessor) backfillBatch() int {
	b := tp.bf
	if len(b.roots) == 0 {
		return 0
	}
	if !b.walked {
		if !b.doneAt.IsZero() && time.Since(b.doneAt) < backfillRewalkInterval {
			return 0 // 방금 전부 끝냈다. 새로 생긴 파일은 조금 뒤에 다시 훑는다
		}
		b.walk()
	}

	start := time.Now()
	n, limit := 0, tp.config.BackfillBatch
	lastDir := b.cur
	for n < limit {
		if b.pi >= len(b.pending) {
			if b.cur != "" {
				if st, err := os.Stat(b.cur); err == nil {
					// .meta.jsonl이 처음 만들어지면 폴더 mtime이 바뀌므로 다 끝난 뒤의 값으로 기록한다
					b.clean[b.cur] = st.ModTime()
				}
				fmt.Printf("[backfill] 폴더 완료: %s\n", b.cur)
				b.cur, b.pending, b.pi = "", nil, 0
			}
			if !b.openNextDir(tp) {
				b.walked = false
				b.doneAt = time.Now()
				if b.total+n > b.announced {
					b.announced = b.total + n
					fmt.Printf("[backfill] (%s) 전체 순회 완료: 메타 없는 이미지 없음 (이번 실행 누적 %d장, 실패 %d장). %s 뒤 다시 확인\n",
						time.Now().In(kst).Format("2006-01-02 15:04:05"), b.total+n, b.errors, backfillRewalkInterval)
				}
				break
			}
		}
		lastDir = b.cur
		name := b.pending[b.pi]
		path := filepath.Join(b.cur, name)
		st, err := os.Stat(path)
		if err != nil { // 그 사이 지워졌거나 옮겨짐
			b.pi++
			continue
		}
		res, err := tp.client.Classify(path)
		if err != nil {
			if !isBadImage(err) {
				// 서비스 문제: 표시하지 않고 멈춘다 (다음 호출에서 같은 파일부터 다시)
				fmt.Printf("[backfill] 태깅 중단 (%s): %v\n", name, err)
				break
			}
			b.errors++
			if werr := AppendMetaErr(b.cur, name, st.ModTime(), err.Error()); werr != nil {
				fmt.Printf("[backfill] err 줄 기록 실패 %s: %v\n", path, werr)
			}
			fmt.Printf("[backfill] 태깅 불가 %s: %v\n", path, err)
			b.pi++
			n++
			continue
		}
		sha, size, err := fileSHA256(path)
		if err != nil {
			fmt.Printf("[backfill] 해시 실패 %s: %v\n", path, err)
			b.pi++
			continue
		}
		rec := NewMetaRecord(name, sha, size, st.ModTime(), "", res)
		if err := AppendMeta(b.cur, rec, res.Emb); err != nil {
			fmt.Printf("[backfill] 메타 기록 실패 %s: %v\n", path, err)
			break
		}
		b.pi++
		n++
	}
	if n > 0 {
		b.total += n
		el := time.Since(start).Seconds()
		fmt.Printf("[backfill] (%s) %s +%d장 (이 폴더 남은 %d장, 이번 실행 누적 %d장, 실패 %d장) %.1fs/장\n",
			time.Now().In(kst).Format("2006-01-02 15:04:05"), lastDir, n, len(b.pending)-b.pi, b.total, b.errors, el/float64(n))
	}
	return n
}
