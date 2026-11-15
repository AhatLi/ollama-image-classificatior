# llama.cpp 이미지 분류기 (짤 자동 정리 프로그램)

llama.cpp(llama-server)를 사용하여 이미지를 자동으로 분류하고 카테고리별 폴더로 정리하는 Go 프로그램입니다.

## 주요 기능

- 🤖 **llama.cpp를 통한 자동 이미지 분류**: Vision(멀티모달) 모델을 사용하여 이미지를 분석하고 카테고리로 분류합니다
- 📁 **카테고리별 자동 이동**: 분류된 이미지를 해당 카테고리 폴더로 자동 이동합니다
- ⚠️ **에러 처리**: API 오류 발생 시 이미지를 에러 폴더로 이동하여 별도 관리합니다
- 🎨 **다양한 이미지 형식 지원**: JPG, PNG, GIF, WebP, BMP, TIFF 등 다양한 형식을 지원합니다

## 사전 요구사항

- **llama.cpp**: 멀티모달(Vision) GGUF 모델을 실행할 수 있는 `llama-server`
- **Vision GGUF 모델 + mmproj(비전 프로젝터) 파일**: 이미지를 인식하려면 반드시 mmproj 파일이 함께 필요합니다

## llama.cpp 설치 및 설정

### 1. llama.cpp 설치

Termux 기준:

```bash
pkg install llama-cpp
```

다른 환경에서는 소스 빌드 또는 릴리스 바이너리를 사용합니다. `llama-server` 바이너리가 있으면 됩니다.

### 2. Vision 모델 다운로드

이 프로그램은 Vision(멀티모달) 모델이 필요합니다. 본 저장소는 아래 모델로 검증되었습니다:

- 모델(GGUF): `Qwen3.5-2B-Uncensored-HauhauCS-Aggressive-Q4_K_M.gguf`
- 비전 프로젝터(mmproj): `mmproj-Qwen3.5-2B-Uncensored-HauhauCS-Aggressive-f16.gguf`

```bash
mkdir -p models && cd models
BASE=https://huggingface.co/HauhauCS/Qwen3.5-2B-Uncensored-HauhauCS-Aggressive/resolve/main
curl -L -O "$BASE/Qwen3.5-2B-Uncensored-HauhauCS-Aggressive-Q4_K_M.gguf"
curl -L -O "$BASE/mmproj-Qwen3.5-2B-Uncensored-HauhauCS-Aggressive-f16.gguf"
```

### 3. llama-server 실행

모델과 mmproj를 함께 로드하여 OpenAI 호환 서버를 실행합니다. 기본 포트는 `8080`입니다.

```bash
llama-server \
  -m models/Qwen3.5-2B-Uncensored-HauhauCS-Aggressive-Q4_K_M.gguf \
  --mmproj models/mmproj-Qwen3.5-2B-Uncensored-HauhauCS-Aggressive-f16.gguf \
  --host 127.0.0.1 --port 8080
```

`http://localhost:8080/v1/chat/completions` (OpenAI 호환) 엔드포인트를 사용합니다.

### 실행 파일 사용

`llama-server`가 실행 중인 상태에서 빌드된 실행 파일을 실행하면 됩니다:

```bash
go build -o image-classificator .
./image-classificator            # 기본 config.json 사용
./image-classificator my.json    # 다른 설정 파일 지정
```

## 설정 파일 설명

### config.json 필드

| 필드 | 설명 | 필수 | 기본값 |
|------|------|------|--------|
| `source_path` | 분류할 이미지가 있는 경로 (단일) | ✅(둘 중 하나) | - |
| `source_paths` | 분류할 이미지 경로 목록. 여러 개면 라운드로빈으로 순회 | ✅(둘 중 하나) | - |
| `destination_path` | 분류된 이미지를 이동할 기본 경로 | ✅ | - |
| `model` | 모델 라벨명 (llama-server는 로드한 단일 모델을 사용하므로 참고용) | ✅ | - |
| `prompt_file` | 프롬프트 파일 경로 | ❌ | `prompt.txt` |
| `animation_category` | 애니메이션 파일로 분류할 카테고리명 | ❌ | `animation` |
| `llama_base_url` | llama-server URL | ❌ | `http://localhost:8080` |
| `error_path` | 에러 발생 시 이미지를 이동할 경로 | ❌ | `{destination_path}/error` |
| `valid_categories` | 유효한 카테고리 목록 | ❌ | 모든 카테고리 허용 |
| `watch_mode` | `true`면 처리할 이미지가 없어도 종료하지 않고 새 이미지를 기다림 | ❌ | `false` |
| `batch_size` | 소스 하나에서 한 번에 처리할 최대 개수. 넘으면 다음 소스로 넘어감 | ❌ | `100` |
| `poll_interval_sec` | watch 모드에서 처리할 이미지가 없을 때 재검사 주기(초) | ❌ | `30` |
| `min_file_age_sec` | 이 시간(초) 안에 수정된 파일은 아직 다운로드 중으로 보고 건너뜀 | ❌ | `3` |
| `request_timeout_sec` | llama-server 요청 1회 타임아웃(초). 멈춘 요청을 끊고 다음 이미지로 넘어감 | ❌ | `300` |
| `llama_model` / `llama_mmproj` | 지정하면 프로그램이 llama-server를 직접 실행·감시함 | ❌ | `models/` 자동 탐색 |
| `mem_threshold_percent` | 시스템 메모리 사용률이 이 값을 넘으면 llama-server 재시작 | ❌ | `85` |

