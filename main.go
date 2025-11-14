package main

import (
	"fmt"
	"os"
)

func main() {
	// 설정 파일 경로 (기본값: config.json)
	configPath := "config.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	// 설정 로드
	fmt.Println("설정 파일 로드 중...")
	config, err := LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "오류: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("검사 경로: %s\n", config.SourcePath)
	fmt.Printf("이동 경로: %s\n", config.DestinationPath)
	fmt.Printf("모델명: %s\n\n", config.Model)

	// Ollama 클라이언트 생성
	ollamaClient := NewOllamaClient("", config.Model, config.Prompt)

	// 이미지 처리기 생성
	processor := NewImageProcessor(config, ollamaClient)

	// 모든 이미지 처리
	if err := processor.ProcessAllImages(); err != nil {
		fmt.Fprintf(os.Stderr, "오류: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n모든 이미지 처리가 완료되었습니다.")
}
