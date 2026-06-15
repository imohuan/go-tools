package main

import (
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var _ = embed.FS{}

//go:embed web/assets/conversation-viewer.html
var viewerHTML string

// ========== 数据结构 ==========

// ProjectInfo 项目信息
type ProjectInfo struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	ConvCount int `json:"conv_count"`
}

// ConvInfo 对话摘要信息
type ConvInfo struct {
	SessionID     string `json:"session_id"`
	Title         string `json:"title"`
	Date          string `json:"date"`
	MsgCount      int    `json:"msg_count"`
	SubAgentCount int    `json:"sub_agent_count"`
	Timestamp     int64  `json:"timestamp"`
	ProjectName   string `json:"project_name,omitempty"`
}

// APIResponse 通用 API 响应
type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// ========== API 处理器 ==========

type Server struct {
	projectsDir string
}

func NewServer(projectsDir string) *Server {
	return &Server{projectsDir: projectsDir}
}

// listProjects 列出所有项目
func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(s.projectsDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	var projects []ProjectInfo
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		projectPath := filepath.Join(s.projectsDir, entry.Name())

		// 统计 JSONL 文件数量
		jsonlFiles, _ := filepath.Glob(filepath.Join(projectPath, "*.jsonl"))
		convCount := len(jsonlFiles)

		projects = append(projects, ProjectInfo{
			Name:      entry.Name(),
			Path:      projectPath,
			ConvCount: convCount,
		})
	}

	sort.Slice(projects, func(i, j int) bool {
		return projects[i].Name > projects[j].Name
	})

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: projects})
}

// listConversations 列出项目下的对话
func (s *Server) listConversations(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")

	projectPath := filepath.Join(s.projectsDir, projectName)
	if _, err := os.Stat(projectPath); os.IsNotExist(err) {
		writeJSON(w, http.StatusNotFound, APIResponse{Error: "项目不存在"})
		return
	}

	files, err := filepath.Glob(filepath.Join(projectPath, "*.jsonl"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	var convs []ConvInfo
	for _, f := range files {
		sessionID := strings.TrimSuffix(filepath.Base(f), ".jsonl")
		info := parseConvFile(f, sessionID, projectName)
		if info != nil {
			convs = append(convs, *info)
		}
	}

	sort.Slice(convs, func(i, j int) bool {
		return convs[i].Timestamp > convs[j].Timestamp
	})

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: convs})
}

// getConversation 获取对话详情
func (s *Server) getConversation(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	sessionID := r.PathValue("session")

	filePath := filepath.Join(s.projectsDir, projectName, sessionID+".jsonl")
	data, err := os.ReadFile(filePath)
	if err != nil {
		writeJSON(w, http.StatusNotFound, APIResponse{Error: "对话文件不存在"})
		return
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var messages []json.RawMessage
	for _, line := range lines {
		if line == "" {
			continue
		}
		messages = append(messages, json.RawMessage(line))
	}

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: map[string]interface{}{
		"session_id": sessionID,
		"messages":   messages,
		"total":      len(messages),
	}})
}

// getSubAgent 获取子代理详情
func (s *Server) getSubAgent(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	sessionID := r.PathValue("session")
	agentID := r.PathValue("agent")

	filePath := filepath.Join(s.projectsDir, projectName, sessionID, "subagents", agentID+".jsonl")

	// 兼容 agent- 前缀
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		filePath = filepath.Join(s.projectsDir, projectName, sessionID, "subagents", "agent-"+agentID+".jsonl")
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		writeJSON(w, http.StatusNotFound, APIResponse{Error: "子代理文件不存在"})
		return
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var messages []json.RawMessage
	for _, line := range lines {
		if line == "" {
			continue
		}
		messages = append(messages, json.RawMessage(line))
	}

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: map[string]interface{}{
		"session_id": agentID,
		"messages":   messages,
		"total":      len(messages),
	}})
}

// allConversations 一次性返回所有项目的所有对话摘要
func (s *Server) allConversations(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(s.projectsDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	type ProjectWithConvs struct {
		Name          string     `json:"name"`
		ConvCount     int        `json:"conv_count"`
		Conversations []ConvInfo `json:"conversations"`
	}

	var result []ProjectWithConvs
	totalConvs := 0

	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		projectPath := filepath.Join(s.projectsDir, entry.Name())

		files, _ := filepath.Glob(filepath.Join(projectPath, "*.jsonl"))
		if len(files) == 0 {
			continue
		}

		var convs []ConvInfo
		for _, f := range files {
			sessionID := strings.TrimSuffix(filepath.Base(f), ".jsonl")
			info := parseConvFile(f, sessionID, entry.Name())
			if info != nil {
				convs = append(convs, *info)
			}
		}

		sort.Slice(convs, func(i, j int) bool {
			return convs[i].Timestamp > convs[j].Timestamp
		})

		result = append(result, ProjectWithConvs{
			Name:          entry.Name(),
			ConvCount:     len(convs),
			Conversations: convs,
		})
		totalConvs += len(convs)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name > result[j].Name
	})

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: map[string]interface{}{
		"projects":    result,
		"total_convs": totalConvs,
	}})
}

