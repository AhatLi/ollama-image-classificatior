package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ImageProcessor 이미지 처리기
type ImageProcessor struct {
	config      *Config
	llamaClient *LlamaClient
	imageExts   map[string]bool
}

// NewImageProcessor 새로운 이미지 처리기를 생성합니다
func NewImageProcessor(config *Config, llamaClient *LlamaClient) *ImageProcessor {
	// 지원하는 이미지 확장자
	exts := map[string]bool{
		".jpg":  true,
		".jpeg": true,
		".png":  true,
		".gif":  true,
		".webp": true,
		".bmp":  true,
		".tiff": true,
		".tif":  true,
	}

	return &ImageProcessor{
		config:      config,
		llamaClient: llamaClient,
		imageExts:   exts,
	}
}

// IsImageFile 파일이 이미지 파일인지 확인합니다
func (ip *ImageProcessor) IsImageFile(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	return ip.imageExts[ext]
}

// ScanImages 경로에서 이미지 파일을 스캔합니다 (하위 경로 제외).
// 아직 쓰는 중일 수 있는 파일(min_file_age_sec 이내 수정)은 건너뜁니다.
// 결과는 파일명 순으로 정렬되어 있습니다 (os.ReadDir 보장).
func (ip *ImageProcessor) ScanImages(rootPath string) ([]string, error) {
	var images []string

	// 해당 경로의 파일만 읽기 (하위 디렉토리 제외)
	entries, err := os.ReadDir(rootPath)
	if err != nil {
		return nil, fmt.Errorf("디렉토리 읽기 실패: %w", err)
	}

	minAge := time.Duration(ip.config.MinFileAgeSec) * time.Second
	now := time.Now()

	for _, entry := range entries {
		// 디렉토리는 건너뛰기
		if entry.IsDir() {
			continue
		}

		// 파일 경로 생성
		filePath := filepath.Join(rootPath, entry.Name())

		// 이미지 파일인지 확인
		if !ip.IsImageFile(filePath) {
			continue
		}

		// 방금 생성/수정된 파일은 다운로드가 끝나지 않았을 수 있으므로 다음 라운드에 처리
		if minAge > 0 {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			if now.Sub(info.ModTime()) < minAge {
				continue
			}
		}

		images = append(images, filePath)
	}

	return images, nil
}

// ProcessImage 이미지 하나를 처리합니다
func (ip *ImageProcessor) ProcessImage(imagePath string) error {
	fmt.Printf("처리 중: %s\n", imagePath)

	// 원본 파일 경로 저장
	originalPath := imagePath

	// 파일명 정규화
	normalizedPath, err := NormalizeFileName(imagePath)
	if err != nil {
		return fmt.Errorf("파일명 정규화 실패: %w", err)
	}

	// 파일명 변경 (특수문자 제거)
	needsRestore := false
	if originalPath != normalizedPath {
		if err := os.Rename(originalPath, normalizedPath); err != nil {
			return fmt.Errorf("파일명 변경 실패: %w", err)
		}
		imagePath = normalizedPath
		needsRestore = true

		// 복원 로그에 기록
		if err := AddRestoreLogEntry(originalPath, normalizedPath); err != nil {
			fmt.Printf("경고: 복원 로그 저장 실패: %v\n", err)
		}
	}

	// llama.cpp API로 이미지 분류
	category, err := ip.llamaClient.ClassifyImage(imagePath)
	if err != nil {
		// unknown format 또는 invalid format 오류인지 확인
		errStr := err.Error()
		if strings.Contains(errStr, "unknown format") || strings.Contains(errStr, "invalid format") {
			// 애니메이션 파일로 분류
			category = ip.config.AnimationCategory
			fmt.Printf("  형식 오류 감지: %s로 분류\n", category)
		} else {
			// API 오류인 경우 (500 등) 에러 폴더로 이동
			// 원래 이름으로 복원
			if needsRestore {
				if restoreErr := RestoreFileName(originalPath, normalizedPath); restoreErr != nil {
					fmt.Printf("경고: 파일명 복원 실패 (%s -> %s): %v\n", normalizedPath, originalPath, restoreErr)
					// 복원 실패해도 에러 폴더로 이동 시도
					imagePath = normalizedPath
				} else {
					imagePath = originalPath
					// 복원 성공 시 로그에서 제거
					if err := RemoveRestoreLogEntry(normalizedPath); err != nil {
						fmt.Printf("경고: 복원 로그 제거 실패: %v\n", err)
					}
				}
			}

			// 에러 폴더로 이동
			if moveErr := MoveFileToErrorFolder(imagePath, ip.config.ErrorPath); moveErr != nil {
				return fmt.Errorf("이미지 분류 실패 및 에러 폴더 이동 실패: %w (이동 오류: %v)", err, moveErr)
			}

			fmt.Printf("  에러 폴더로 이동: %s -> %s\n", imagePath, ip.config.ErrorPath)
			return fmt.Errorf("이미지 분류 실패: %w", err)
		}
	}

	fmt.Printf("  분류 결과: %s\n", category)

	// 카테고리 폴더로 이동
	// 이동 전에 원래 이름으로 복원
	if needsRestore {
		if err := RestoreFileName(originalPath, normalizedPath); err != nil {
			return fmt.Errorf("파일명 복원 실패: %w", err)
		}
		imagePath = originalPath

		// 복원 성공 시 로그에서 제거
		if err := RemoveRestoreLogEntry(normalizedPath); err != nil {
			fmt.Printf("경고: 복원 로그 제거 실패: %v\n", err)
		}
	}

	// 파일 이동
	if err := MoveFileToCategory(imagePath, ip.config.DestinationPath, category); err != nil {
		return fmt.Errorf("파일 이동 실패: %w", err)
	}

	fmt.Printf("  이동 완료: %s -> %s/%s/\n", imagePath, ip.config.DestinationPath, category)
	return nil
}

