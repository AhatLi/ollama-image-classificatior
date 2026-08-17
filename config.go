package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config 설정 파일 구조체
type Config struct {
	SourcePath         string   `json:"source_path"`
	DestinationPath    string   `json:"destination_path"`
	Model              string   `json:"model"`
	PromptFile         string   `json:"prompt_file"`
	AnimationCategory  string   `json:"animation_category"`
	ValidCategories    []string `json:"valid_categories"`
	LlamaBaseURL       string   `json:"llama_base_url"`
	LlamaServerBin     string   `json:"llama_server_bin"`
	LlamaModel         string   `json:"llama_model"`
	LlamaMmproj        string   `json:"llama_mmproj"`
	LlamaHost          string   `json:"llama_host"`
	LlamaPort          int      `json:"llama_port"`
	LlamaCtxSize       int      `json:"llama_ctx_size"`
	LlamaExtraArgs     []string `json:"llama_extra_args"`
	MemThresholdPct    float64  `json:"mem_threshold_percent"`
	MonitorIntervalSec int      `json:"monitor_interval_sec"`
	ErrorPath          string   `json:"error_path"` // 에러 폴더 경로 (선택적, 없으면 destination_path/error 사용)
	Prompt             string   // 내부 사용용 (파일에서 읽은 내용)
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

	// 기본 llama.cpp(llama-server) Base URL 설정
	if config.LlamaBaseURL == "" {
		config.LlamaBaseURL = "http://localhost:8080"
	}
	if config.LlamaServerBin == "" {
		config.LlamaServerBin = "llama-server"
	}
	if config.LlamaHost == "" {
		config.LlamaHost = "127.0.0.1"
	}
	if config.LlamaPort == 0 {
		config.LlamaPort = 8080
	}
	if config.LlamaCtxSize == 0 {
		config.LlamaCtxSize = 16384
	}
	if config.MemThresholdPct == 0 {
		config.MemThresholdPct = 85
	}
	if config.MonitorIntervalSec == 0 {
		config.MonitorIntervalSec = 15
	}
	if config.LlamaModel == "" || config.LlamaMmproj == "" {
		m, mm := autodetectModels("models")
		if config.LlamaModel == "" {
			config.LlamaModel = m
		}
		if config.LlamaMmproj == "" {
			config.LlamaMmproj = mm
		}
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

// autodetectModels finds a GGUF model and its mmproj in dir (e.g. "models").
func autodetectModels(dir string) (model string, mmproj string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", ""
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		low := strings.ToLower(name)
		if !strings.HasSuffix(low, ".gguf") {
			continue
		}
		if strings.Contains(low, "mmproj") {
			if mmproj == "" {
				mmproj = filepath.Join(dir, name)
			}
		} else if model == "" {
			model = filepath.Join(dir, name)
		}
	}
	return model, mmproj
}
