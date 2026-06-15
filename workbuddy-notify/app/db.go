package main

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// SessionInfo 会话信息
type SessionInfo struct {
	ID             string
	Title          string
	CustomTitle    string
	Status         string
	CWD            string
	ExpertID       string
	Model          string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	LastActivityAt time.Time // 可能为零值（对应 NULL）
}

// SessionChange 状态变更
type SessionChange struct {
	SessionInfo
	PreviousStatus string
}

// SessionMonitor 会话状态监听器
type SessionMonitor struct {
	db     *sql.DB
	mu     sync.Mutex
	states map[string]string // sessionID -> status
}

func OpenSQLite(path string) (*sql.DB, error) {
	// modernc.org/sqlite uses file: URI scheme for read-only with WAL support
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&mode=ro", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err = db.Ping(); err != nil {
		return nil, err
	}
	return db, nil
}

func NewSessionMonitor(db *sql.DB) *SessionMonitor {
	return &SessionMonitor{
		db:     db,
		states: make(map[string]string),
	}
}

// Refresh 刷新状态，返回所有 working → completed/failed 的变更
func (m *SessionMonitor) Refresh() []SessionChange {
	m.mu.Lock()
	defer m.mu.Unlock()

	rows, err := m.db.Query(`
		SELECT id, title, custom_title, status, cwd, expert_id, model,
			   created_at, updated_at, last_activity_at
		FROM sessions
		WHERE deleted_at IS NULL
		ORDER BY updated_at DESC
	`)
	if err != nil {
		log.Printf("查询 sessions 失败: %v", err)
		return nil
	}
	defer rows.Close()

	var changes []SessionChange
	seen := make(map[string]bool) // 记录本轮查询到的 ID，用于清理已删除的 session

	for rows.Next() {
		var s SessionInfo
		var createdAt, updatedAt, lastActivityAt sql.NullInt64
		var title, customTitle, status, cwd, expertID, model sql.NullString

		err := rows.Scan(&s.ID, &title, &customTitle, &status, &cwd,
			&expertID, &model, &createdAt, &updatedAt, &lastActivityAt)
		if err != nil {
			log.Printf("扫描行失败 (session=%s): %v", s.ID, err)
			continue
		}

		s.Title = title.String
		s.CustomTitle = customTitle.String
		s.Status = status.String
		s.CWD = cwd.String
		s.ExpertID = expertID.String
		s.Model = model.String
		if createdAt.Valid {
			s.CreatedAt = time.UnixMilli(createdAt.Int64)
		}
		if updatedAt.Valid {
			s.UpdatedAt = time.UnixMilli(updatedAt.Int64)
		}
		if lastActivityAt.Valid {
			s.LastActivityAt = time.UnixMilli(lastActivityAt.Int64)
		}

		seen[s.ID] = true
		prevStatus, existed := m.states[s.ID]

		if existed && prevStatus != s.Status {
			// 状态变化
			change := SessionChange{
				SessionInfo:    s,
				PreviousStatus: prevStatus,
			}
			m.states[s.ID] = s.Status

			// 只要变成 completed / failed / error 就通知
			if (s.Status == "completed" || s.Status == "failed" || s.Status == "error") {
				log.Printf("状态变更: %s → %s [%s]",
					prevStatus, s.Status, getDisplayTitle(s, ""))
				changes = append(changes, change)
			} else {
				log.Printf("状态变更(不通知): %s → %s [%s]",
					prevStatus, s.Status, getDisplayTitle(s, ""))
			}
		} else {
			// 新增的 session（或者是轮询中间产生的 session）
			m.states[s.ID] = s.Status

			// 如果新增时已经是完成/失败状态，且 created_at == updated_at，
			// 说明它可能在两次轮询之间跑完了，也通知一下
			if (s.Status == "completed" || s.Status == "failed" || s.Status == "error") &&
				!existed &&
				!s.CreatedAt.IsZero() && !s.UpdatedAt.IsZero() &&
				s.UpdatedAt.Sub(s.CreatedAt) > time.Second {
				// 只通知那些确实花了一段时间的任务（排除瞬间完成的，可能是旧数据）
				change := SessionChange{
					SessionInfo:    s,
					PreviousStatus: "working", // 推测
				}
				log.Printf("新增已完成: %s [%s] (耗时 %s)",
					s.Status, getDisplayTitle(s, ""),
					formatDuration(s.UpdatedAt.Sub(s.CreatedAt)))
				changes = append(changes, change)
			}
		}
	}

	// 清理已经从数据库消失的 session（被删除了）
	for id := range m.states {
		if !seen[id] {
			delete(m.states, id)
		}
	}

	return changes
}

func getDisplayTitle(s SessionInfo, jsonlPath string) string {
	// 优先使用 JSONL 中最后一条 user 消息作为标题
	if userText := extractLastUserText(jsonlPath, 80); userText != "" {
		// 取第一行（去掉多行内容）
		idx := strings.IndexByte(userText, '\n')
		if idx > 0 {
			userText = userText[:idx]
		}
		return userText
	}
	if s.CustomTitle != "" {
		return s.CustomTitle
	}
	t := s.Title
	if len(t) > 60 {
		t = t[:60] + "..."
	}
	return t
}

