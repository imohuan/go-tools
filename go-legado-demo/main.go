package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/antchfx/htmlquery"
	"github.com/dop251/goja"
	"github.com/tidwall/gjson"
	"golang.org/x/net/html/charset"
)

type Source struct {
	BookSourceName string `json:"bookSourceName"`
	BookSourceURL  string `json:"bookSourceUrl"`
	BookSourceType int    `json:"bookSourceType"`
	SearchURL      string `json:"searchUrl"`
	RuleSearch     struct {
		BookList string `json:"bookList"`
		Name     string `json:"name"`
		Author   string `json:"author"`
		Intro    string `json:"intro"`
		BookURL  string `json:"bookUrl"`
	} `json:"ruleSearch"`
	RuleBookInfo struct {
		Name   string `json:"name"`
		Author string `json:"author"`
		Intro  string `json:"intro"`
		Cover  string `json:"coverUrl"`
		TocURL string `json:"tocUrl"`
	} `json:"ruleBookInfo"`
	RuleToc struct {
		ChapterList string `json:"chapterList"`
		ChapterName string `json:"chapterName"`
		ChapterURL  string `json:"chapterUrl"`
	} `json:"ruleToc"`
	RuleContent struct {
		Content string `json:"content"`
	} `json:"ruleContent"`
}

type App struct {
	sources []Source
	client  *http.Client
}

func main() {
	app, err := newApp()
	if err != nil {
		log.Fatalf("初始化失败: %v", err)
	}
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir("./web")))
	mux.HandleFunc("/api/sources", app.handleSources)
	mux.HandleFunc("/api/search", app.handleSearch)
	mux.HandleFunc("/api/book", app.handleBook)
	mux.HandleFunc("/api/chapter", app.handleChapter)
	log.Println("服务已启动: http://localhost:8080")
	log.Println("按 Ctrl+C 停止")
	log.Fatal(http.ListenAndServe(":8080", withCORS(mux)))
}

func newApp() (*App, error) {
	sourcePath := filepath.Join("data-all.json")
	if _, err := os.Stat(sourcePath); err != nil {
		return nil, fmt.Errorf("未找到书源文件: %s", sourcePath)
	}
	b, err := os.ReadFile(sourcePath)
	if err != nil {
		return nil, err
	}
	var list []Source
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, err
	}
	valid := make([]Source, 0, len(list))
	for _, s := range list {
		if strings.TrimSpace(s.BookSourceName) == "" || strings.TrimSpace(s.SearchURL) == "" {
			continue
		}
		valid = append(valid, s)
	}
	if len(valid) == 0 {
		return nil, fmt.Errorf("没有可用书源")
	}
	return &App{sources: valid, client: &http.Client{Timeout: 20 * time.Second}}, nil
}

func (a *App) getSourceByQuery(r *http.Request) (Source, int, error) {
	idxStr := strings.TrimSpace(r.URL.Query().Get("sourceIndex"))
	if idxStr == "" {
		idxStr = "0"
	}
	idx, err := strconv.Atoi(idxStr)
	if err != nil || idx < 0 || idx >= len(a.sources) {
		return Source{}, -1, fmt.Errorf("sourceIndex 无效")
	}
	return a.sources[idx], idx, nil
}

func (a *App) handleSources(w http.ResponseWriter, r *http.Request) {
	items := make([]map[string]any, 0, len(a.sources))
	for i, s := range a.sources {
		items = append(items, map[string]any{"index": i, "name": s.BookSourceName, "url": s.BookSourceURL})
	}
	writeJSON(w, 200, map[string]any{"list": items})
}

func (a *App) handleSearch(w http.ResponseWriter, r *http.Request) {
	src, idx, err := a.getSourceByQuery(r)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	key := strings.TrimSpace(r.URL.Query().Get("key"))
	debug := r.URL.Query().Get("debug") == "1"
	if key == "" {
		writeErr(w, 400, "key 不能为空")
		return
	}
	u := strings.ReplaceAll(src.SearchURL, "{{key}}", url.QueryEscape(key))
	u = strings.ReplaceAll(u, "{{page}}", "1")
	log.Printf("[search] sourceIndex=%d source=%s key=%q url=%s", idx, src.BookSourceName, key, u)
	html, err := a.fetchTextWithRule(u)
	if err != nil {
		log.Printf("[search] fetch error: %v", err)
		writeErr(w, 502, err.Error())
		return
	}
	items, parser := parseSearchByRule(src, html, src.BookSourceURL)
	log.Printf("[search] parser=%s htmlLen=%d resultCount=%d", parser, len(html), len(items))
	saveSearchDebug(idx, src, key, u, parser, html, items)
	for _, it := range items {
		it["sourceIndex"] = idx
	}
	resp := map[string]any{"list": items}
	if debug {
		resp["debug"] = map[string]any{
			"sourceIndex": idx,
			"sourceName":  src.BookSourceName,
			"sourceUrl":   src.BookSourceURL,
			"searchUrl":   u,
			"parser":      parser,
			"htmlLen":     len(html),
			"resultCount": len(items),
			"htmlPreview": safePreview(html, 400),
		}
	}
	writeJSON(w, 200, resp)
}

