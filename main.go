package main

import (
	"fmt"
	"os"
	"strings"
	"time"
	_ "time/tzdata" // TZ 환경변수(예: Asia/Seoul)를 zoneinfo 없는 환경(Termux)에서도 적용
)

// applyTZ TZ 환경변수를 time.Local에 적용합니다.
// Android(Termux)용 Go 런타임은 TZ를 무시하고 항상 UTC를 쓰기 때문에 직접 로드합니다.
func applyTZ() {
	tz := os.Getenv("TZ")
	if tz == "" {
		return
	}
	if loc, err := time.LoadLocation(tz); err == nil {
		time.Local = loc
	} else {
		fmt.Printf("경고: TZ=%q 적용 실패: %v\n", tz, err)
	}
}

func main() {
	applyTZ()

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

	fmt.Printf("검사 경로(%d개, 라운드로빈 %d개씩):\n  %s\n", len(config.SourcePaths), config.BatchSize,
		strings.Join(config.SourcePaths, "\n  "))
	fmt.Printf("이동 경로: %s\n", config.DestinationPath)
	fmt.Printf("에러 폴더: %s\n", config.ErrorPath)
	fmt.Printf("모델명: %s\n", config.Model)
	fmt.Printf("llama.cpp Base URL: %s\n", config.LlamaBaseURL)
	if config.WatchMode {
		fmt.Printf("watch 모드: 켬 (비어 있으면 %d초마다 재검사, 종료하지 않음)\n", config.PollIntervalSec)
	} else {
		fmt.Println("watch 모드: 끔 (모두 처리하면 종료)")
	}
	fmt.Println()

	// llama.cpp 클라이언트 생성
	// launch and supervise llama-server (auto-restart on high system memory)
	var llamaSrv *LlamaServer
	if config.LlamaModel != "" {
		llamaSrv = NewLlamaServer(config)
		if err := llamaSrv.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "error: llama-server start failed: %v\n", err)
			os.Exit(1)
		}
		defer llamaSrv.Stop()
		stopMon := make(chan struct{})
		defer close(stopMon)
		go llamaSrv.MonitorMemory(config.MemThresholdPct, time.Duration(config.MonitorIntervalSec)*time.Second, stopMon)
	}

	llamaClient := NewLlamaClient(config.LlamaBaseURL, config.Model, config.Prompt, config.ValidCategories,
		time.Duration(config.RequestTimeoutSec)*time.Second)

	// 이미지 처리기 생성
	processor := NewImageProcessor(config, llamaClient)

	// 모든 소스를 라운드로빈으로 처리 (watch 모드면 여기서 계속 돈다)
	if err := processor.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "오류: %v\n", err)
		os.Exit(1)
	}

	// 모든 작업 완료 시 로그 파일 삭제
	if err := ClearRestoreLog(); err != nil {
		fmt.Printf("경고: 복원 로그 파일 삭제 실패: %v\n", err)
	}

	fmt.Println("\n모든 이미지 처리가 완료되었습니다.")
}
