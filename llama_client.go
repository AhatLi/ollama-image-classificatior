package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// LlamaClient llama.cpp (llama-server) API 클라이언트
// llama-server의 OpenAI 호환 /v1/chat/completions 엔드포인트를 사용합니다.
type LlamaClient struct {
	baseURL         string
	model           string
	prompt          string
	client          *http.Client
	validCategories map[string]bool
}

// NewLlamaClient 새로운 llama.cpp 클라이언트를 생성합니다
func NewLlamaClient(baseURL, model, prompt string, validCategories []string) *LlamaClient {
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	if model == "" {
		// llama-server는 실행 시 로드한 단일 모델을 사용하므로 model 값은 라벨 용도입니다.
		model = "local"
	}

	// 유효한 카테고리 맵 생성
	var categoryMap map[string]bool
	if len(validCategories) > 0 {
		categoryMap = make(map[string]bool)
		for _, cat := range validCategories {
			categoryMap[strings.ToLower(cat)] = true
		}
	} else {
		// validCategories가 비어있으면 nil로 설정하여 검증 비활성화
		categoryMap = nil
	}

	return &LlamaClient{
		baseURL:         baseURL,
		model:           model,
		prompt:          prompt,
		client:          &http.Client{},
		validCategories: categoryMap,
	}
}

// ClassifyResponse 분류 응답 구조체
type ClassifyResponse struct {
	Category string `json:"category"`
}

// ChatMessage OpenAI 호환 채팅 메시지 (멀티모달 컨텐츠 지원)
type ChatMessage struct {
	Role    string        `json:"role"`
	Content []ContentPart `json:"content"`
}

// ContentPart 메시지 컨텐츠 파트 (텍스트 또는 이미지)
type ContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL 이미지 URL (data URI base64 포함)
type ImageURL struct {
	URL string `json:"url"`
}

// ChatRequest llama-server(OpenAI 호환) 요청 구조체
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Stream      bool          `json:"stream"`
	Temperature float64       `json:"temperature"`
}

// ChatResponse llama-server(OpenAI 호환) 응답 구조체
type ChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// ClassifyImage 이미지를 분류합니다
func (lc *LlamaClient) ClassifyImage(imagePath string) (string, error) {
	// 프롬프트 생성 (설정에서 읽은 프롬프트 템플릿에 이미지 경로 삽입)
	prompt := fmt.Sprintf(lc.prompt, imagePath)

	// 이미지를 base64로 인코딩
	imageBase64, err := encodeImageToBase64(imagePath)
	if err != nil {
		return "", fmt.Errorf("이미지 인코딩 실패: %w", err)
	}

	// data URI 생성 (llama-server 멀티모달 입력 형식)
	dataURI := fmt.Sprintf("data:%s;base64,%s", mimeTypeForImage(imagePath), imageBase64)

	// 요청 생성
	reqBody := ChatRequest{
		Model:       lc.model,
		Stream:      false,
		Temperature: 0,
		Messages: []ChatMessage{
			{
				Role: "user",
				Content: []ContentPart{
					{Type: "text", Text: prompt},
					{Type: "image_url", ImageURL: &ImageURL{URL: dataURI}},
				},
			},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("요청 생성 실패: %w", err)
	}

	// API 호출 (OpenAI 호환 엔드포인트)
	url := fmt.Sprintf("%s/v1/chat/completions", lc.baseURL)
	resp, err := lc.client.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("API 호출 실패: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API 오류 (상태 코드: %d): %s", resp.StatusCode, string(body))
	}

	// 응답 파싱
	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", fmt.Errorf("응답 파싱 실패: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("응답에 choices가 없습니다")
	}

	// JSON 응답 추출
	responseText := chatResp.Choices[0].Message.Content
	responseText = strings.TrimSpace(responseText)

	// JSON 부분만 추출 (마크다운 코드 블록 제거)
	responseText = extractJSON(responseText)

	// 카테고리 파싱
	var classifyResp ClassifyResponse
	if err := json.Unmarshal([]byte(responseText), &classifyResp); err != nil {
		return "", fmt.Errorf("카테고리 파싱 실패: %w (응답: %s)", err, responseText)
	}

	if classifyResp.Category == "" {
		return "", fmt.Errorf("카테고리가 비어있습니다 (응답: %s)", responseText)
	}

	// 카테고리 이름 정리 (콜론 이후 설명 제거)
	category := cleanCategoryName(classifyResp.Category)

	// 카테고리 이름 검증
	if !lc.isValidCategory(category) {
		return "", fmt.Errorf("유효하지 않은 카테고리: %s (원본: %s)", category, classifyResp.Category)
	}

	return category, nil
}

// encodeImageToBase64 이미지를 base64로 인코딩합니다
func encodeImageToBase64(imagePath string) (string, error) {
	data, err := os.ReadFile(imagePath)
	if err != nil {
		return "", err
	}

	// base64 인코딩
	return base64.StdEncoding.EncodeToString(data), nil
}

// mimeTypeForImage 확장자에 따른 MIME 타입을 반환합니다
func mimeTypeForImage(imagePath string) string {
	switch strings.ToLower(filepath.Ext(imagePath)) {
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	case ".tiff", ".tif":
		return "image/tiff"
	default:
		return "image/jpeg"
	}
}

// extractJSON 텍스트에서 JSON 부분만 추출합니다
func extractJSON(text string) string {
	// JSON 객체 찾기
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")

	if start == -1 || end == -1 || end < start {
		return text
	}

	return text[start : end+1]
}

// cleanCategoryName 카테고리 이름에서 설명 부분을 제거합니다
// 예: "illustration: General 2D art..." -> "illustration"
func cleanCategoryName(category string) string {
	category = strings.TrimSpace(category)

	// 콜론(:) 이후 부분 제거
	if idx := strings.Index(category, ":"); idx != -1 {
		category = category[:idx]
	}

	// 공백 제거
	category = strings.TrimSpace(category)

	// 소문자로 변환
	category = strings.ToLower(category)

	return category
}

// isValidCategory 카테고리가 유효한지 검증합니다
func (lc *LlamaClient) isValidCategory(category string) bool {
	// validCategories가 nil이면 검증 비활성화 (모든 카테고리 허용)
	if lc.validCategories == nil {
		return true
	}
	return lc.validCategories[category]
}
