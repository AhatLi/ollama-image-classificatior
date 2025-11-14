package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ImageProcessor 이미지 처리기
type ImageProcessor struct {
	config       *Config
	ollamaClient *OllamaClient
	imageExts    map[string]bool
}

// NewImageProcessor 새로운 이미지 처리기를 생성합니다
func NewImageProcessor(config *Config, ollamaClient *OllamaClient) *ImageProcessor {
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
		config:       config,
		ollamaClient: ollamaClient,
		imageExts:    exts,
	}
}

// IsImageFile 파일이 이미지 파일인지 확인합니다
func (ip *ImageProcessor) IsImageFile(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	return ip.imageExts[ext]
}

// ScanImages 경로에서 이미지 파일을 스캔합니다 (하위 경로 제외)
func (ip *ImageProcessor) ScanImages(rootPath string) ([]string, error) {
	var images []string

	// 해당 경로의 파일만 읽기 (하위 디렉토리 제외)
	entries, err := os.ReadDir(rootPath)
	if err != nil {
		return nil, fmt.Errorf("디렉토리 읽기 실패: %w", err)
	}

	for _, entry := range entries {
		// 디렉토리는 건너뛰기
		if entry.IsDir() {
			continue
		}

		// 파일 경로 생성
		filePath := filepath.Join(rootPath, entry.Name())

		// 이미지 파일인지 확인
		if ip.IsImageFile(filePath) {
			images = append(images, filePath)
		}
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
	}

	// Ollama API로 이미지 분류
	category, err := ip.ollamaClient.ClassifyImage(imagePath)
	if err != nil {
		// unknown format 또는 invalid format 오류인지 확인
		errStr := err.Error()
		if strings.Contains(errStr, "unknown format") || strings.Contains(errStr, "invalid format") {
			// 애니메이션 파일로 분류
			category = "animation"
			fmt.Printf("  형식 오류 감지: 애니메이션 파일로 분류\n")
		} else {
			// 다른 오류인 경우 원래 이름으로 복원 후 종료
			if needsRestore {
				if restoreErr := RestoreFileName(originalPath, normalizedPath); restoreErr != nil {
					fmt.Printf("경고: 파일명 복원 실패 (%s -> %s): %v\n", normalizedPath, originalPath, restoreErr)
				}
			}
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
	}

	// 파일 이동
	if err := MoveFileToCategory(imagePath, ip.config.DestinationPath, category); err != nil {
		return fmt.Errorf("파일 이동 실패: %w", err)
	}

	fmt.Printf("  이동 완료: %s -> %s/%s/\n", imagePath, ip.config.DestinationPath, category)
	return nil
}

// ProcessAllImages 모든 이미지를 처리합니다
func (ip *ImageProcessor) ProcessAllImages() error {
	// 이미지 스캔
	fmt.Printf("이미지 스캔 중: %s\n", ip.config.SourcePath)
	images, err := ip.ScanImages(ip.config.SourcePath)
	if err != nil {
		return err
	}

	fmt.Printf("총 %d개의 이미지를 찾았습니다.\n\n", len(images))

	// 각 이미지 처리
	for i, imagePath := range images {
		fmt.Printf("[%d/%d] ", i+1, len(images))
		if err := ip.ProcessImage(imagePath); err != nil {
			fmt.Printf("오류: %v\n", err)
			continue
		}
	}

	return nil
}