### 여러 소스 + 연속 실행 (다운로더와 함께 쓰기)

이미지 다운로더가 여러 폴더에 계속 이미지를 내려받는 상황을 위한 설정 예시입니다.
소스마다 `batch_size`개씩 처리하고 다음 소스로 넘어가므로, 한 폴더에 이미지가 계속 들어와도
다른 폴더가 영원히 뒤로 밀리지 않습니다. `watch_mode`가 켜져 있으면 모두 처리한 뒤에도 종료하지 않고
`poll_interval_sec`마다 다시 검사합니다.

```json
{
  "source_paths": ["/path/dl/a", "/path/dl/b", "/path/dl/c"],
  "destination_path": "/path/category",
  "watch_mode": true,
  "batch_size": 100,
  "poll_interval_sec": 30,
  "request_timeout_sec": 300
}
```


### prompt.txt

이미지 분류를 위한 프롬프트 파일입니다. 프롬프트는 다음 요구사항을 만족해야 합니다:

- JSON 형식으로 응답을 반환해야 함
- `{"category": "category_name"}` 형식으로 응답
- `valid_categories`에 정의된 카테고리 중 하나를 반환해야 함

프롬프트 예시는 프로젝트의 `prompt.txt` 파일을 참고하세요.

## 지원하는 이미지 형식

다음 이미지 형식을 지원합니다:

- `.jpg` / `.jpeg`
- `.png`
- `.gif`
- `.webp`
- `.bmp`
- `.tiff` / `.tif`

## 문제 해결

### llama-server 연결 오류

**오류**: `API 호출 실패: connection refused`

**해결 방법**:
1. `llama-server`가 실행 중인지 확인
2. `llama_base_url` 설정이 올바른지 확인 (기본 `http://localhost:8080`)
3. 방화벽이 포트 8080을 차단하지 않는지 확인

### 모델/이미지 인식 오류

**해결 방법**:
1. `--mmproj` 인자로 비전 프로젝터 파일을 함께 로드했는지 확인 (없으면 이미지를 인식하지 못함)
2. 모델과 mmproj 파일 경로가 올바른지 확인
3. 시스템 리소스(메모리, 디스크 공간) 확인
4. 분류에 실패한 이미지는 자동으로 에러 폴더로 이동됩니다

### API 오류 (상태 코드: 500)

**해결 방법**:
1. `llama-server` 로그 확인
2. 모델이 메모리 부족으로 중단되었을 수 있음
3. 더 작은 양자화(quant) 사용 고려 (예: Q6_K → Q4_K_M)
4. 해당 이미지는 자동으로 에러 폴더로 이동됩니다

---

## tagger 백엔드 (ONNX, 점수만 기록)

`backend: "tagger"`로 설정하면 VLM(llama.cpp) 대신 가벼운 ONNX 모델 3개로 이미지를 처리합니다.
분류기는 **판정을 하지 않고 점수와 태그만 기록**하며, 무엇을 nsfw로 볼지·어떤 카테고리로 볼지는
보는 쪽(갤러리)이 이 점수로 필터링합니다. `backend`를 지정하지 않으면 기존 llama 방식 그대로 동작합니다.

| 모델 | 역할 | 파일 (`models/tagger/`) |
|---|---|---|
| SigLIP 2 base/16-224 (vision) | 내용 카테고리·매체(실사/2D 등) zero-shot | `siglip2_vision.onnx` |
| ViT NSFW 판별기 | 수위 점수(0~1) | `nsfw_vit.onnx` |
| WD ViT 태거 v3 | 태그·캐릭터·rating | `wd_vit_v3.onnx`, `wd_vit_v3_tags.csv` |