// processBatch 소스 하나에서 최대 batch_size개의 이미지를 처리하고 처리한 개수를 돌려줍니다.
// 소스 폴더가 아직 없으면(다운로더가 만들기 전) 0을 돌려줍니다.
func (ip *ImageProcessor) processBatch(source string) (int, error) {
	images, err := ip.ScanImages(source)
	if err != nil {
		if os.IsNotExist(err) || strings.Contains(err.Error(), "no such file") {
			return 0, nil
		}
		return 0, err
	}
	if len(images) == 0 {
		return 0, nil
	}

	total := len(images)
	if total > ip.config.BatchSize {
		images = images[:ip.config.BatchSize]
	}
	fmt.Printf("\n=== [%s] 대기 %d개 중 %d개 처리 ===\n", filepath.Base(source), total, len(images))

	for i, imagePath := range images {
		start := time.Now()
		fmt.Printf("[%s %d/%d] (%s) ", filepath.Base(source), i+1, len(images), start.Format("2006-01-02 15:04:05"))
		if err := ip.ProcessImage(imagePath); err != nil {
			fmt.Printf("error: %v (took %.1fs)\n", err, time.Since(start).Seconds())
			continue
		}
		fmt.Printf("  took %.1fs\n", time.Since(start).Seconds())
	}
	return len(images), nil
}

// Run 모든 소스 경로를 라운드로빈으로 순회하며 이미지를 처리합니다.
// 소스마다 batch_size개씩 처리하고 다음 소스로 넘어가므로, 이미지가 계속 들어오는 소스가 있어도
// 다른 소스가 영원히 뒤로 밀리지 않습니다.
// watch_mode가 true면 처리할 이미지가 없어도 종료하지 않고 poll_interval_sec마다 다시 검사합니다.
func (ip *ImageProcessor) Run() error {
	round := 0
	for {
		round++
		processed := 0
		for _, source := range ip.config.SourcePaths {
			n, err := ip.processBatch(source)
			if err != nil {
				fmt.Printf("경고: %s 처리 중 오류: %v\n", source, err)
				continue
			}
			processed += n
		}

		if processed > 0 {
			continue
		}
		if !ip.config.WatchMode {
			return nil
		}
		// 처리할 이미지 없음: 잠시 대기 후 다시 스캔 (로그 과다 방지를 위해 가끔만 출력)
		if round%20 == 1 {
			fmt.Printf("[watch] (%s) 처리할 이미지 없음, %d초마다 재검사 중...\n",
				time.Now().Format("2006-01-02 15:04:05"), ip.config.PollIntervalSec)
		}
		time.Sleep(time.Duration(ip.config.PollIntervalSec) * time.Second)
	}
}

// ProcessAllImages 하위 호환용: 모든 소스를 한 번 순회합니다 (watch_mode 무시).
func (ip *ImageProcessor) ProcessAllImages() error {
	saved := ip.config.WatchMode
	ip.config.WatchMode = false
	defer func() { ip.config.WatchMode = saved }()
	return ip.Run()
}