// serveHTML 提供 HTML 页面
func (s *Server) serveHTML(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write([]byte(viewerHTML))
}

func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	// API 路由
	mux.HandleFunc("GET /api/all-conversations", s.allConversations)       // 一次性返回所有
	mux.HandleFunc("GET /api/projects", s.listProjects)
	mux.HandleFunc("GET /api/projects/{project}/conversations", s.listConversations)
	mux.HandleFunc("GET /api/projects/{project}/conversations/{session}", s.getConversation)
	mux.HandleFunc("GET /api/projects/{project}/conversations/{session}/subagents/{agent}", s.getSubAgent)

	// 首页
	mux.HandleFunc("GET /", s.serveHTML)
}

// ========== 工具函数 ==========

func parseConvFile(filePath, sessionID, projectName string) *ConvInfo {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		return nil
	}

	var firstMsg map[string]interface{}
	json.Unmarshal([]byte(lines[0]), &firstMsg)

	var lastMsg map[string]interface{}
	json.Unmarshal([]byte(lines[len(lines)-1]), &lastMsg)

	// 提取标题
	title := sessionID
	if firstMsg != nil {
		if mtype, _ := firstMsg["type"].(string); mtype == "message" {
			if role, _ := firstMsg["role"].(string); role == "user" {
				if content := extractText(firstMsg["content"]); len(content) > 0 {
					title = content
					if len(title) > 80 {
						title = title[:80] + "..."
					}
				}
			}
		}
	}

	// 时间戳
	var timestamp int64
	if ts, ok := firstMsg["timestamp"].(float64); ok {
		timestamp = int64(ts)
	}

	// 格式化日期
	date := ""
	if timestamp > 0 {
		date = time.UnixMilli(timestamp).Format("01-02 15:04")
	}

	// 统计消息数和子代理数
	msgCount := 0
	subAgentCount := 0
	for _, line := range lines {
		var d map[string]interface{}
		if json.Unmarshal([]byte(line), &d) != nil {
			continue
		}
		mtype, _ := d["type"].(string)
		if mtype == "message" {
			role, _ := d["role"].(string)
			if role == "user" || role == "assistant" {
				msgCount++
			}
		}
		if mtype == "function_call_result" {
			if name, _ := d["name"].(string); name == "Agent" {
				subAgentCount++
			}
		}
	}

	return &ConvInfo{
		SessionID:     sessionID,
		Title:         title,
		Date:          date,
		MsgCount:      msgCount,
		SubAgentCount: subAgentCount,
		Timestamp:     timestamp,
		ProjectName:   projectName,
	}
}

func extractText(content interface{}) string {
	switch v := content.(type) {
	case string:
		// 去掉 system-reminder 块
		sysRe := regexp.MustCompile(`(?s)<system-reminder[^>]*>.*?</system-reminder>`)
		cleaned := sysRe.ReplaceAllString(v, "")
		// 去掉其他 XML 标签
		tagRe := regexp.MustCompile(`<[^>]+>`)
		cleaned = tagRe.ReplaceAllString(cleaned, " ")
		// 合并多余空白
		spaceRe := regexp.MustCompile(`\s+`)
		cleaned = spaceRe.ReplaceAllString(cleaned, " ")
		cleaned = strings.TrimSpace(cleaned)
		if len(cleaned) > 100 {
			cleaned = cleaned[:100] + "..."
		}
		return cleaned
	case []interface{}:
		var texts []string
		for _, block := range v {
			if b, ok := block.(map[string]interface{}); ok {
				if t, ok := b["text"].(string); ok {
					texts = append(texts, t)
				}
			}
		}
		result := strings.Join(texts, "\n")
		if len(result) > 100 {
			result = result[:100]
		}
		return strings.TrimSpace(result)
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// ========== 入口 ==========

func StartServer(port int, projectsDir string) {
	if projectsDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("无法获取用户主目录: %v", err)
		}
		projectsDir = filepath.Join(home, ".workbuddy", "projects")
	}

	// 检查目录是否存在
	if _, err := os.Stat(projectsDir); os.IsNotExist(err) {
		log.Printf("警告: 项目目录不存在: %s", projectsDir)
	}

	server := NewServer(projectsDir)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	addr := fmt.Sprintf(":%d", port)
	log.Printf("=== CodeBuddy 对话查看器 ===")
	log.Printf("服务地址: http://localhost%s", addr)
	log.Printf("项目目录: %s", projectsDir)

	// 打开浏览器（可选）
	go func() {
		time.Sleep(500 * time.Millisecond)
		_ = openBrowser(fmt.Sprintf("http://localhost%s", addr))
	}()

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("服务器启动失败: %v", err)
	}
}

func openBrowser(url string) error {
	_, err := sql.Open("dummy", "")
	_ = err
	return nil // TODO: windows shell open
}