func (a *App) handleBook(w http.ResponseWriter, r *http.Request) {
	src, idx, err := a.getSourceByQuery(r)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	bookURL := strings.TrimSpace(r.URL.Query().Get("url"))
	if bookURL == "" {
		writeErr(w, 400, "url 不能为空")
		return
	}
	html, err := a.fetchTextWithRule(bookURL)
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	book := parseDetailByRule(src, html, bookURL)
	// 搜索专用 API 常见：详情接口只返回目录，名称/作者/简介需要沿用搜索结果
	if strings.TrimSpace(anyToString(book["name"])) == "" {
		book["name"] = strings.TrimSpace(r.URL.Query().Get("name"))
	}
	if strings.TrimSpace(anyToString(book["author"])) == "" {
		book["author"] = strings.TrimSpace(r.URL.Query().Get("author"))
	}
	if strings.TrimSpace(anyToString(book["intro"])) == "" {
		book["intro"] = strings.TrimSpace(r.URL.Query().Get("intro"))
	}
	tocURL, _ := book["tocUrl"].(string)
	// 搜索专用 API 源常见没有 ruleBookInfo，bookURL 本身就是目录接口
	if strings.TrimSpace(tocURL) == "" && strings.TrimSpace(src.RuleBookInfo.TocURL) == "" {
		if isLikelyJSONRule(src.RuleToc.ChapterList) {
			tocURL = bookURL
			book["tocUrl"] = tocURL
		}
	}
	if strings.TrimSpace(tocURL) != "" {
		if tocHTML, err := a.fetchTextWithRule(tocURL); err == nil {
			vars := map[string]string{}
			if v, ok := book["comic_id"].(string); ok {
				vars["comic_id"] = v
			}
			book["chapters"] = parseChaptersByRuleWithVars(src, tocHTML, tocURL, vars)
		} else {
			book["chapters"] = []map[string]any{}
			book["tocError"] = err.Error()
		}
	} else {
		book["chapters"] = []map[string]any{}
	}
	book["sourceIndex"] = idx
	saveDetailDebug(idx, src, bookURL, html, book)
	writeJSON(w, 200, book)
}

func (a *App) handleChapter(w http.ResponseWriter, r *http.Request) {
	src, idx, err := a.getSourceByQuery(r)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	chURL := strings.TrimSpace(r.URL.Query().Get("url"))
	if chURL == "" {
		writeErr(w, 400, "url 不能为空")
		return
	}
	html, err := a.fetchTextWithRule(chURL)
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	content := extractByRuleWithPostProcess(html, src.RuleContent.Content, chURL)
	content = normalizeChapterContent(src, content)
	if strings.TrimSpace(content) == "" {
		content = strings.TrimSpace(extractChapterFallback(html))
	}
	resp := map[string]any{"content": strings.TrimSpace(content)}
	saveChapterDebug(idx, src, chURL, html, resp)
	writeJSON(w, 200, resp)
}

func normalizeChapterContent(src Source, content string) string {
	if src.BookSourceType == 2 {
		// 漫画源正文通常是 <img> 列表，不能走文本清洗，否则会被清空
		return strings.TrimSpace(content)
	}
	return cleanHTML(content)
}

func anyToString(v any) string {
	s, _ := v.(string)
	return s
}

func extractChapterFallback(html string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return ""
	}
	candidates := []string{
		".content p",
		".content",
		"#chapterContent p",
		"#chapterContent",
		".chapter-content p",
		".chapter-content",
		"article p",
		"article",
	}
	for _, css := range candidates {
		sel := doc.Find(css)
		if sel.Length() == 0 {
			continue
		}
		text := cleanHTML(sel.Text())
		if strings.TrimSpace(text) != "" {
			return text
		}
	}
	return ""
}

func (a *App) fetchText(u string) (string, error) {
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("上游错误 %d: %s", resp.StatusCode, string(b))
	}
	b, _ := io.ReadAll(resp.Body)
	log.Printf("[http] GET %s -> %d bytes=%d", u, resp.StatusCode, len(b))
	return decodeBody(resp.Header.Get("Content-Type"), b), nil
}

type ruleRequest struct {
	URL     string
	Method  string
	Body    string
	Headers map[string]string
}

func parseRuleRequest(raw string) ruleRequest {
	raw = strings.TrimSpace(raw)
	req := ruleRequest{URL: raw, Method: http.MethodGet, Headers: map[string]string{}}
	if !strings.Contains(raw, ",{") {
		return req
	}
	i := strings.Index(raw, ",{")
	if i <= 0 {
		return req
	}
	urlPart := strings.TrimSpace(raw[:i])
	cfgPart := strings.TrimSpace(raw[i+1:])
	req.URL = urlPart
	var cfg struct {
		Method  string            `json:"method"`
		Body    string            `json:"body"`
		Headers map[string]string `json:"headers"`
	}
	if err := json.Unmarshal([]byte(cfgPart), &cfg); err != nil {
		return req
	}
	if strings.TrimSpace(cfg.Method) != "" {
		req.Method = strings.ToUpper(strings.TrimSpace(cfg.Method))
	}
	req.Body = cfg.Body
	if cfg.Headers != nil {
		req.Headers = cfg.Headers
	}
	return req
}

func (a *App) fetchTextWithRule(raw string) (string, error) {
	reqInfo := parseRuleRequest(raw)
	var bodyReader io.Reader
	if reqInfo.Method == http.MethodPost {
		bodyReader = bytes.NewBufferString(reqInfo.Body)
	}
	req, _ := http.NewRequest(reqInfo.Method, reqInfo.URL, bodyReader)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	if reqInfo.Method == http.MethodPost && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range reqInfo.Headers {
		req.Header.Set(k, v)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("上游错误 %d: %s", resp.StatusCode, string(b))
	}
	b, _ := io.ReadAll(resp.Body)
	log.Printf("[http] %s %s -> %d bytes=%d", reqInfo.Method, reqInfo.URL, resp.StatusCode, len(b))
	return decodeBody(resp.Header.Get("Content-Type"), b), nil
}

