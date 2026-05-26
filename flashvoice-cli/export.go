package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func loadTranscriptions(source string) ([]Transcription, error) {
	data, err := os.ReadFile(source)
	if err != nil {
		return nil, fmt.Errorf("读取源文件失败: %w", err)
	}
	var items []Transcription
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("解析 JSON 失败（请确认源文件格式）: %w", err)
	}
	return items, nil
}

func resolveOutputDir(outputFlag string, positional []string) (string, error) {
	dir := outputFlag
	if dir == "" && len(positional) > 0 {
		dir = positional[0]
	}
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("无法获取当前工作目录: %w", err)
		}
		dir = wd
	}

	info, err := os.Stat(dir)
	if err == nil && !info.IsDir() {
		return "", fmt.Errorf("输出路径必须是目录: %s", dir)
	}
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建输出目录失败: %w", err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir, nil
	}
	return abs, nil
}

func uniqueSRTPath(dir, baseName string, used map[string]int) string {
	name := baseName
	if n, ok := used[baseName]; ok {
		ext := filepath.Ext(baseName)
		stem := strings.TrimSuffix(baseName, ext)
		name = fmt.Sprintf("%s_%d%s", stem, n+1, ext)
		used[baseName] = n + 1
		return uniqueSRTPath(dir, name, used)
	}
	used[baseName] = 0
	return filepath.Join(dir, name)
}

type exportResult struct {
	written int
	skipped int
	dir     string
	files   []string
}

func exportSRT(source, outDir string) (*exportResult, error) {
	items, err := loadTranscriptions(source)
	if err != nil {
		return nil, err
	}

	res := &exportResult{dir: outDir}
	used := make(map[string]int)

	for _, item := range items {
		if len(item.Segments) == 0 {
			res.skipped++
			continue
		}
		if strings.TrimSpace(item.FilePath) == "" {
			res.skipped++
			continue
		}

		dest := uniqueSRTPath(outDir, srtBaseName(item.FilePath), used)
		content := segmentsToSRT(item.Segments)
		if err := os.WriteFile(dest, []byte(content), 0o644); err != nil {
			return res, fmt.Errorf("写入 %s 失败: %w", dest, err)
		}
		res.written++
		res.files = append(res.files, dest)
	}
	return res, nil
}
