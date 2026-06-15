package main

import (
	"bufio"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var _ = embed.FS{}

//go:embed index.html
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
	Status        string `json:"status,omitempty"` // completed / working / pending / failed
	FilePath      string `json:"file_path,omitempty"` // 本地 JSONL 绝对路径
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
	dbPath      string
}

func NewServer(projectsDir, dbPath string) *Server {
	return &Server{projectsDir: projectsDir, dbPath: dbPath}
}

// projectList 仅返回项目目录列表（不含对话详情），用于快速初始化
func (s *Server) projectList(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(s.projectsDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	type ProjectBrief struct {
		Name      string `json:"name"`
		ConvCount int    `json:"conv_count"`
		Newest    int64  `json:"newest"` // 最新对话时间戳，用于排序
	}

	var projects []ProjectBrief
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		projectPath := filepath.Join(s.projectsDir, entry.Name())

		jsonlFiles, _ := filepath.Glob(filepath.Join(projectPath, "*.jsonl"))
		convCount := len(jsonlFiles)

		// 探最新对话时间戳（只读第一行的 timestamp 字段）
		var newest int64
		for _, f := range jsonlFiles {
			ts := readFirstLineTimestamp(f)
			if ts > newest {
				newest = ts
			}
		}

		projects = append(projects, ProjectBrief{
			Name:      entry.Name(),
			ConvCount: convCount,
			Newest:    newest,
		})
	}

	sort.Slice(projects, func(i, j int) bool {
		return projects[i].Newest > projects[j].Newest
	})

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: projects})
}

// readFirstLineTimestamp 读取 JSONL 第一行的 timestamp 字段
func readFirstLineTimestamp(filePath string) int64 {
	f, err := os.Open(filePath)
	if err != nil {
		return 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if scanner.Scan() {
		var firstMsg map[string]interface{}
		if json.Unmarshal(scanner.Bytes(), &firstMsg) == nil {
			if ts, ok := firstMsg["timestamp"].(float64); ok {
				return int64(ts)
			}
		}
	}
	return 0
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

// listConversations 列出项目下的对话，支持 ?since=<timestamp_ms> 增量刷新
func (s *Server) listConversations(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")

	projectPath := filepath.Join(s.projectsDir, projectName)
	if _, err := os.Stat(projectPath); os.IsNotExist(err) {
		writeJSON(w, http.StatusNotFound, APIResponse{Error: "项目不存在"})
		return
	}

	// 加载 DB 中的 session 信息（title + status）
	dbTitles := s.loadSessionTitles()

	// 增量刷新：只返回 since 之后更新的对话
	sinceStr := r.URL.Query().Get("since")
	var sinceTS int64
	if sinceStr != "" {
		sinceTS, _ = strconv.ParseInt(sinceStr, 10, 64)
	}

	files, err := filepath.Glob(filepath.Join(projectPath, "*.jsonl"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	var convs []ConvInfo
	var maxTS int64
	for _, f := range files {
		sessionID := strings.TrimSuffix(filepath.Base(f), ".jsonl")
		info := parseConvFile(f, sessionID, projectName)
		if info == nil {
			continue
		}

		// 用 DB 数据覆盖标题和状态
		if dbInfo, ok := dbTitles[sessionID]; ok {
			if dbInfo.title != "" {
				info.Title = dbInfo.title
			}
			if dbInfo.status != "" {
				info.Status = dbInfo.status
			}
		}

		// 增量模式：跳过旧对话。放宽 2 秒避免边界丢失
		if sinceTS > 0 && info.Timestamp <= sinceTS-2000 {
			continue
		}
		if info.Timestamp > maxTS {
			maxTS = info.Timestamp
		}
		convs = append(convs, *info)
	}

	sort.Slice(convs, func(i, j int) bool {
		return convs[i].Timestamp > convs[j].Timestamp
	})

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: map[string]interface{}{
		"conversations": convs,
		"total":         len(convs),
		"newest_ts":     maxTS,
	}})
}

type dbSessionInfo struct {
	title  string
	status string
}

// loadSessionTitles 从 workbuddy.db 加载所有 session 的标题和状态
func (s *Server) loadSessionTitles() map[string]dbSessionInfo {
	result := make(map[string]dbSessionInfo)
	if s.dbPath == "" {
		return result
	}

	db, err := sql.Open("sqlite", s.dbPath+"?mode=ro")
	if err != nil {
		return result
	}
	defer db.Close()

	rows, err := db.Query("SELECT id, title, status FROM sessions WHERE title IS NOT NULL AND title != ''")
	if err != nil {
		return result
	}
	defer rows.Close()

	for rows.Next() {
		var id, title, status string
		if err := rows.Scan(&id, &title, &status); err != nil {
			continue
		}
		result[id] = dbSessionInfo{title: title, status: status}
	}
	return result
}

// getConversation 获取对话详情，支持 ?since=<timestamp_ms> 仅返回新消息
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

	sinceStr := r.URL.Query().Get("since")
	var sinceTS int64
	var newestTS int64
	if sinceStr != "" {
		sinceTS, _ = strconv.ParseInt(sinceStr, 10, 64)
	}

	var messages []json.RawMessage
	for _, line := range lines {
		if line == "" {
			continue
		}

		// 始终追踪最新时间戳（增量模式下顺便做过滤）
		if sinceTS > 0 {
			var peek map[string]interface{}
			if json.Unmarshal([]byte(line), &peek) == nil {
				if ts, ok := peek["timestamp"].(float64); ok {
					ti := int64(ts)
					if ti > newestTS {
						newestTS = ti
					}
					if ti <= sinceTS {
						continue
					}
				}
			}
		} else {
			// 全量模式下也追踪最新时间戳
			var peek map[string]interface{}
			if json.Unmarshal([]byte(line), &peek) == nil {
				if ts, ok := peek["timestamp"].(float64); ok {
					ti := int64(ts)
					if ti > newestTS {
						newestTS = ti
					}
				}
			}
		}

		messages = append(messages, json.RawMessage(line))
	}

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: map[string]interface{}{
		"session_id": sessionID,
		"messages":   messages,
		"total":      len(messages),
		"newest_ts":  newestTS,
	}})
}