func decodeBody(contentType string, body []byte) string {
	if len(body) == 0 {
		return ""
	}
	reader, err := charset.NewReader(bytes.NewReader(body), contentType)
	if err != nil {
		return string(body)
	}
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return string(body)
	}
	return string(decoded)
}

func parseDetailByRule(src Source, html, detailURL string) map[string]any {
	name := cleanHTML(extractByRuleFromHTML(html, src.RuleBookInfo.Name, detailURL))
	if strings.TrimSpace(name) == "" && strings.Contains(html, "\"title\"") {
		name = cleanHTML(extractJSONRule(html, "$..title"))
		if strings.Contains(name, ",") {
			name = strings.Split(name, ",")[0]
		}
	}
	author := cleanHTML(extractByRuleFromHTML(html, src.RuleBookInfo.Author, detailURL))
	intro := cleanHTML(extractByRuleFromHTML(html, src.RuleBookInfo.Intro, detailURL))
	cover := absURL(detailURL, extractByRuleFromHTML(html, src.RuleBookInfo.Cover, detailURL))
	tocURL := absURL(detailURL, extractByRuleFromHTML(html, src.RuleBookInfo.TocURL, detailURL))
	book := map[string]any{"name": name, "author": author, "intro": intro, "cover": cover, "tocUrl": tocURL}
	// JSON 书源常见：详情页里的 comic_id 需要传给 chapterUrl 的 @get:{comic_id}
	if strings.Contains(src.RuleToc.ChapterURL, "@get:{comic_id}") {
		if id := extractJSONRule(html, "$..comic_id"); strings.TrimSpace(id) != "" {
			book["comic_id"] = strings.TrimSpace(strings.Split(id, ",")[0])
		}
	}
	return book
}

func parseSearchByRule(src Source, html, base string) ([]map[string]any, string) {
	if isLikelyJSONRule(src.RuleSearch.BookList) {
		return parseSearchByJSONRule(src, html, base)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return []map[string]any{}, "none"
	}
	itemsSel := selectNodes(doc.Selection, src.RuleSearch.BookList)
	if itemsSel.Length() == 0 {
		return []map[string]any{}, "none"
	}
	out := make([]map[string]any, 0, itemsSel.Length())
	itemsSel.Each(func(_ int, s *goquery.Selection) {
		name := cleanHTML(extractByRule(s, src.RuleSearch.Name, base))
		link := absURL(base, extractByRule(s, src.RuleSearch.BookURL, base))
		if name == "" || link == "" {
			return
		}
		out = append(out, map[string]any{
			"name":   name,
			"author": cleanHTML(extractByRule(s, src.RuleSearch.Author, base)),
			"intro":  cleanHTML(extractByRule(s, src.RuleSearch.Intro, base)),
			"url":    link,
		})
	})
	return out, "rule"
}

func parseSearchByJSONRule(src Source, body, base string) ([]map[string]any, string) {
	items := getJSONResultsByRulePath(body, strings.TrimSpace(src.RuleSearch.BookList))
	if len(items) == 0 {
		return []map[string]any{}, "none"
	}
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		itemRaw := it.Raw
		name := cleanHTML(extractJSONRule(itemRaw, src.RuleSearch.Name))
		if name == "" {
			continue
		}
		rawURL := renderTemplateWithJSON(src.RuleSearch.BookURL, itemRaw)
		rawURL = renderSingleBraceTemplateWithJSON(rawURL, itemRaw)
		link := absURL(base, rawURL)
		if link == "" {
			continue
		}
		out = append(out, map[string]any{
			"name":   name,
			"author": cleanHTML(extractJSONRule(itemRaw, src.RuleSearch.Author)),
			"intro":  cleanHTML(extractJSONRule(itemRaw, src.RuleSearch.Intro)),
			"url":    link,
		})
	}
	return out, "rule"
}

func parseChaptersByRule(src Source, html, base string) []map[string]any {
	return parseChaptersByRuleWithVars(src, html, base, map[string]string{})
}

