package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSingleSourceFlowByFunctions(t *testing.T) {
	configPath := envOr("TEST_CONFIG_PATH", "D:/CodeX/go-legado-demo/data-all.json")
	sourceName := envOr("TEST_SOURCE_NAME", "武芊漫画")
	keyword := envOr("TEST_KEYWORD", "姐")

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config failed: %v", err)
	}
	var list []Source
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("parse config failed: %v", err)
	}

	var src *Source
	for i := range list {
		if strings.TrimSpace(list[i].BookSourceName) == sourceName {
			src = &list[i]
			break
		}
	}
	if src == nil {
		t.Fatalf("source not found by name: %s", sourceName)
	}

	now := time.Now()
	outDir := filepath.Join("debug_logs", now.Format("20060102"), "single_source_func_"+now.Format("150405"))
	_ = os.MkdirAll(outDir, 0o755)
	t.Logf("debug dir: %s", outDir)

	app := &App{client: httpClientForTest()}

	searchURL := strings.ReplaceAll(src.SearchURL, "{{key}}", url.QueryEscape(keyword))
	searchURL = strings.ReplaceAll(searchURL, "{{page}}", "1")
	searchRaw, err := app.fetchText(searchURL)
	if err != nil {
		t.Fatalf("fetch search failed: %v", err)
	}
	mustWriteFile(t, filepath.Join(outDir, "01_search_raw.txt"), searchRaw)

	items, parser := parseSearchByRule(*src, searchRaw, src.BookSourceURL)
	mustWriteJSONFile(t, filepath.Join(outDir, "01_search_items.json"), items)
	t.Logf("search parser=%s resultCount=%d", parser, len(items))
	if len(items) == 0 {
		t.Fatalf("search empty")
	}

	firstURL, _ := items[0]["url"].(string)
	firstName, _ := items[0]["name"].(string)
	t.Logf("first item name=%s url=%s", firstName, firstURL)
	if strings.TrimSpace(firstURL) == "" {
		t.Fatalf("first url empty")
	}

	detailRaw, err := app.fetchTextWithRule(firstURL)
	if err != nil {
		t.Fatalf("fetch detail failed: %v", err)
	}
	mustWriteFile(t, filepath.Join(outDir, "02_detail_raw.txt"), detailRaw)
	book := parseDetailByRule(*src, detailRaw, firstURL)
	mustWriteJSONFile(t, filepath.Join(outDir, "02_detail_parsed.json"), book)

	tocURL, _ := book["tocUrl"].(string)
	if strings.TrimSpace(tocURL) == "" && strings.TrimSpace(src.RuleBookInfo.TocURL) == "" && isLikelyJSONRule(src.RuleToc.ChapterList) {
		tocURL = firstURL
		book["tocUrl"] = tocURL
		mustWriteJSONFile(t, filepath.Join(outDir, "02_detail_parsed.json"), book)
	}
	t.Logf("book name=%v tocUrl=%s", book["name"], tocURL)
	if strings.TrimSpace(tocURL) == "" {
		t.Fatalf("tocUrl empty")
	}

	tocRaw, err := app.fetchTextWithRule(tocURL)
	if err != nil {
		t.Fatalf("fetch toc failed: %v", err)
	}
	mustWriteFile(t, filepath.Join(outDir, "03_toc_raw.txt"), tocRaw)
	vars := map[string]string{}
	if v, ok := book["comic_id"].(string); ok {
		vars["comic_id"] = v
	}
	chapters := parseChaptersByRuleWithVars(*src, tocRaw, tocURL, vars)
	mustWriteJSONFile(t, filepath.Join(outDir, "03_toc_chapters.json"), chapters)
	t.Logf("chapters count=%d", len(chapters))
	if len(chapters) == 0 {
		t.Fatalf("chapters empty")
	}

	ch1URL, _ := chapters[0]["url"].(string)
	ch1Name, _ := chapters[0]["name"].(string)
	t.Logf("first chapter name=%s url=%s", ch1Name, ch1URL)
	if strings.TrimSpace(ch1URL) == "" {
		t.Fatalf("first chapter url empty")
	}

	chRaw, err := app.fetchTextWithRule(ch1URL)
	if err != nil {
		t.Fatalf("fetch chapter failed: %v", err)
	}
	mustWriteFile(t, filepath.Join(outDir, "04_chapter_raw.txt"), chRaw)
	content := normalizeChapterContent(*src, extractByRuleWithPostProcess(chRaw, src.RuleContent.Content, ch1URL))
	mustWriteJSONFile(t, filepath.Join(outDir, "04_chapter_content.json"), map[string]any{
		"name":    ch1Name,
		"url":     ch1URL,
		"length":  len(content),
		"preview": safeCut(content, 200),
		"content": content,
	})
	t.Logf("chapter content length=%d preview=%s", len(content), safeCut(content, 120))
	if strings.TrimSpace(content) == "" {
		t.Fatalf("chapter content empty")
	}
}

func envOr(key, def string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return v
}

func httpClientForTest() *http.Client {
	return &http.Client{Timeout: 20 * time.Second}
}

func mustWriteFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatalf("write file failed %s: %v", p, err)
	}
}

func mustWriteJSONFile(t *testing.T, p string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal json failed %s: %v", p, err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatalf("write json failed %s: %v", p, err)
	}
}

func safeCut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
