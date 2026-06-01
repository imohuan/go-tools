package main

import (
	"os"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestXXSYChapterOfflineExtract(t *testing.T) {
	filePath := strings.TrimSpace(os.Getenv("TEST_XXSY_HTML_PATH"))
	if filePath == "" {
		filePath = "D:/CodeX/go-legado-demo/debug_logs/20260528/195647_source6_潇湘书院_chapter_raw.html"
	}
	rule := "id.content@p@text||id.content@html"

	raw, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read html failed: %v", err)
	}
	html := string(raw)

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parse html failed: %v", err)
	}

	pCount := doc.Find("#content p").Length()
	mainText := strings.TrimSpace(doc.Find("#content").Text())
	ruleOutput := strings.TrimSpace(extractByRule(doc.Selection, rule, "https://www.xxsy.net/"))

	t.Logf("p_count=%d", pCount)
	t.Logf("main_text_len=%d", len(mainText))
	t.Logf("rule_output_len=%d", len(ruleOutput))
	t.Logf("has_subscribe_gate=%v", strings.Contains(html, "订阅本章"))
	t.Logf("has_next_chapter_btn=%v", strings.Contains(html, "下一章"))
	t.Logf("rule_output_preview=%s", cutRune(ruleOutput, 220))

	if pCount == 0 {
		t.Fatalf("no chapter paragraphs found in #content")
	}
	if ruleOutput == "" {
		t.Fatalf("rule extracted empty content")
	}
	if ruleOutput != mainText {
		t.Fatalf("rule output != #content text")
	}
}

func cutRune(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