func parseChaptersByRuleWithVars(src Source, html, base string, vars map[string]string) []map[string]any {
	if isLikelyJSONRule(src.RuleToc.ChapterList) {
		items := getJSONResultsByRulePath(html, strings.TrimSpace(src.RuleToc.ChapterList))
		out := make([]map[string]any, 0, len(items))
		seen := map[string]bool{}
		comicID := strings.TrimSpace(vars["comic_id"])
		if comicID == "" {
			comicID = gjson.Get(base, "comic_id").String()
		}
		for _, it := range items {
			itemRaw := it.Raw
			name := cleanHTML(extractJSONRule(itemRaw, src.RuleToc.ChapterName))
			urlRule := strings.ReplaceAll(src.RuleToc.ChapterURL, "@get:{comic_id}", comicID)
			linkRaw := renderTemplateWithJSON(urlRule, itemRaw)
			linkRaw = renderSingleBraceTemplateWithJSON(linkRaw, itemRaw)
			link := absURL(base, linkRaw)
			if name == "" || link == "" || seen[link] {
				continue
			}
			seen[link] = true
			out = append(out, map[string]any{"name": name, "url": link})
		}
		return out
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	listSel := selectNodes(doc.Selection, src.RuleToc.ChapterList)
	out := make([]map[string]any, 0, listSel.Length())
	seen := map[string]bool{}
	listSel.Each(func(_ int, s *goquery.Selection) {
		name := cleanHTML(extractByRule(s, src.RuleToc.ChapterName, base))
		link := absURL(base, extractByRule(s, src.RuleToc.ChapterURL, base))
		if name == "" || link == "" || seen[link] {
			return
		}
		seen[link] = true
		out = append(out, map[string]any{"name": name, "url": link})
	})
	return out
}

func extractByRuleFromHTML(html, rule, base string) string {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return ""
	}
	if strings.Contains(rule, "\n@js:") {
		parts := strings.SplitN(rule, "\n@js:", 2)
		leftRule := strings.TrimSpace(parts[0])
		jsCode := strings.TrimSpace(parts[1])
		leftVal := extractByRuleFromHTML(html, leftRule, base)
		return runJSExprWithInput(leftVal, jsCode)
	}
	// 兼容旧式 <js>...</js> 规则
	if strings.Contains(strings.ToLower(rule), "<js>") {
		return runLegacyJSBlock(html, rule)
	}
	// JSON 规则直接走 JSON 解析，不要求是 HTML
	if strings.HasPrefix(strings.ToLower(rule), "@json:") {
		return extractJSONByRule(html, rule)
	}
	if strings.Contains(rule, "$..") || strings.Contains(rule, "$.") || strings.Contains(rule, "{{$.") {
		return extractJSONRule(html, rule)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return ""
	}
	return extractByRule(doc.Selection, rule, base)
}

func extractByRuleWithPostProcess(html, rule, base string) string {
	mainRule, pattern, replacement := splitCleanRule(rule)
	res := extractByRuleFromHTML(html, mainRule, base)
	if pattern == "" {
		return res
	}
	return regexReplace(res, pattern, replacement)
}

func regexReplace(input, pattern, replacement string) string {
	if strings.TrimSpace(pattern) == "" {
		return input
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return input
	}
	return re.ReplaceAllString(input, replacement)
}

func extractByRule(sel *goquery.Selection, rule, base string) string {
	rawRule := rule
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return ""
	}
	// @text: 需要保留尾部空格，不能被 TrimSpace 吃掉
	if idx := strings.Index(strings.ToLower(rawRule), "@text:"); idx >= 0 && strings.TrimSpace(rawRule[:idx]) == "" {
		return rawRule[idx+6:]
	}

	if strings.Contains(rule, "||") {
		for _, p := range splitByOperator(rule, "||") {
			v := strings.TrimSpace(extractByRule(sel, p, base))
			if v != "" {
				return v
			}
		}
		return ""
	}
	if strings.Contains(rule, "&&") {
		var b strings.Builder
		for _, p := range splitByOperator(rule, "&&") {
			b.WriteString(extractByRule(sel, p, base))
		}
		return b.String()
	}
	if strings.HasPrefix(strings.ToLower(rule), "@js:") {
		jsBody := strings.TrimSpace(strings.TrimPrefix(rule, "@js:"))
		return runJSRule(sel, jsBody)
	}
	if strings.HasPrefix(strings.ToLower(rule), "@regex:") {
		p := strings.TrimSpace(strings.TrimPrefix(rule, "@regex:"))
		re, err := regexp.Compile(p)
		if err != nil {
			return ""
		}
		m := re.FindStringSubmatch(sel.Text())
		if len(m) == 0 {
			return ""
		}
		if len(m) > 1 {
			return m[1]
		}
		return m[0]
	}
	if strings.HasPrefix(strings.ToLower(rule), "@xpath:") {
		return extractXPath(sel, strings.TrimSpace(strings.TrimPrefix(rule, "@xpath:")))
	}

	mainRule, pattern, replacement := splitCleanRule(rule)
	if pattern != "" {
		raw := extractByRule(sel, mainRule, base)
		return regexReplace(raw, pattern, replacement)
	}

	parts := strings.Split(rule, "@")
	cur := sel
	start := 0
	if len(parts) > 0 {
		first := strings.TrimSpace(parts[0])
		if first != "" {
			cur = selectNodes(cur, first)
		}
		start = 1
	}
	for i := start; i < len(parts); i++ {
		p := strings.TrimSpace(parts[i])
		if p == "" {
			continue
		}
		switch {
		case p == "text":
			return strings.TrimSpace(cur.Text())
		case p == "textNodes":
			return strings.TrimSpace(cur.First().Text())
		case p == "html":
			h, _ := cur.First().Html()
			return strings.TrimSpace(h)
		case p == "href":
			v, _ := cur.First().Attr("href")
			return strings.TrimSpace(v)
		case p == "src":
			v, _ := cur.First().Attr("src")
			return strings.TrimSpace(v)
		case p == "data-src":
			v, _ := cur.First().Attr("data-src")
			return strings.TrimSpace(v)
		case strings.HasPrefix(p, "data-"):
			v, _ := cur.First().Attr(p)
			return strings.TrimSpace(v)
		case strings.HasPrefix(p, "attr-"):
			attrName := strings.TrimPrefix(p, "attr-")
			v, _ := cur.First().Attr(attrName)
			return strings.TrimSpace(v)
		default:
			cur = selectNodes(cur, p)
		}
	}
	// 未显式指定 text/href/html 时，默认文本
	return strings.TrimSpace(cur.First().Text())
}