// getDisplayContent 生成通知内容（现代化卡片设计）
func getDisplayContent(s SessionInfo) string {
	jsonlPath := findSessionJSONL(s.ID)
	title := getDisplayTitle(s, jsonlPath)
	summary := extractLastAssistantText(jsonlPath, 300)

	// HTML 转义
	escTitle := htmlEscape(title)
	escCWD := htmlEscape(s.CWD)
	escModel := htmlEscape(s.Model)
	escSummary := htmlEscape(summary)

	var statusColor, statusGradient, statusLabel string
	switch s.Status {
	case "completed":
		statusColor = "#059669"
		statusGradient = "linear-gradient(135deg, #059669, #10b981)"
		statusLabel = "任务完成"
	case "failed", "error":
		statusColor = "#dc2626"
		statusGradient = "linear-gradient(135deg, #dc2626, #ef4444)"
		statusLabel = "任务失败"
	default:
		statusColor = "#6366f1"
		statusGradient = "linear-gradient(135deg, #4f46e5, #6366f1)"
		statusLabel = "状态更新"
	}

	cardCSS := `max-width:480px;border-radius:14px;overflow:hidden;box-shadow:0 4px 24px rgba(0,0,0,0.06),0 1px 4px rgba(0,0,0,0.04);font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC","Microsoft YaHei","Helvetica Neue",sans-serif;color:#1e293b;line-height:1.6;padding:0;margin:0;`

	// 摘要区块
	var summaryBlock string
	if escSummary != "" {
		summaryBlock = fmt.Sprintf(
			`<div style="margin-top:12px;padding:14px 16px;background:#f1f5f9;border-left:4px solid %s;border-radius:0 8px 8px 0;font-size:13px;color:#475569;line-height:1.6;white-space:pre-wrap;word-break:break-word;">%s</div>`,
			statusColor, escSummary,
		)
	}

	// 信息行模板
	row := func(label, value string) string {
		return fmt.Sprintf(
			`<div style="display:flex;align-items:flex-start;gap:10px;padding:10px 0;">`+
				`<span style="color:#94a3b8;font-size:14px;flex-shrink:0;min-width:48px;">%s</span>`+
				`<span style="font-size:14px;word-break:break-all;">%s</span>`+
				`</div>`,
			label, value,
		)
	}

	timeStr := s.UpdatedAt.Format("2006-01-02 15:04:05")
	durationStr := formatDuration(s.UpdatedAt.Sub(s.CreatedAt))
	sessionShort := s.ID
	if len(sessionShort) > 8 {
		sessionShort = sessionShort[:8]
	}

	return fmt.Sprintf(
		`<div style="`+cardCSS+`">`+
			// Header
			`<div style="background:`+statusGradient+`;padding:20px 22px;">`+
			`<div style="display:flex;align-items:center;gap:12px;">`+
			`<div style="width:36px;height:36px;border-radius:50%%;background:rgba(255,255,255,0.18);display:flex;align-items:center;justify-content:center;flex-shrink:0;">`+
			`<span style="color:#fff;font-size:20px;">`+statusIconChar(s.Status)+`</span>`+
			`</div>`+
			`<div style="flex:1;min-width:0;">`+
			`<div style="color:#fff;font-size:17px;font-weight:700;letter-spacing:-0.01em;">%s</div>`+
			`<div style="color:rgba(255,255,255,0.7);font-size:12px;margin-top:2px;">%s</div>`+
			`</div>`+
			`</div>`+
			`</div>`+
			// Body
			`<div style="background:#fff;padding:16px 22px;">`+
			`<div style="font-size:16px;font-weight:600;color:#0f172a;padding:8px 0 12px;border-bottom:1px solid #f1f5f9;word-break:break-all;line-height:1.5;">%s</div>`+
			row("耗时", durationStr)+
			`<div style="height:1px;background:#f1f5f9;margin:0 -6px;"></div>`+
			row("目录", `<span style="font-family:'JetBrains Mono','SF Mono','Menlo',monospace;font-size:12px;color:#64748b;">`+escCWD+`</span>`)+
			`%s`+
			`</div>`+
			// Footer
			`<div style="background:#f8fafc;padding:10px 22px;display:flex;justify-content:space-between;align-items:center;border-top:1px solid #f1f5f9;">`+
			`<span style="color:#94a3b8;font-size:11px;">`+escModel+`</span>`+
			`<span style="color:#cbd5e1;font-size:11px;font-family:'JetBrains Mono','SF Mono','Menlo',monospace;">`+sessionShort+`</span>`+
			`</div>`+
			`</div>`,
		statusLabel, timeStr, escTitle, summaryBlock,
	)
}

// statusIconChar 返回状态图标字符
func statusIconChar(status string) string {
	switch status {
	case "completed":
		return "✓"
	case "failed", "error":
		return "✕"
	default:
		return "i"
	}
}

// htmlEscape 简单的 HTML 转义
func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0f秒", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.0f分%.0f秒", d.Minutes(), d.Seconds()-60*d.Minutes())
	}
	return fmt.Sprintf("%.0f时%.0f分", d.Hours(), d.Minutes()-60*d.Hours())
}
