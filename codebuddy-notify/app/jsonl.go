package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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
// 会自动剥离 <system-reminder>、<user_info> 等注入标签，
// 并优先提取 <user_query> 中的实际用户输入
func extractContentTextUntyped(content interface{}) string {
	raw := extractRawText(content)
	if raw == "" {
		return ""
	}

	// 1. 优先提取 <user_query> 标签中的实际用户输入
	qmRe := regexp.MustCompile(`(?s)<user_query>\s*(.*?)\s*</user_query>`)
	if m := qmRe.FindStringSubmatch(raw); m != nil && strings.TrimSpace(m[1]) != "" {
		return strings.TrimSpace(m[1])
	}

	// 2. 剥离注入的 system-reminder 块
	sysRe := regexp.MustCompile(`(?s)<system-reminder[^>]*>.*?</system-reminder>`)
	cleaned := sysRe.ReplaceAllString(raw, "")

	// 3. 剥离 <user_info>, <project_context>, <additional_data> 等大块注入
	blockRe := regexp.MustCompile(`(?s)<(user_info|project_context|additional_data|memory_and_skills_reminder|connector-status|user_custom_instructions|identity_context|product_identity|tone_and_style|instructions_for_visualizer|visualizer_examples|task_management|asking_questions|tool_usage_policy|agent_skills|expert_management|mcp_configuration|agent_loop|result_presentation|code-explorer_subagent_usage|automations|personal_files_safety)[^>]*>.*?</(user_info|project_context|additional_data|memory_and_skills_reminder|connector-status|user_custom_instructions|identity_context|product_identity|tone_and_style|instructions_for_visualizer|visualizer_examples|task_management|asking_questions|tool_usage_policy|agent_skills|expert_management|mcp_configuration|agent_loop|result_presentation|code-explorer_subagent_usage|automations|personal_files_safety)>`)
	cleaned = blockRe.ReplaceAllString(cleaned, "")

	// 4. 剥离剩余 XML 标签
	tagRe := regexp.MustCompile(`<[^>]+>`)
	cleaned = tagRe.ReplaceAllString(cleaned, " ")

	// 5. 合并多余空白
	spaceRe := regexp.MustCompile(`\s+`)
	cleaned = spaceRe.ReplaceAllString(cleaned, " ")

	return strings.TrimSpace(cleaned)
}

// extractRawText 从 content 字段提取原始文本（不剥离任何标签）
func extractRawText(content interface{}) string {
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