func selectNodes(sel *goquery.Selection, token string) *goquery.Selection {
	token = strings.TrimSpace(token)
	if token == "" {
		return sel
	}
	token = strings.ReplaceAll(token, "&&", " ")
	token = strings.TrimSpace(token)

	// 支持 @css:li[1] 的索引（0 基）
	if strings.HasPrefix(token, "@css:") || strings.HasPrefix(token, "css:") {
		raw := strings.TrimPrefix(strings.TrimPrefix(token, "@css:"), "css:")
		raw = strings.TrimSpace(raw)
		re := regexp.MustCompile(`^(.*)\[(-?\d+)\]\s*$`)
		if m := re.FindStringSubmatch(raw); len(m) == 3 {
			nodes := sel.Find(strings.TrimSpace(m[1]))
			idx, _ := strconv.Atoi(m[2])
			if nodes.Length() == 0 {
				return nodes
			}
			if idx < 0 {
				idx = nodes.Length() + idx
			}
			if idx < 0 || idx >= nodes.Length() {
				return nodes.Filter("_never_match_")
			}
			return nodes.Eq(idx)
		}
		return selectWithJQueryPseudo(sel, raw)
	}

	if strings.HasPrefix(token, "@css:") {
		return sel.Find(strings.TrimSpace(strings.TrimPrefix(token, "@css:")))
	}
	if strings.HasPrefix(token, "css:") {
		return sel.Find(strings.TrimSpace(strings.TrimPrefix(token, "css:")))
	}
	if strings.HasPrefix(token, "id.") {
		return sel.Find("#" + strings.TrimSpace(strings.TrimPrefix(token, "id.")))
	}
	if strings.HasPrefix(token, "class.") {
		return sel.Find("." + strings.TrimSpace(strings.TrimPrefix(token, "class.")))
	}
	if strings.HasPrefix(token, "tag.") {
		return sel.Find(strings.TrimSpace(strings.TrimPrefix(token, "tag.")))
	}
	// 兜底当 css 选择器
	return sel.Find(token)
}

func cleanHTML(s string) string {
	s = regexp.MustCompile(`(?s)<script.*?</script>`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`(?s)<style.*?</style>`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`<br\s*/?>`).ReplaceAllString(s, "\n")
	s = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	return strings.TrimSpace(s)
}

func absURL(base, p string) string {
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
		return p
	}
	base = strings.TrimSpace(base)
	if strings.HasPrefix(base, "http://") || strings.HasPrefix(base, "https://") {
		u, err := url.Parse(base)
		if err == nil {
			base = u.Scheme + "://" + u.Host
		}
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(p, "/")
}

func writeErr(w http.ResponseWriter, code int, msg string) { writeJSON(w, code, map[string]any{"error": msg}) }
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func safePreview(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func saveSearchDebug(sourceIndex int, src Source, key, reqURL, parser, html string, items []map[string]any) {
	now := time.Now()
	baseDir := filepath.Join("debug_logs", now.Format("20060102"))
	_ = os.MkdirAll(baseDir, 0o755)
	name := fmt.Sprintf(
		"%s_source%d_%s",
		now.Format("150405"),
		sourceIndex,
		sanitizeFileName(src.BookSourceName),
	)

	meta := map[string]any{
		"time":        now.Format(time.RFC3339),
		"sourceIndex": sourceIndex,
		"sourceName":  src.BookSourceName,
		"sourceUrl":   src.BookSourceURL,
		"key":         key,
		"requestUrl":  reqURL,
		"parser":      parser,
		"htmlLen":     len(html),
		"resultCount": len(items),
	}
	if b, err := json.MarshalIndent(meta, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(baseDir, name+"_meta.json"), b, 0o644)
	}
	_ = os.WriteFile(filepath.Join(baseDir, name+"_raw.html"), []byte(html), 0o644)
	if b, err := json.MarshalIndent(items, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(baseDir, name+"_items.json"), b, 0o644)
	}
	log.Printf("[search] debug saved: %s", filepath.Join(baseDir, name+"_*.{json,html}"))
}

func saveDetailDebug(sourceIndex int, src Source, reqURL, html string, result map[string]any) {
	now := time.Now()
	baseDir := filepath.Join("debug_logs", now.Format("20060102"))
	_ = os.MkdirAll(baseDir, 0o755)
	name := fmt.Sprintf("%s_source%d_%s_detail", now.Format("150405"), sourceIndex, sanitizeFileName(src.BookSourceName))

	meta := map[string]any{
		"time":        now.Format(time.RFC3339),
		"type":        "detail",
		"sourceIndex": sourceIndex,
		"sourceName":  src.BookSourceName,
		"sourceUrl":   src.BookSourceURL,
		"requestUrl":  reqURL,
		"htmlLen":     len(html),
	}
	if b, err := json.MarshalIndent(meta, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(baseDir, name+"_meta.json"), b, 0o644)
	}
	_ = os.WriteFile(filepath.Join(baseDir, name+"_raw.html"), []byte(html), 0o644)
	if b, err := json.MarshalIndent(result, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(baseDir, name+"_result.json"), b, 0o644)
	}
	log.Printf("[detail] debug saved: %s", filepath.Join(baseDir, name+"_*.{json,html}"))
}

