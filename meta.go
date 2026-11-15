package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// 메타데이터 형식 (갤러리와 공유하는 계약, v1)
//
// 이미지가 놓인 폴더마다:
//
//	.meta.jsonl  : 한 줄에 이미지 하나 (append-only, 같은 file은 마지막 줄이 유효)
//	.siglip.f16  : SigLIP 이미지 임베딩 (float16 LE, 레코드당 768개 = 1536바이트, append-only)
//
// 메타 줄의 "emb"는 .siglip.f16 안의 레코드 번호다.
const (
	metaFileName = ".meta.jsonl"
	embFileName  = ".siglip.f16"
	embRecBytes  = 768 * 2
)

// kst는 메타의 ts와 날짜 폴더에 쓰는 시간대다 (Android Go 런타임은 TZ를 무시하므로 고정).
var kst = time.FixedZone("KST", 9*3600)

// MetaRecord는 .meta.jsonl 한 줄이다. 필드 순서가 곧 출력 순서다.
type MetaRecord struct {
	V          int             `json:"v"`
	File       string          `json:"file"`
	SHA256     string          `json:"sha256"`
	Bytes      int64           `json:"bytes"`
	W          int             `json:"w"`
	H          int             `json:"h"`
	TS         string          `json:"ts"`
	Src        string          `json:"src"`
	Animated   bool            `json:"animated"`
	ContentTop json.RawMessage `json:"content_top"`
	Medium     string          `json:"medium"`
	MediumP    float64         `json:"medium_p"`
	NSFW       float64         `json:"nsfw"`
	Rating     string          `json:"rating"`
	Tags       json.RawMessage `json:"tags"`
	Chars      json.RawMessage `json:"chars"`
	Emb        int64           `json:"emb"`
	Models     string          `json:"models"`
	Labels     string          `json:"labels"`
}

// MetaErrRecord는 태깅에 실패한 파일을 기록하는 줄이다 (backfill이 같은 파일을 계속 재시도하지 않도록).
type MetaErrRecord struct {
	V    int    `json:"v"`
	File string `json:"file"`
	TS   string `json:"ts"`
	Err  string `json:"err"`
}

func rawOrEmpty(r json.RawMessage) json.RawMessage {
	if len(r) == 0 {
		return json.RawMessage("[]")
	}
	return r
}

// NewMetaRecord는 태거 응답으로 메타 레코드를 만든다 (emb 번호는 AppendMeta가 채운다).
func NewMetaRecord(file, sha string, size int64, ts time.Time, src string, r *TagResult) *MetaRecord {
	return &MetaRecord{
		V: 1, File: file, SHA256: sha, Bytes: size, W: r.W, H: r.H,
		TS: ts.In(kst).Format(time.RFC3339), Src: src, Animated: r.Animated,
		ContentTop: rawOrEmpty(r.ContentTop), Medium: r.Medium, MediumP: r.MediumP,
		NSFW: r.NSFW, Rating: r.Rating, Tags: rawOrEmpty(r.Tags), Chars: rawOrEmpty(r.Chars),
		Emb: -1, Models: r.Models, Labels: r.Labels,
	}
}

func marshalLine(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil { // Encode는 끝에 개행을 붙인다
		return nil, err
	}
	return buf.Bytes(), nil
}

// appendLine은 한 줄을 한 번의 write로 덧붙인다 (O_APPEND라 다른 프로세스의 append와 섞이지 않는다).
func appendLine(dir string, line []byte) error {
	f, err := os.OpenFile(filepath.Join(dir, metaFileName), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(line)
	return err
}

// AppendMeta는 임베딩을 먼저 .siglip.f16에 쓰고, 그 레코드 번호를 담은 메타 줄을 .meta.jsonl에 덧붙인다.
// 임베딩만 쓰이고 메타 줄이 안 쓰인 경우(중간에 죽음)는 쓰이지 않는 레코드가 하나 남을 뿐 정합성은 깨지지 않는다.
func AppendMeta(dir string, rec *MetaRecord, embB64 string) error {
	rec.Emb = -1
	if raw, err := base64.StdEncoding.DecodeString(embB64); err == nil && len(raw) == embRecBytes {
		f, err := os.OpenFile(filepath.Join(dir, embFileName), os.O_CREATE|os.O_RDWR, 0644)
		if err != nil {
			return fmt.Errorf("임베딩 파일 열기 실패: %w", err)
		}
		st, err := f.Stat()
		if err != nil {
			f.Close()
			return err
		}
		size := st.Size() - st.Size()%embRecBytes // 중간에 끊긴 불완전 레코드는 덮어쓴다
		if _, err := f.WriteAt(raw, size); err != nil {
			f.Close()
			return fmt.Errorf("임베딩 쓰기 실패: %w", err)
		}
		if err := f.Close(); err != nil {
			return err
		}
		rec.Emb = size / embRecBytes
	}
	line, err := marshalLine(rec)
	if err != nil {
		return err
	}
	return appendLine(dir, line)
}

// AppendMetaErr는 태깅 실패 줄을 기록한다.
func AppendMetaErr(dir, file string, ts time.Time, msg string) error {
	line, err := marshalLine(&MetaErrRecord{V: 1, File: file, TS: ts.In(kst).Format(time.RFC3339), Err: msg})
	if err != nil {
		return err
	}
	return appendLine(dir, line)
}

// LoadMetaFiles는 폴더의 .meta.jsonl에 이미 기록된 파일명 집합을 돌려준다 (없으면 빈 집합).
func LoadMetaFiles(dir string) (map[string]struct{}, error) {
	done := make(map[string]struct{})
	f, err := os.Open(filepath.Join(dir, metaFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return done, nil
		}
		return nil, err
	}
	defer f.Close()
	rd := bufio.NewReaderSize(f, 1<<20)
	var rec struct {
		File string `json:"file"`
	}
	for {
		line, err := rd.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			rec.File = ""
			if json.Unmarshal(line, &rec) == nil && rec.File != "" {
				done[rec.File] = struct{}{}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return done, nil
}
