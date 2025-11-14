package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// NormalizeFileName 파일명에서 특수문자를 _로 변경합니다
func NormalizeFileName(filePath string) (string, error) {
	dir := filepath.Dir(filePath)
	fileName := filepath.Base(filePath)
	ext := filepath.Ext(fileName)
	nameWithoutExt := strings.TrimSuffix(fileName, ext)

	// 특수문자([], 공백 등)를 _로 변경
	re := regexp.MustCompile(`[\[\]\s]+`)
	normalized := re.ReplaceAllString(nameWithoutExt, "_")

	// 연속된 _를 하나로 통합
	re2 := regexp.MustCompile(`_+`)
	normalized = re2.ReplaceAllString(normalized, "_")

	// 앞뒤 _ 제거
	normalized = strings.Trim(normalized, "_")

	newFileName := normalized + ext
	return filepath.Join(dir, newFileName), nil
}

// RestoreFileName 파일명을 원래대로 복원합니다
func RestoreFileName(oldPath, newPath string) error {
	if oldPath == newPath {
		return nil
	}
	return os.Rename(newPath, oldPath)
}

// MoveFileToCategory 파일을 카테고리 폴더로 이동시킵니다
func MoveFileToCategory(filePath, destinationPath, category string) error {
	// 카테고리 폴더 생성
	categoryPath := filepath.Join(destinationPath, category)
	if err := os.MkdirAll(categoryPath, 0755); err != nil {
		return fmt.Errorf("카테고리 폴더 생성 실패: %w", err)
	}

	// 파일명 추출
	fileName := filepath.Base(filePath)
	destPath := filepath.Join(categoryPath, fileName)

	// 파일 이동
	if err := os.Rename(filePath, destPath); err != nil {
		return fmt.Errorf("파일 이동 실패: %w", err)
	}

	return nil
}