func saveChapterDebug(sourceIndex int, src Source, reqURL, html string, result map[string]any) {
	now := time.Now()
	baseDir := filepath.Join("debug_logs", now.Format("20060102"))
	_ = os.MkdirAll(baseDir, 0o755)
	name := fmt.Sprintf("%s_source%d_%s_chapter", now.Format("150405"), sourceIndex, sanitizeFileName(src.BookSourceName))

	meta := map[string]any{
		"time":        now.Format(time.RFC3339),
		"type":        "chapter",
		"sourceIndex": sourceIndex,
		"sourceName":  src.BookSourceName,
		"sourceUrl":   src.BookSourceURL,
		"requestUrl":  reqURL,
		"htmlLen":     len(html),
	}
	if b, err := json.MarshalIndent(meta, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(baseDir, name+"_meta.json"), b, 0o644)
	}
	_ = os.WriteFile(filepath.Join(baseDir, name+"_raw.html"), []byte(html), 0o644)
	if b, err := json.MarshalIndent(result, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(baseDir, name+"_result.json"), b, 0o644)
	}
	log.Printf("[chapter] debug saved: %s", filepath.Join(baseDir, name+"_*.{json,html}"))
}

func sanitizeFileName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "unknown"
	}
	re := regexp.MustCompile(`[\\/:*?"<>|\s]+`)
	s = re.ReplaceAllString(s, "_")
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}

