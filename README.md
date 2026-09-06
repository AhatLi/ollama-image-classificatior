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
