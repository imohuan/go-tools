package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

const defaultFileName = "fv_transcriptions.json"

// Segment 与 FlashVoice 转写分段结构一致。
type Segment struct {
	StartMs int64  `json:"start_ms"`
	EndMs   int64  `json:"end_ms"`
	Text    string `json:"text"`
}

// Transcription 与 fv_transcriptions.json 中单条记录结构一致。
type Transcription struct {
	ID           string    `json:"id"`
	FilePath     string    `json:"filePath"`
	FileSize     int64     `json:"fileSize"`
	Status       string    `json:"status"`
	Progress     int       `json:"progress"`
	Language     string    `json:"language"`
	OutputFormat string    `json:"outputFormat"`
	CreatedAt    int64     `json:"createdAt"`
	Duration     int64     `json:"duration"`
	Result       string    `json:"result"`
	AIContent    string    `json:"aiContent"`
	Segments     []Segment `json:"segments"`
}

func defaultSourcePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("无法获取用户主目录: %w", err)
	}
	return filepath.Join(home, "AppData", "Roaming", "com.flashvoices", defaultFileName), nil
}

func main() {
	var (
		sourceFlag = flag.String("source", "", "源 JSON 路径（默认: %USERPROFILE%\\AppData\\Roaming\\com.flashvoices\\fv_transcriptions.json）")
		outputFlag = flag.String("o", "", "输出目录（也可用位置参数指定，例如: flashvoice-cli output）")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "用法: %s [输出目录] [-o 输出目录] [-source 源路径]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "从 FlashVoice 转写 JSON 导出多条 SRT 字幕，文件名取自 filePath 的视频文件名。\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	source := *sourceFlag
	if source == "" {
		var err error
		source, err = defaultSourcePath()
		if err != nil {
			fmt.Fprintf(os.Stderr, "错误: %v\n", err)
			os.Exit(1)
		}
	}

	if _, err := os.Stat(source); err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "错误: 源文件不存在: %s\n", source)
		} else {
			fmt.Fprintf(os.Stderr, "错误: 无法访问源文件: %v\n", err)
		}
		os.Exit(1)
	}

	outDir, err := resolveOutputDir(*outputFlag, flag.Args())
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}

	res, err := exportSRT(source, outDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("已导出 %d 个 SRT 文件", res.written)
	if res.skipped > 0 {
		fmt.Printf("（跳过 %d 条无分段或缺少 filePath 的记录）", res.skipped)
	}
	fmt.Println()
	fmt.Printf("源: %s\n", source)
	fmt.Printf("输出目录: %s\n", res.dir)
	const maxList = 10
	for i, f := range res.files {
		if i >= maxList {
			fmt.Printf("  ... 还有 %d 个文件\n", len(res.files)-maxList)
			break
		}
		fmt.Printf("  - %s\n", filepath.Base(f))
	}
}