func splitByOperator(rule, op string) []string {
	parts := strings.Split(rule, op)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		raw := p
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(strings.ToLower(trimmed), "@text:") {
			// 保留 @text: 后面的空格语义（例如 "@text: - "）
			idx := strings.Index(strings.ToLower(raw), "@text:")
			if idx >= 0 {
				out = append(out, raw[idx:])
				continue
			}
		}
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func splitCleanRule(rule string) (mainRule, pattern, replacement string) {
	parts := strings.Split(rule, "##")
	mainRule = strings.TrimSpace(parts[0])
	if len(parts) < 2 {
		return mainRule, "", ""
	}
	pattern = parts[1]
	if len(parts) >= 3 {
		replacement = parts[2]
	} else {
		replacement = ""
	}
	return
}

func extractJSONByRule(source, rule string) string {
	path := strings.TrimSpace(strings.TrimPrefix(rule, "@json:"))
	path = normalizeJSONPath(path)
	if path == "" {
		return ""
	}
	v := gjson.Get(source, path)
	if !v.Exists() {
		return ""
	}
	if v.IsArray() {
		arr := v.Array()
		if len(arr) == 0 {
			return ""
		}
		return arr[0].String()
	}
	return v.String()
}

func extractJSONRule(source, rule string) string {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return ""
	}
	// 兼容 Legado 变量写入语法：@put:{k:$..v}
	// 当前引擎先不维护变量池，这里只避免把它当正文展示
	if strings.HasPrefix(rule, "@put:{") {
		return ""
	}
	mainRule, pattern, replacement := splitCleanRule(rule)
	mainRule = strings.TrimSpace(mainRule)
	if strings.Contains(mainRule, "&&") && !strings.Contains(mainRule, "{{") && !strings.Contains(mainRule, "@js:") {
		parts := splitByOperator(mainRule, "&&")
		buf := make([]string, 0, len(parts))
		for _, p := range parts {
			buf = append(buf, strings.TrimSpace(extractJSONRule(source, p)))
		}
		out := strings.Join(buf, "")
		if pattern != "" {
			out = regexReplace(out, pattern, replacement)
		}
		return out
	}
	if strings.Contains(mainRule, "{{$.") || strings.Contains(mainRule, "{{$..") {
		mainRule = renderTemplateWithJSON(mainRule, source)
	}
	var out string
	if strings.HasPrefix(strings.ToLower(mainRule), "@json:") {
		out = extractJSONByRule(source, mainRule)
	} else if strings.HasPrefix(mainRule, "$.") || strings.HasPrefix(mainRule, "$..") {
		rs := getJSONResultsByRulePath(source, mainRule)
		if len(rs) > 0 {
			if len(rs) == 1 {
				out = rs[0].String()
			} else {
				parts := make([]string, 0, len(rs))
				for _, r := range rs {
					parts = append(parts, r.String())
				}
				out = strings.Join(parts, "\n")
			}
		}
	} else {
		// 裸字段名（如 name/authorName/chapterName）按 JSON key 取值
		if regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_\.]*$`).MatchString(mainRule) {
			rs := getJSONResultsByRulePath(source, mainRule)
			if len(rs) > 0 {
				out = rs[0].String()
			} else {
				out = ""
			}
		} else {
			out = mainRule
		}
	}
	if pattern != "" {
		out = regexReplace(out, pattern, replacement)
	}
	return out
}

func renderTemplateWithJSON(tpl, source string) string {
	re := regexp.MustCompile(`\{\{([^}]+)\}\}`)
	return re.ReplaceAllStringFunc(tpl, func(m string) string {
		sub := re.FindStringSubmatch(m)
		if len(sub) != 2 {
			return m
		}
		expr := strings.TrimSpace(sub[1])
		if strings.Contains(expr, "&&") {
			parts := splitByOperator(expr, "&&")
			buf := make([]string, 0, len(parts))
			for _, p := range parts {
				rs := getJSONResultsByRulePath(source, strings.TrimSpace(p))
				if len(rs) > 0 {
					buf = append(buf, rs[0].String())
				}
			}
			return strings.Join(buf, "")
		}
		rs := getJSONResultsByRulePath(source, expr)
		if len(rs) == 0 {
			return ""
		}
		return rs[0].String()
	})
}

func renderSingleBraceTemplateWithJSON(tpl, source string) string {
	re := regexp.MustCompile(`\{([^{}]+)\}`)
	return re.ReplaceAllStringFunc(tpl, func(m string) string {
		sub := re.FindStringSubmatch(m)
		if len(sub) != 2 {
			return m
		}
		expr := strings.TrimSpace(sub[1])
		if expr == "" {
			return m
		}
		if strings.Contains(expr, "&&") {
			parts := splitByOperator(expr, "&&")
			buf := make([]string, 0, len(parts))
			for _, p := range parts {
				rs := getJSONResultsByRulePath(source, strings.TrimSpace(p))
				if len(rs) > 0 {
					buf = append(buf, rs[0].String())
				}
			}
			return strings.Join(buf, "")
		}
		rs := getJSONResultsByRulePath(source, expr)
		if len(rs) == 0 {
			return ""
		}
		return rs[0].String()
	})
}

func getJSONResultsByRulePath(source, rawPath string) []gjson.Result {
	path := strings.TrimSpace(rawPath)
	path = strings.TrimPrefix(path, "@json:")
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if strings.Contains(path, "[*]") {
		return getJSONResultsByWildcardPath(source, path)
	}

	// 兼容 $..list[*] 这类递归写法
	if strings.HasPrefix(path, "$..") {
		key := strings.TrimPrefix(path, "$..")
		key = strings.TrimSuffix(key, "[*]")
		key = strings.TrimSpace(key)
		if key != "" {
			found := findAllByKeyRecursive(gjson.Parse(source), key)
			if strings.HasSuffix(path, "[*]") {
				var flat []gjson.Result
				for _, r := range found {
					if r.IsArray() {
						flat = append(flat, r.Array()...)
					} else {
						flat = append(flat, r)
					}
				}
				return flat
			}
			return found
		}
	}

	norm := normalizeJSONPath(path)
	v := gjson.Get(source, norm)
	if !v.Exists() {
		return nil
	}
	if v.IsArray() {
		return v.Array()
	}
	return []gjson.Result{v}
}

func getJSONResultsByWildcardPath(source, path string) []gjson.Result {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "$.")
	path = strings.TrimPrefix(path, "$")
	path = strings.TrimPrefix(path, ".")
	if path == "" {
		return nil
	}

	cur := []gjson.Result{gjson.Parse(source)}
	parts := strings.Split(path, ".")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		next := make([]gjson.Result, 0)
		if strings.HasSuffix(p, "[*]") {
			key := strings.TrimSuffix(p, "[*]")
			for _, c := range cur {
				var v gjson.Result
				if key == "" {
					v = c
				} else {
					v = c.Get(key)
				}
				if !v.Exists() {
					continue
				}
				if v.IsArray() {
					next = append(next, v.Array()...)
				}
			}
		} else {
			for _, c := range cur {
				v := c.Get(p)
				if v.Exists() {
					next = append(next, v)
				}
			}
		}
		cur = next
		if len(cur) == 0 {
			return nil
		}
	}
	return cur
}

func isLikelyJSONRule(rule string) bool {
	r := strings.TrimSpace(rule)
	if r == "" {
		return false
	}
	if strings.HasPrefix(r, "$") || strings.HasPrefix(strings.ToLower(r), "@json:") {
		return true
	}
	if strings.HasPrefix(r, "data.") || strings.HasPrefix(r, "result.") {
		return true
	}
	return false
}

func findAllByKeyRecursive(node gjson.Result, key string) []gjson.Result {
	var out []gjson.Result
	if node.IsObject() {
		node.ForEach(func(k, v gjson.Result) bool {
			if k.String() == key {
				out = append(out, v)
			}
			out = append(out, findAllByKeyRecursive(v, key)...)
			return true
		})
		return out
	}
	if node.IsArray() {
		node.ForEach(func(_, v gjson.Result) bool {
			out = append(out, findAllByKeyRecursive(v, key)...)
			return true
		})
	}
	return out
}

func normalizeJSONPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "$.")
	path = strings.TrimPrefix(path, "$")
	// 兼容数组写法：
	// [*] -> .#   （gjson 数组遍历）
	// [12] -> .12
	path = strings.ReplaceAll(path, "[*]", ".#.")
	reIndex := regexp.MustCompile(`\[(\d+)\]`)
	path = reIndex.ReplaceAllString(path, ".$1")
	for strings.Contains(path, "..") {
		path = strings.ReplaceAll(path, "..", ".")
	}
	path = strings.TrimSuffix(path, ".")
	path = strings.TrimPrefix(path, ".")
	return strings.TrimSpace(path)
}

func extractXPath(sel *goquery.Selection, expr string) string {
	html, err := goquery.OuterHtml(sel)
	if err != nil {
		return ""
	}
	root, err := htmlquery.Parse(strings.NewReader(html))
	if err != nil {
		return ""
	}
	nodes, err := htmlquery.QueryAll(root, expr)
	if err != nil || len(nodes) == 0 {
		return ""
	}
	if strings.Contains(expr, "/@") {
		lastAttr := expr[strings.LastIndex(expr, "/@")+2:]
		lastAttr = strings.TrimSpace(lastAttr)
		if lastAttr != "" {
			for _, n := range nodes {
				for _, a := range n.Attr {
					if a.Key == lastAttr {
						return strings.TrimSpace(a.Val)
					}
				}
			}
		}
	}
	txt := strings.TrimSpace(htmlquery.InnerText(nodes[0]))
	if txt != "" {
		return txt
	}
	attrs := make([]string, 0, len(nodes[0].Attr))
	for _, a := range nodes[0].Attr {
		attrs = append(attrs, a.Val)
	}
	sort.Strings(attrs)
	if len(attrs) > 0 {
		return strings.TrimSpace(attrs[0])
	}
	return ""
}

func runJSRule(sel *goquery.Selection, code string) string {
	vm := goja.New()

	sourceText := strings.TrimSpace(sel.Text())
	_ = vm.Set("source", sourceText)
	_ = vm.Set("result", sourceText)

	_ = vm.Set("trim", func(v any) string {
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	})
	_ = vm.Set("replace", func(v any, oldV any, newV any) string {
		return strings.Replace(fmt.Sprintf("%v", v), fmt.Sprintf("%v", oldV), fmt.Sprintf("%v", newV), 1)
	})
	_ = vm.Set("replaceAll", func(v any, oldV any, newV any) string {
		return strings.ReplaceAll(fmt.Sprintf("%v", v), fmt.Sprintf("%v", oldV), fmt.Sprintf("%v", newV))
	})
	_ = vm.Set("split", func(v any, sep any) []string {
		return strings.Split(fmt.Sprintf("%v", v), fmt.Sprintf("%v", sep))
	})
	_ = vm.Set("join", func(arr []any, sep any) string {
		p := make([]string, 0, len(arr))
		for _, x := range arr {
			p = append(p, fmt.Sprintf("%v", x))
		}
		return strings.Join(p, fmt.Sprintf("%v", sep))
	})
	_ = vm.Set("isEmpty", func(v any) bool {
		if v == nil {
			return true
		}
		return strings.TrimSpace(fmt.Sprintf("%v", v)) == ""
	})

	v, err := vm.RunString(code)
	if err != nil {
		// 兜底保持兼容：表达式执行失败时返回空
		return ""
	}
	if goja.IsUndefined(v) || goja.IsNull(v) {
		return ""
	}
	return fmt.Sprintf("%v", v.Export())
}

func runJSExprWithInput(input, code string) string {
	vm := goja.New()
	_ = vm.Set("source", input)
	_ = vm.Set("result", input)
	_ = vm.Set("trim", func(v any) string {
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	})
	_ = vm.Set("replace", func(v any, oldV any, newV any) string {
		return strings.Replace(fmt.Sprintf("%v", v), fmt.Sprintf("%v", oldV), fmt.Sprintf("%v", newV), 1)
	})
	_ = vm.Set("replaceAll", func(v any, oldV any, newV any) string {
		return strings.ReplaceAll(fmt.Sprintf("%v", v), fmt.Sprintf("%v", oldV), fmt.Sprintf("%v", newV))
	})
	v, err := vm.RunString(code)
	if err != nil {
		return ""
	}
	if goja.IsUndefined(v) || goja.IsNull(v) {
		return ""
	}
	return fmt.Sprintf("%v", v.Export())
}

func selectWithJQueryPseudo(sel *goquery.Selection, expr string) *goquery.Selection {
	expr = strings.TrimSpace(expr)
	re := regexp.MustCompile(`^(.*?):(eq|gt|lt)\((-?\d+)\)(.*)$`)
	m := re.FindStringSubmatch(expr)
	if len(m) != 5 {
		return sel.Find(expr)
	}
	left := strings.TrimSpace(m[1])
	op := m[2]
	n, _ := strconv.Atoi(m[3])
	right := strings.TrimSpace(m[4])
	nodes := sel.Find(left)
	switch op {
	case "eq":
		if nodes.Length() == 0 {
			return nodes
		}
		if n < 0 {
			n = nodes.Length() + n
		}
		if n < 0 || n >= nodes.Length() {
			return nodes.Filter("_never_match_")
		}
		nodes = nodes.Eq(n)
	case "gt":
		nodes = nodes.FilterFunction(func(i int, _ *goquery.Selection) bool { return i > n })
	case "lt":
		nodes = nodes.FilterFunction(func(i int, _ *goquery.Selection) bool { return i < n })
	}
	if right != "" {
		nodes = nodes.Find(right)
	}
	return nodes
}

func runLegacyJSBlock(html, rule string) string {
	jsCode := extractJSCode(rule)
	if jsCode == "" {
		return ""
	}
	// 特化支持：doc.select("...").text() 这类常见旧规则
	reDocText := regexp.MustCompile(`doc\.select\("([^"]+)"\)\.text\(\)`)
	transformed := reDocText.ReplaceAllString(jsCode, `selectText("$1")`)
	transformed = strings.ReplaceAll(transformed, "var doc = org.jsoup.Jsoup.parse(result);", "")

	lastVar := ""
	reLastVar := regexp.MustCompile(`var\s+([A-Za-z_]\w*)\s*=`)
	matches := reLastVar.FindAllStringSubmatch(transformed, -1)
	if len(matches) > 0 {
		lastVar = matches[len(matches)-1][1]
	}

	vm := goja.New()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return ""
	}
	_ = vm.Set("result", html)
	_ = vm.Set("source", html)
	_ = vm.Set("selectText", func(css string) string {
		return strings.TrimSpace(doc.Find(css).First().Text())
	})

	if lastVar != "" && !strings.Contains(transformed, "return ") {
		transformed = transformed + "\n;" + lastVar
	}
	v, err := vm.RunString(transformed)
	if err != nil {
		return ""
	}
	if goja.IsUndefined(v) || goja.IsNull(v) {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v.Export()))
}

func extractJSCode(rule string) string {
	lower := strings.ToLower(rule)
	start := strings.Index(lower, "<js>")
	end := strings.LastIndex(lower, "</js>")
	if start >= 0 && end > start {
		return strings.TrimSpace(rule[start+4 : end])
	}
	return ""
}
