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

	// 이전 실행에서 남은 파일명 복원
	fmt.Println("이전 실행에서 변경된 파일명 복원 중...")
	if err := RestoreFromLog(); err != nil {
		fmt.Printf("경고: 파일명 복원 중 오류 발생: %v\n", err)
	}
	fmt.Println()

	// 설정 로드
	fmt.Println("설정 파일 로드 중...")
	config, err := LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "오류: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("검사 경로: %s\n", config.SourcePath)
	fmt.Printf("이동 경로: %s\n", config.DestinationPath)
	fmt.Printf("에러 폴더: %s\n", config.ErrorPath)
	fmt.Printf("모델명: %s\n", config.Model)
	fmt.Printf("llama.cpp Base URL: %s\n\n", config.LlamaBaseURL)

	// llama.cpp 클라이언트 생성
	llamaClient := NewLlamaClient(config.LlamaBaseURL, config.Model, config.Prompt, config.ValidCategories)

	// 이미지 처리기 생성
	processor := NewImageProcessor(config, llamaClient)

	// 모든 이미지 처리
	if err := processor.ProcessAllImages(); err != nil {
		fmt.Fprintf(os.Stderr, "오류: %v\n", err)
		os.Exit(1)
	}

	// 모든 작업 완료 시 로그 파일 삭제
	if err := ClearRestoreLog(); err != nil {
		fmt.Printf("경고: 복원 로그 파일 삭제 실패: %v\n", err)
	}

	fmt.Println("\n모든 이미지 처리가 완료되었습니다.")
}
