package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// OllamaClient Ollama API 클라이언트
type OllamaClient struct {
	baseURL         string
	model           string
	prompt          string
	client          *http.Client
	validCategories map[string]bool
}

// NewOllamaClient 새로운 Ollama 클라이언트를 생성합니다
func NewOllamaClient(baseURL, model, prompt string, validCategories []string) *OllamaClient {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	if model == "" {
		model = "qwen3-vl:latest"
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

	return &OllamaClient{
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

// GenerateRequest Ollama API 요청 구조체
type GenerateRequest struct {
	Model  string   `json:"model"`
	Prompt string   `json:"prompt"`
	Stream bool     `json:"stream"`
	Images []string `json:"images,omitempty"`
}

// GenerateResponse Ollama API 응답 구조체
type GenerateResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

// ClassifyImage 이미지를 분류합니다
func (oc *OllamaClient) ClassifyImage(imagePath string) (string, error) {
	// 프롬프트 생성 (설정에서 읽은 프롬프트 템플릿에 이미지 경로 삽입)
	prompt := fmt.Sprintf(oc.prompt, imagePath)

	// 이미지를 base64로 인코딩
	imageBase64, err := encodeImageToBase64(imagePath)
	if err != nil {
		return "", fmt.Errorf("이미지 인코딩 실패: %w", err)
	}

	// 요청 생성
	reqBody := GenerateRequest{
		Model:  oc.model,
		Prompt: prompt,
		Stream: false,
		Images: []string{imageBase64},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("요청 생성 실패: %w", err)
	}

	// API 호출
	url := fmt.Sprintf("%s/api/generate", oc.baseURL)
	resp, err := oc.client.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("API 호출 실패: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API 오류 (상태 코드: %d): %s", resp.StatusCode, string(body))
	}

	// 응답 파싱
	var generateResp GenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&generateResp); err != nil {
		return "", fmt.Errorf("응답 파싱 실패: %w", err)
	}

	// JSON 응답 추출
	responseText := generateResp.Response
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
	if !oc.isValidCategory(category) {
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
func (oc *OllamaClient) isValidCategory(category string) bool {
	// validCategories가 nil이면 검증 비활성화 (모든 카테고리 허용)
	if oc.validCategories == nil {
		return true
	}
	return oc.validCategories[category]
}
