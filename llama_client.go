package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
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
// requestTimeout은 요청 1회의 전체 타임아웃입니다 (0이면 무제한). llama-server가 응답 없이 멈추면
// 이 시간 뒤 실패로 처리되어 다음 이미지로 넘어갑니다.
func NewLlamaClient(baseURL, model, prompt string, validCategories []string, requestTimeout time.Duration) *LlamaClient {
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
		client:          &http.Client{Timeout: requestTimeout},
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
	Model          string          `json:"model"`
	Messages       []ChatMessage   `json:"messages"`
	Stream         bool            `json:"stream"`
	Temperature    float64         `json:"temperature"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
}

type ResponseFormat struct {
	Type       string      `json:"type"`
	JSONSchema *JSONSchema `json:"json_schema,omitempty"`
}

type JSONSchema struct {
	Name   string                 `json:"name"`
	Strict bool                   `json:"strict"`
	Schema map[string]interface{} `json:"schema"`
}

// ChatResponse llama-server(OpenAI 호환) 응답 구조체
type ChatResponse struct {
	Choices []struct {
		Message struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
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
	dataURI := "data:image/jpeg;base64," + imageBase64

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

	// no strict json_schema: this is a reasoning model (<think>...</think>);
	// a grammar forcing '{' first yields empty output.
	reqBody.MaxTokens = 512

	url := fmt.Sprintf("%s/v1/chat/completions", lc.baseURL)
	var lastErr error
	// retry the whole request; empty/invalid responses happen intermittently with this
	// vision build, and temperature=0 is deterministic, so vary temp across attempts.
	for outer := 0; outer < 3; outer++ {
		reqBody.Temperature = float64(outer) * 0.4
		jsonData, merr := json.Marshal(reqBody)
		if merr != nil {
			return "", fmt.Errorf("request marshal failed: %w", merr)
		}
		if outer > 0 {
			time.Sleep(5 * time.Second)
		}
		var resp *http.Response
		var perr error
		for attempt := 0; attempt < 6; attempt++ {
			resp, perr = lc.client.Post(url, "application/json", bytes.NewBuffer(jsonData))
			if perr == nil {
				break
			}
			time.Sleep(10 * time.Second) // llama-server may be restarting
		}
		if perr != nil {
			lastErr = fmt.Errorf("API call failed: %w", perr)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
			continue
		}
		var chatResp ChatResponse
		derr := json.NewDecoder(resp.Body).Decode(&chatResp)
		resp.Body.Close()
		if derr != nil {
			lastErr = fmt.Errorf("response decode failed: %w", derr)
			continue
		}
		if len(chatResp.Choices) == 0 {
			lastErr = fmt.Errorf("no choices in response")
			continue
		}
		msg := chatResp.Choices[0].Message
		raw := strings.TrimSpace(msg.Content)
		if raw == "" {
			raw = strings.TrimSpace(msg.ReasoningContent)
		}
		raw = stripThink(raw)
		category := ""
		if js := extractJSON(raw); js != "" {
			var cr ClassifyResponse
			if json.Unmarshal([]byte(js), &cr) == nil {
				category = cleanCategoryName(cr.Category)
			}
		}
		if !lc.isValidCategory(category) {
			category = lc.findCategoryInText(raw)
		}
		if lc.isValidCategory(category) {
			return category, nil
		}
		lastErr = fmt.Errorf("no valid category (resp: %q)", firstN(raw, 200))
		continue
	}
	return "", lastErr
}

// encodeImageToBase64 이미지를 base64로 인코딩합니다
func encodeImageToBase64(imagePath string) (string, error) {
	// Resize to <=448px and convert to JPEG via ffmpeg. Massively speeds up vision
	// encoding and handles webp/gif that the clip loader may otherwise reject.
	tmp, terr := os.CreateTemp("", "clf*.jpg")
	if terr == nil {
		name := tmp.Name()
		tmp.Close()
		defer os.Remove(name)
		cmd := exec.Command("ffmpeg", "-y", "-loglevel", "error", "-i", imagePath,
			"-vf", "scale=448:448:force_original_aspect_ratio=decrease", "-frames:v", "1", name)
		if cmd.Run() == nil {
			if data, rerr := os.ReadFile(name); rerr == nil && len(data) > 0 {
				return base64.StdEncoding.EncodeToString(data), nil
			}
		}
	}
	data, err := os.ReadFile(imagePath)
	if err != nil {
		return "", err
	}
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

func stripThink(s string) string {
	for {
		i := strings.Index(s, "<think>")
		if i < 0 {
			break
		}
		j := strings.Index(s, "</think>")
		if j < 0 {
			s = s[:i]
			break
		}
		s = s[:i] + s[j+len("</think>"):]
	}
	return strings.TrimSpace(s)
}

func (lc *LlamaClient) findCategoryInText(s string) string {
	low := strings.ToLower(s)
	best := ""
	bestPos := -1
	for cat := range lc.validCategories {
		if p := strings.LastIndex(low, cat); p > bestPos {
			bestPos = p
			best = cat
		}
	}
	return best
}

func firstN(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
