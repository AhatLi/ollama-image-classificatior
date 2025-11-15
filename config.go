package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config 설정 파일 구조체
type Config struct {
	SourcePath        string   `json:"source_path"`
	DestinationPath   string   `json:"destination_path"`
	Model             string   `json:"model"`
	PromptFile        string   `json:"prompt_file"`
	AnimationCategory string   `json:"animation_category"`
	ValidCategories   []string `json:"valid_categories"`
	OllamaBaseURL     string   `json:"ollama_base_url"`
	ErrorPath         string   `json:"error_path"` // 에러 폴더 경로 (선택적, 없으면 destination_path/error 사용)
	Prompt            string   // 내부 사용용 (파일에서 읽은 내용)
}

// LoadConfig 설정 파일을 로드합니다
func LoadConfig(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("설정 파일을 읽을 수 없습니다: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("설정 파일 파싱 실패: %w", err)
	}

	if config.SourcePath == "" {
		return nil, fmt.Errorf("source_path가 설정되지 않았습니다")
	}

	if config.DestinationPath == "" {
		return nil, fmt.Errorf("destination_path가 설정되지 않았습니다")
	}

	// 기본 모델명 설정
	if config.Model == "" {
		return nil, fmt.Errorf("model이 설정되지 않았습니다")
	}

	// 기본 애니메이션 카테고리 설정
	if config.AnimationCategory == "" {
		config.AnimationCategory = "animation"
	}

	// 기본 Ollama Base URL 설정
	if config.OllamaBaseURL == "" {
		config.OllamaBaseURL = "http://localhost:11434"
	}

	// 기본 에러 폴더 경로 설정 (지정되지 않은 경우 destination_path/error 사용)
	if config.ErrorPath == "" {
		config.ErrorPath = filepath.Join(config.DestinationPath, "error")
	}

	// 프롬프트 파일 읽기
	promptFile := config.PromptFile
	if promptFile == "" {
		promptFile = "prompt.txt" // 기본값
	}

	promptData, err := os.ReadFile(promptFile)
	if err != nil {
		return nil, fmt.Errorf("프롬프트 파일을 읽을 수 없습니다: %w", err)
	}
	config.Prompt = "%s\n" + string(promptData)

	return &config, nil
}
