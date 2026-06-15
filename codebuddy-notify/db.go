package main

import (
	"database/sql"
	"fmt"
	"log"
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
					prevStatus, s.Status, getDisplayTitle(s))
				changes = append(changes, change)
			} else {
				log.Printf("状态变更(不通知): %s → %s [%s]",
					prevStatus, s.Status, getDisplayTitle(s))
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
					s.Status, getDisplayTitle(s),
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

func getDisplayTitle(s SessionInfo) string {
	if s.CustomTitle != "" {
		return s.CustomTitle
	}
	t := s.Title
	if len(t) > 60 {
		t = t[:60] + "..."
	}
	return t
}

// getDisplayContent 生成通知内容
func getDisplayContent(s SessionInfo) string {
	title := getDisplayTitle(s)

	switch s.Status {
	case "completed":
		return fmt.Sprintf(
			"<h2 style=\"color:#07c160;\">✅ 任务完成</h2>"+
				"<p><b>任务：</b>%s</p>"+
				"<p><b>完成时间：</b>%s</p>"+
				"<p><b>耗时：</b>%s</p>"+
				"<p><b>工作目录：</b>%s</p>"+
				"<p style=\"color:#888;font-size:12px;\">模型: %s | 会话: %s</p>",
			title,
			s.UpdatedAt.Format("2006-01-02 15:04:05"),
			formatDuration(s.UpdatedAt.Sub(s.CreatedAt)),
			s.CWD,
			s.Model,
			s.ID[:8],
		)
	case "failed", "error":
		return fmt.Sprintf(
			"<h2 style=\"color:#fa5151;\">❌ 任务失败</h2>"+
				"<p><b>任务：</b>%s</p>"+
				"<p><b>失败时间：</b>%s</p>"+
				"<p><b>工作目录：</b>%s</p>"+
				"<p style=\"color:#888;font-size:12px;\">模型: %s | 会话: %s</p>",
			title,
			s.UpdatedAt.Format("2006-01-02 15:04:05"),
			s.CWD,
			s.Model,
			s.ID[:8],
		)
	default:
		return fmt.Sprintf("<p>任务 <b>%s</b> 状态: %s</p>", title, s.Status)
	}
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
