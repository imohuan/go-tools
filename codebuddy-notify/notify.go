package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

const WxPusherSimplePushURL = "https://wxpusher.zjiecode.com/api/send/message/simple-push"

// WxPusherRequest 简单推送请求体
type WxPusherRequest struct {
	Content     string `json:"content"`
	Summary     string `json:"summary"`
	ContentType int    `json:"contentType"` // 1=文本 2=HTML 3=Markdown
	SPT         string `json:"spt"`
	URL         string `json:"url,omitempty"`
}

// WxPusherResponse 简单推送响应体
type WxPusherResponse struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg"`
	Success bool   `json:"success"`
}

// WxPusherNotifier WxPusher 通知器
type WxPusherNotifier struct {
	token     string
	client    *http.Client
	retryMax  int
}

func NewWxPusherNotifier(token string, retryMax int) *WxPusherNotifier {
	return &WxPusherNotifier{
		token:    token,
		retryMax: retryMax,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// NotifyCompleted 发送任务完成通知
func (n *WxPusherNotifier) NotifyCompleted(s SessionChange) {
	req := WxPusherRequest{
		Content:     getDisplayContent(s.SessionInfo),
		Summary:     "✅ " + getDisplayTitle(s.SessionInfo),
		ContentType: 2, // HTML
		SPT:         n.token,
	}
	n.sendWithRetry(req, "任务完成")
}

// NotifyFailed 发送任务失败通知
func (n *WxPusherNotifier) NotifyFailed(s SessionChange) {
	req := WxPusherRequest{
		Content:     getDisplayContent(s.SessionInfo),
		Summary:     "❌ " + getDisplayTitle(s.SessionInfo),
		ContentType: 2, // HTML
		SPT:         n.token,
	}
	n.sendWithRetry(req, "任务失败")
}

func (n *WxPusherNotifier) sendWithRetry(req WxPusherRequest, tag string) {
	for i := 0; i < n.retryMax; i++ {
		if i > 0 {
			time.Sleep(time.Duration(i) * time.Second)
		}

		resp, err := n.send(req)
		if err != nil {
			log.Printf("[%s] 第 %d 次发送失败: %v", tag, i+1, err)
			continue
		}

		if resp.Code == 1000 {
			log.Printf("[%s] 通知发送成功: %s", tag, req.Summary)
			return
		}

		log.Printf("[%s] 第 %d 次返回错误: code=%d msg=%s", tag, i+1, resp.Code, resp.Msg)
	}

	log.Printf("[%s] 通知发送全部失败 (%d次重试): %s", tag, n.retryMax, req.Summary)
}

func (n *WxPusherNotifier) send(req WxPusherRequest) (*WxPusherResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	httpReq, err := http.NewRequest("POST", WxPusherSimplePushURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	var result WxPusherResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	return &result, nil
}
