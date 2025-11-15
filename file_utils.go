package main

import (
	"encoding/json"
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

// MoveFileToErrorFolder 파일을 에러 폴더로 이동시킵니다
func MoveFileToErrorFolder(filePath, errorPath string) error {
	// 에러 폴더 생성
	if err := os.MkdirAll(errorPath, 0755); err != nil {
		return fmt.Errorf("에러 폴더 생성 실패: %w", err)
	}

	// 파일명 추출
	fileName := filepath.Base(filePath)
	destPath := filepath.Join(errorPath, fileName)

	// 파일 이동
	if err := os.Rename(filePath, destPath); err != nil {
		return fmt.Errorf("파일 이동 실패: %w", err)
	}

	return nil
}

// RestoreLogEntry 복원 로그 항목
type RestoreLogEntry struct {
	Original   string `json:"original"`
	Normalized string `json:"normalized"`
}

// RestoreLog 복원 로그 구조체
type RestoreLog struct {
	Restores []RestoreLogEntry `json:"restores"`
}

const restoreLogFile = "restore.json"

// LoadRestoreLog 복원 로그 파일을 읽습니다
func LoadRestoreLog() (*RestoreLog, error) {
	data, err := os.ReadFile(restoreLogFile)
	if err != nil {
		if os.IsNotExist(err) {
			return &RestoreLog{Restores: []RestoreLogEntry{}}, nil
		}
		return nil, err
	}

	var log RestoreLog
	if err := json.Unmarshal(data, &log); err != nil {
		return nil, fmt.Errorf("로그 파일 파싱 실패: %w", err)
	}

	return &log, nil
}

// SaveRestoreLog 복원 로그를 파일에 저장합니다 (덮어쓰기)
func SaveRestoreLog(log *RestoreLog) error {
	data, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return fmt.Errorf("JSON 마샬링 실패: %w", err)
	}

	return os.WriteFile(restoreLogFile, data, 0644)
}

// AddRestoreLogEntry 복원 로그에 항목을 추가합니다
func AddRestoreLogEntry(originalPath, normalizedPath string) error {
	log, err := LoadRestoreLog()
	if err != nil {
		return err
	}

	// 이미 존재하는지 확인
	for i := range log.Restores {
		if log.Restores[i].Normalized == normalizedPath {
			// 이미 존재하면 업데이트
			log.Restores[i].Original = originalPath
			return SaveRestoreLog(log)
		}
	}

	// 새 항목 추가
	log.Restores = append(log.Restores, RestoreLogEntry{
		Original:   originalPath,
		Normalized: normalizedPath,
	})

	return SaveRestoreLog(log)
}

// RemoveRestoreLogEntry 복원 로그에서 항목을 제거합니다
func RemoveRestoreLogEntry(normalizedPath string) error {
	log, err := LoadRestoreLog()
	if err != nil {
		return err
	}

	// 해당 항목 제거
	var newRestores []RestoreLogEntry
	for _, entry := range log.Restores {
		if entry.Normalized != normalizedPath {
			newRestores = append(newRestores, entry)
		}
	}

	log.Restores = newRestores
	return SaveRestoreLog(log)
}

// RestoreFromLog 로그 파일에서 파일명을 복원합니다
func RestoreFromLog() error {
	log, err := LoadRestoreLog()
	if err != nil {
		return err
	}

	if len(log.Restores) == 0 {
		return nil
	}

	restoredCount := 0
	for _, entry := range log.Restores {
		// 파일이 존재하는지 확인
		if _, err := os.Stat(entry.Normalized); err == nil {
			if restoreErr := RestoreFileName(entry.Original, entry.Normalized); restoreErr == nil {
				restoredCount++
				fmt.Printf("복원됨: %s -> %s\n", entry.Normalized, entry.Original)
			}
		}
	}

	if restoredCount > 0 {
		fmt.Printf("총 %d개의 파일명이 복원되었습니다.\n", restoredCount)
	}

	return nil
}

// ClearRestoreLog 로그 파일을 삭제합니다
func ClearRestoreLog() error {
	if _, err := os.Stat(restoreLogFile); os.IsNotExist(err) {
		return nil
	}
	return os.Remove(restoreLogFile)
}