실측(Redmi K50 Ultra, 4스레드): 장당 2~3초, 태거 서비스 RSS 약 1.5GB.

### 준비

```bash
pkg install python python-numpy python-pillow python-onnxruntime
sh tagger/download_models.sh        # models/tagger/ 에 약 1.1GB
go build -o image-classificator .
```

### 동작

- **새 이미지**: `source_paths`의 이미지를 태깅 → `library_path/YYYY/MM/DD/`(처리 시각, KST)로 이동 →
  그 폴더의 `.meta.jsonl`에 한 줄 기록. gif 등 애니메이션도 첫 프레임으로 태깅해 같은 곳에 둡니다.
  같은 이름이 이미 있으면 `이름_1.확장자`처럼 번호를 붙입니다(덮어쓰지 않음).
- **깨진 이미지**(열 수 없는 파일)는 `error_path`로 이동합니다. 태거 서비스가 죽었거나 응답이 없을 때는
  파일을 옮기지 않고 서비스를 되살린 뒤 다시 시도합니다.
- **backfill**: 소스에 처리할 것이 없을 때마다 `backfill_paths` 아래(재귀)의 기존 이미지 중 그 폴더
  `.meta.jsonl`에 없는 것을 `backfill_batch`장씩 **옮기지 않고** 태깅해 메타만 채웁니다. 새 이미지가 들어오면
  소스가 항상 우선입니다. 재시작해도 이미 기록된 파일은 건너뜁니다. `library_path`도 함께 훑어 메타가 빠진
  파일을 채웁니다. 전부 끝나면 30분마다 새로 생긴 파일만 다시 확인합니다. `watch_mode: false`면 소스와
  backfill을 모두 끝낸 뒤 종료합니다.
- **태거 서비스**(`tagger/tagger_server.py`)는 분류기가 자식 프로세스로 띄우고, 죽으면 다시 띄웁니다.
  로그는 `tagger-server.log`.

### 설정 항목

| 키 | 기본값 | 설명 |
|---|---|---|
| `backend` | `"llama"` | `"tagger"`로 지정하면 이 백엔드 사용 |
| `library_path` | (필수) | 저장 루트. 아래에 `YYYY/MM/DD/` 폴더가 생김 |
| `error_path` | `library_path/_error` | 열 수 없는 이미지를 옮길 곳 |
| `backfill_paths` | `[]` | 메타만 채울 기존 폴더들 |
| `backfill_batch` | `50` | 소스가 비었을 때 한 번에 채울 장수 |
| `duplicate_action` | `"move"` | sha256이 같은 중복 이미지 처리: `move` / `delete` / `keep`(기능 끔) |
| `duplicates_path` | `<library_path의 부모>_duplicates` | 중복을 옮길 곳. `library_path`·`backfill_paths` 밖이어야 함 |
| `tagger_port` | `8090` | 태거 서비스 포트 (127.0.0.1) |
| `tagger_threads` | `4` | onnxruntime 스레드 수 |
| `tagger_python` | `"python"` | 파이썬 실행 파일 |
| `tagger_script` | `tagger/tagger_server.py` | 서비스 스크립트 |
| `tagger_model_dir` | `models/tagger` | 모델 디렉터리 |
| `tagger_labels` | `tagger/labels/siglip_labels.json` | SigLIP 라벨 (같은 이름의 `.npz` 필요) |

`source_paths`, `watch_mode`, `batch_size`, `poll_interval_sec`, `min_file_age_sec`, `request_timeout_sec`,
`monitor_interval_sec`은 llama 백엔드와 같은 의미입니다. `destination_path`, `model`, `prompt_file`,
`valid_categories`, `llama_*`는 tagger 백엔드에서 쓰지 않습니다.

```json
{
  "backend": "tagger",
  "source_paths": ["/path/dl/a", "/path/dl/b"],
  "library_path": "/path/images/library",
  "error_path": "/path/images/category/error",
  "backfill_paths": ["/path/images/category"],
  "watch_mode": true
}
```

### 중복 이미지 제거 (sha256 완전 일치)

내용이 바이트 단위로 완전히 같은 이미지는 한 장만 남깁니다. (리사이즈·재인코딩된 "비슷한" 이미지는 대상이 아닙니다.)