// getSubAgent 获取子代理详情
func (s *Server) getSubAgent(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	sessionID := r.PathValue("session")
	agentID := r.PathValue("agent")

	subDir := filepath.Join(s.projectsDir, projectName, sessionID, "subagents")

	// 尝试多种文件名模式
	candidates := []string{
		filepath.Join(subDir, agentID+".jsonl"),          // 精确匹配
		filepath.Join(subDir, "agent-"+agentID+".jsonl"),  // agent- 前缀
	}

	var filePath string
	for _, fp := range candidates {
		if _, err := os.Stat(fp); err == nil {
			filePath = fp
			break
		}
	}

	// 兜底：glob 模糊匹配（处理 sessionId 变化等情况）
	if filePath == "" {
		pattern := filepath.Join(subDir, "*"+agentID+"*.jsonl")
		matches, _ := filepath.Glob(pattern)
		if len(matches) > 0 {
			filePath = matches[0]
		}
	}

	if filePath == "" {
		log.Printf("[getSubAgent] NOT FOUND project=%s session=%s agent=%s dir=%s", projectName, sessionID, agentID, subDir)
		writeJSON(w, http.StatusNotFound, APIResponse{Error: fmt.Sprintf("子代理文件不存在: %s/%s", sessionID, agentID)})
		return
	}

	log.Printf("[getSubAgent] found: %s", filePath)

	data, err := os.ReadFile(filePath)
	if err != nil {
		writeJSON(w, http.StatusNotFound, APIResponse{Error: "子代理文件读取失败"})
		return
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")

	sinceStr := r.URL.Query().Get("since")
	var sinceTS int64
	var newestTS int64
	if sinceStr != "" {
		sinceTS, _ = strconv.ParseInt(sinceStr, 10, 64)
	}

	var messages []json.RawMessage
	for _, line := range lines {
		if line == "" {
			continue
		}

		// 始终追踪最新时间戳
		var peek map[string]interface{}
		if json.Unmarshal([]byte(line), &peek) == nil {
			if ts, ok := peek["timestamp"].(float64); ok {
				ti := int64(ts)
				if ti > newestTS {
					newestTS = ti
				}
			}
		}

		// since 过滤
		if sinceTS > 0 {
			var peek2 map[string]interface{}
			if json.Unmarshal([]byte(line), &peek2) == nil {
				if ts, ok := peek2["timestamp"].(float64); ok {
					if int64(ts) <= sinceTS {
						continue
					}
				}
			}
		}

		messages = append(messages, json.RawMessage(line))
	}

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: map[string]interface{}{
		"session_id": agentID,
		"messages":   messages,
		"total":      len(messages),
		"newest_ts":  newestTS,
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
	mux.HandleFunc("GET /api/project-list", s.projectList)                     // 仅项目目录（快速）
	mux.HandleFunc("GET /api/all-conversations", s.allConversations)           // 一次性返回所有（兼容旧版）
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
		FilePath:      filePath,
	}
}

func extractText(content interface{}) string {
	switch v := content.(type) {
	case string:
		// 优先提取 <user_query> 标签中的实际用户输入
		qmRe := regexp.MustCompile(`(?s)<user_query>\s*(.*?)\s*</user_query>`)
		if m := qmRe.FindStringSubmatch(v); m != nil && strings.TrimSpace(m[1]) != "" {
			cleaned := strings.TrimSpace(m[1])
			if len(cleaned) > 100 {
				cleaned = cleaned[:100] + "..."
			}
			return cleaned
		}
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
		// 数组形式：先拼接所有 text 字段，再用跟 string 一样的清洗逻辑
		var texts []string
		for _, block := range v {
			if b, ok := block.(map[string]interface{}); ok {
				if t, ok := b["text"].(string); ok {
					texts = append(texts, t)
				}
			}
		}
		result := strings.Join(texts, "\n")
		// 优先提取 <user_query>
		qmRe := regexp.MustCompile(`(?s)<user_query>\s*(.*?)\s*</user_query>`)
		if m := qmRe.FindStringSubmatch(result); m != nil && strings.TrimSpace(m[1]) != "" {
			cleaned := strings.TrimSpace(m[1])
			if len(cleaned) > 100 {
				cleaned = cleaned[:100] + "..."
			}
			return cleaned
		}
		// 剥离 system-reminder
		sysRe := regexp.MustCompile(`(?s)<system-reminder[^>]*>.*?</system-reminder>`)
		cleaned := sysRe.ReplaceAllString(result, "")
		tagRe := regexp.MustCompile(`<[^>]+>`)
		cleaned = tagRe.ReplaceAllString(cleaned, " ")
		spaceRe := regexp.MustCompile(`\s+`)
		cleaned = spaceRe.ReplaceAllString(cleaned, " ")
		cleaned = strings.TrimSpace(cleaned)
		if len(cleaned) > 100 {
			cleaned = cleaned[:100] + "..."
		}
		return cleaned
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

func StartServer(port int, projectsDir, dbPath string) {
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

	server := NewServer(projectsDir, dbPath)
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
	var cmd string
	var args []string
	switch {
	case strings.Contains(runtime.GOOS, "windows"):
		cmd = "cmd"
		args = []string{"/c", "start", url}
	case runtime.GOOS == "darwin":
		cmd = "open"
		args = []string{url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}
	return exec.Command(cmd, args...).Start()
}
