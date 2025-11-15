# Ollama 이미지 분류기 (짤 자동 정리 프로그램)

Ollama API를 사용하여 이미지를 자동으로 분류하고 카테고리별 폴더로 정리하는 Go 프로그램입니다.

## 주요 기능

- 🤖 **Ollama API를 통한 자동 이미지 분류**: Vision 모델을 사용하여 이미지를 분석하고 카테고리로 분류합니다
- 📁 **카테고리별 자동 이동**: 분류된 이미지를 해당 카테고리 폴더로 자동 이동합니다
- ⚠️ **에러 처리**: API 오류 발생 시 이미지를 에러 폴더로 이동하여 별도 관리합니다
- 🎨 **다양한 이미지 형식 지원**: JPG, PNG, GIF, WebP, BMP, TIFF 등 다양한 형식을 지원합니다

## 사전 요구사항

- **Ollama**: 이미지 분류를 위한 Vision 모델 실행 환경

## Ollama 설치 및 설정

### 1. Ollama 설치

# Ollama 공식 사이트에서 설치 프로그램 다운로드
# https://ollama.ai/download

### 2. Ollama 서버 실행

설치 후 Ollama 서버가 자동으로 실행됩니다. 수동으로 실행하려면:

```bash
ollama serve
```

기본적으로 `http://localhost:11434`에서 실행됩니다.

### 3. Vision 모델 다운로드

이 프로그램은 Vision 모델이 필요합니다. 예를 들어:

```bash
# Qwen3-VL 모델 다운로드 (4B 버전)
ollama pull qwen3-vl:4b

```
추천 모델
qwen3-vl:2b
qwen3-vl:4b
qwen3-vl:8b

### 4. 모델 확인

다운로드한 모델을 확인하려면:

```bash
ollama list
```


### 실행 파일 사용

이미 빌드된 `image-classificator.exe` 파일이 있다면 바로 실행할 수 있습니다.


## 설정 파일 설명

### config.json 필드

| 필드 | 설명 | 필수 | 기본값 |
|------|------|------|--------|
| `source_path` | 분류할 이미지가 있는 경로 | ✅ | - |
| `destination_path` | 분류된 이미지를 이동할 기본 경로 | ✅ | - |
| `model` | 사용할 Ollama 모델명 (예: `qwen3-vl:4b`) | ✅ | - |
| `prompt_file` | 프롬프트 파일 경로 | ❌ | `prompt.txt` |
| `animation_category` | 애니메이션 파일로 분류할 카테고리명 | ❌ | `animation` |
| `ollama_base_url` | Ollama 서버 URL | ❌ | `http://localhost:11434` |
| `error_path` | 에러 발생 시 이미지를 이동할 경로 | ❌ | `{destination_path}/error` |
| `valid_categories` | 유효한 카테고리 목록 | ❌ | 모든 카테고리 허용 |

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

### Ollama 연결 오류

**오류**: `API 호출 실패: connection refused`

**해결 방법**:
1. Ollama 서버가 실행 중인지 확인:
   ```bash
   ollama serve
   ```
2. `ollama_base_url` 설정이 올바른지 확인
3. 방화벽이 포트 11434를 차단하지 않는지 확인

### 모델 로드 오류

**오류**: `model runner has unexpectedly stopped`

**해결 방법**:
1. 모델이 올바르게 다운로드되었는지 확인:
   ```bash
   ollama list
   ```
2. 모델명이 `config.json`의 `model` 필드와 일치하는지 확인
3. 시스템 리소스(메모리, 디스크 공간) 확인
4. 해당 이미지는 자동으로 에러 폴더로 이동됩니다

### API 오류 (상태 코드: 500)

**오류**: `API 오류 (상태 코드: 500)`

**해결 방법**:
1. Ollama 서버 로그 확인
2. 모델이 메모리 부족으로 중단되었을 수 있음
3. 더 작은 모델 사용 고려 (예: `qwen3-vl:4b` → `qwen3-vl:2b`)
4. 해당 이미지는 자동으로 에러 폴더로 이동됩니다