- 시작할 때 `library_path`와 `backfill_paths` 아래 모든 `.meta.jsonl`에서 sha256 → 파일 위치를 읽어 메모리에 두고,
  메타를 기록할 때마다 갱신합니다.
- 소스 처리와 backfill 모두 **태깅 전에** 해시를 먼저 계산합니다. 같은 해시의 다른 파일이 이미 기록돼 있고
  그 파일이 실제로 존재하면 중복으로 보고, 태깅도 메타 기록도 하지 않습니다. 기록만 있고 원본 파일이
  사라진 경우는 중복이 아니므로 정상 처리합니다(먼저 메타가 기록된 쪽이 원본입니다).
- `duplicate_action`:
  - `move`(기본): `duplicates_path`로 옮깁니다. 소스의 중복은 `duplicates_path/<소스 이름>/`,
    backfill의 중복은 backfill 루트 기준 상대 경로를 유지해 `duplicates_path/<상대 폴더>/`.
    이름이 겹치면 `이름_1.확장자`.
  - `delete`: 바로 지웁니다.
  - `keep`: 기능을 끕니다(집합도 읽지 않음).
- 처리한 중복은 `duplicates_path/duplicates.log`에 `시각<TAB>중복 경로<TAB>원본 경로<TAB>sha256`으로 남고,
  분류기 로그에도 `중복(sha256 일치): ...` 줄이 남습니다.

### 메타데이터 형식 (v1)

이미지가 놓인 폴더마다 두 파일이 생깁니다.

- `.meta.jsonl` — 한 줄에 이미지 하나, append-only. **같은 `file`이 여러 번 나오면 마지막 줄이 유효**합니다.
- `.siglip.f16` — SigLIP 이미지 임베딩. float16 little-endian, 레코드당 768개(1536바이트), append-only.

```json
{"v":1,"file":"a.jpg","sha256":"..","bytes":48912,"w":554,"h":554,"ts":"2026-11-15T15:32:19+09:00","src":"mangall","animated":false,"content_top":[["animal",0.93],["meme",0.04],["object",0.01]],"medium":"photo","medium_p":0.99,"nsfw":0.02,"rating":"general","tags":[["cat",0.98]],"chars":[["name",0.97]],"emb":123,"models":"siglip2-b16-224+wd-vit-v3+nsfw-vit","labels":"7c5fba3793"}
```

| 필드 | 내용 |
|---|---|
| `ts` | 새 이미지는 처리 시각, backfill은 파일 수정 시각 (RFC3339, +09:00) |
| `src` | 소스 폴더 이름. backfill은 빈 문자열 |
| `content_top` | 내용 카테고리 상위 3개와 확률 (라벨의 `category` 그룹에서 `nsfw_*` 제외 후 softmax) |
| `medium`, `medium_p` | 매체 1순위와 확률 (라벨의 `medium` 그룹) |
| `nsfw` | NSFW 판별기 점수 0~1 |
| `rating` | WD 태거의 수위 (general/sensitive/questionable/explicit) |
| `tags` | WD 일반 태그, 0.35 이상, 확률 내림차순 |
| `chars` | WD 캐릭터 태그, 0.85 이상 |
| `emb` | `.siglip.f16` 안의 레코드 번호 |
| `labels` | 점수를 낼 때 쓴 라벨 json의 해시 |

태깅에 실패한 파일(backfill)은 `{"v":1,"file":"..","ts":"..","err":"..."}` 줄로 남겨 다시 시도하지 않습니다.

### 라벨 문장을 바꿀 때 (rescore)

내용 카테고리와 매체는 SigLIP이 "문장과 이미지의 유사도"로 정합니다. 문장을 고치려면:

```bash
# 1) (PC 등 torch가 있는 곳에서) tagger/make_siglip_labels.py 의 GROUPS를 고치고 실행
python tagger/make_siglip_labels.py          # siglip_labels.npz / .json 생성
# 2) 두 파일을 tagger/labels/ 에 넣고, 이미지 재처리 없이 저장된 임베딩으로 재계산
python tagger/rescore.py /path/images/library /path/images/category [--dry-run]
# 3) 분류기를 재시작하면 새 이미지에도 새 라벨이 적용됩니다
```

`rescore.py`는 `labels` 해시가 다른 줄만 `content_top`/`medium`/`medium_p`/`labels`를 다시 계산해
갱신된 줄을 덧붙입니다. NSFW 점수와 WD 태그는 문장과 무관하므로 바뀌지 않습니다.
