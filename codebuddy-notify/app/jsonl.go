package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// findSessionJSONL 在 ~/.workbuddy/projects/ 下查找指定 session 的 JSONL 文件
func findSessionJSONL(sessionID string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	projectsDir := filepath.Join(home, ".workbuddy", "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return ""
	}

	target := sessionID + ".jsonl"
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		candidate := filepath.Join(projectsDir, entry.Name(), target)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// extractLastAssistantText 从 JSONL 文件中提取最后一条 assistant 消息的文本
// 返回截断到 maxLen 的纯文本摘要（0 表示无限制）
func extractLastAssistantText(jsonlPath string, maxLen int) string {
	if jsonlPath == "" {
		return ""
	}

	data, err := os.ReadFile(jsonlPath)
	if err != nil {
		return ""
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	// 从后往前找最后一条 assistant 消息
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		var msg map[string]interface{}
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		mtype, _ := msg["type"].(string)
		role, _ := msg["role"].(string)

		if mtype == "message" && role == "assistant" {
			content, ok := msg["content"]
			if !ok {
				continue
			}

			text := extractContentText(content)
			if text == "" {
				continue
			}
			if maxLen > 0 && len(text) > maxLen {
				text = text[:maxLen] + "..."
			}
			return text
		}
	}
	return ""
}

// extractLastUserText 从 JSONL 文件中提取最后一条 user 消息的文本作为标题
func extractLastUserText(jsonlPath string, maxLen int) string {
	if jsonlPath == "" {
		return ""
	}

	data, err := os.ReadFile(jsonlPath)
	if err != nil {
		return ""
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		var msg map[string]interface{}
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		mtype, _ := msg["type"].(string)
		role, _ := msg["role"].(string)

		if mtype == "message" && role == "user" {
			text := extractContentTextUntyped(msg["content"])
			if text == "" {
				continue
			}
			if maxLen > 0 && len(text) > maxLen {
				text = text[:maxLen] + "..."
			}
			return text
		}
	}
	return ""
}

// extractContentTextUntyped 提取所有 content 块的文本（不区分 type）
func extractContentTextUntyped(content interface{}) string {
	switch v := content.(type) {
	case string:
		return strings.TrimSpace(v)
	case []interface{}:
		var texts []string
		for _, block := range v {
			b, ok := block.(map[string]interface{})
			if !ok {
				continue
			}
			if t, ok := b["text"].(string); ok {
				texts = append(texts, strings.TrimSpace(t))
			}
		}
		return strings.Join(texts, "\n")
	}
	return ""
}

// extractContentText 从 JSONL content 字段提取纯文本
// content 可能是 string 或 []interface{}
func extractContentText(content interface{}) string {
	switch v := content.(type) {
	case string:
		return strings.TrimSpace(v)
	case []interface{}:
		var texts []string
		for _, block := range v {
			b, ok := block.(map[string]interface{})
			if !ok {
				continue
			}
			// 只提取 output_text 类型（跳过 tool_use 等）
			btype, _ := b["type"].(string)
			if btype != "" && btype != "output_text" {
				continue
			}
			if t, ok := b["text"].(string); ok {
				texts = append(texts, strings.TrimSpace(t))
			}
		}
		return strings.Join(texts, "\n")
	}
	return ""
}
