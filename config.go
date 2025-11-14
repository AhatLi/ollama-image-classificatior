package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config 설정 파일 구조체
type Config struct {
	SourcePath      string `json:"source_path"`
	DestinationPath string `json:"destination_path"`
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

	return &config, nil
}

