package main

import "testing"

func TestRuleSupport_TextFallbackConcat(t *testing.T) {
	html := `<div class="title">书名X</div><div class="author">作者Y</div>`
	rule := `@css:.none@text || @css:.title@text && @text: - && @css:.author@text`
	got := extractByRuleFromHTML(html, rule, "https://a.com")
	if got != "书名X - 作者Y" {
		t.Fatalf("got=%q", got)
	}
}

func TestRuleSupport_CleanReplace(t *testing.T) {
	html := `<div class="price">价格：￥128.50元</div>`
	rule := `@css:.price@text##[^\d\.]##`
	got := extractByRuleWithPostProcess(html, rule, "https://a.com")
	if got != "128.50" {
		t.Fatalf("got=%q", got)
	}
}

func TestRuleSupport_JsonSelector(t *testing.T) {
	jsonText := `{"book":{"title":"三体","author":"刘慈欣"},"items":[{"name":"A"},{"name":"B"}]}`
	if v := extractByRuleFromHTML(jsonText, `@json:$.book.title`, ""); v != "三体" {
		t.Fatalf("json title=%q", v)
	}
	if v := extractByRuleFromHTML(jsonText, `@json:items[1].name`, ""); v != "B" {
		t.Fatalf("json items[1]=%q", v)
	}
}

func TestRuleSupport_XPathSelector(t *testing.T) {
	html := `<html><body><h1>标题A</h1><img src="/a.jpg" /></body></html>`
	if v := extractByRuleFromHTML(html, `@xpath://h1/text()`, "https://a.com"); v != "标题A" {
		t.Fatalf("xpath text=%q", v)
	}
	if v := extractByRuleFromHTML(html, `@xpath://img/@src`, "https://a.com"); v != "/a.jpg" {
		t.Fatalf("xpath src=%q", v)
	}
}

func TestRuleSupport_JSRegex(t *testing.T) {
	if v := extractByRuleFromHTML("", `@js:"常量值"`, ""); v != "常量值" {
		t.Fatalf("js const=%q", v)
	}
	html := `<div>  Abc-123  </div>`
	if v := extractByRuleFromHTML(html, `@js:trim(source)`, ""); v != "Abc-123" {
		t.Fatalf("js trim=%q", v)
	}
	if v := extractByRuleFromHTML(html, `@js:replaceAll(trim(source), "-", "_")`, ""); v != "Abc_123" {
		t.Fatalf("js replaceAll=%q", v)
	}
	if v := extractByRuleFromHTML("abc123def", `@regex:\d+`, ""); v != "123" {
		t.Fatalf("regex=%q", v)
	}
}

func TestRuleSupport_CSSContainsAndIndex(t *testing.T) {
	html := `
<ul>
  <li>第一章</li>
  <li>第二章</li>
  <li>第三章</li>
</ul>`
	if v := extractByRuleFromHTML(html, `@css:li:contains(第二)@text`, ""); v != "第二章" {
		t.Fatalf("contains=%q", v)
	}
	if v := extractByRuleFromHTML(html, `@css:li[1]@text`, ""); v != "第二章" {
		t.Fatalf("index=%q", v)
	}
}

func TestRuleSupport_CSSPseudoEq(t *testing.T) {
	html := `<div class="novel_info"><p><a>作者甲</a></p><p><a>作者乙</a></p></div>`
	v := extractByRuleFromHTML(html, `@css:.novel_info p:eq(0) a@text`, "")
	if v != "作者甲" {
		t.Fatalf("eq(0) got=%q", v)
	}
}

func TestRuleSupport_LegacyJSBlock(t *testing.T) {
	html := `<div class="tags">标签A</div><div class="jianjie"><p>简介B</p></div>`
	rule := `<js>
var doc = org.jsoup.Jsoup.parse(result);
var infoText = doc.select(".tags").text() + " " + doc.select(".jianjie p").text();
</js>`
	v := extractByRuleFromHTML(html, rule, "")
	if v != "标签A 简介B" {
		t.Fatalf("legacy js got=%q", v)
	}
}

func TestRuleSupport_DataAndAttrDynamic(t *testing.T) {
	html := `<div class="item" data-id="42" data-kind="novel" title="T1" custom-attr="C9">X</div>`

	if v := extractByRuleFromHTML(html, `@css:.item@data-id`, ""); v != "42" {
		t.Fatalf("@data-id got=%q", v)
	}
	if v := extractByRuleFromHTML(html, `@css:.item@data-kind`, ""); v != "novel" {
		t.Fatalf("@data-kind got=%q", v)
	}
	if v := extractByRuleFromHTML(html, `@css:.item@attr-title`, ""); v != "T1" {
		t.Fatalf("@attr-title got=%q", v)
	}
	if v := extractByRuleFromHTML(html, `@css:.item@attr-custom-attr`, ""); v != "C9" {
		t.Fatalf("@attr-custom-attr got=%q", v)
	}
}

func TestRuleSupport_SelectorEntrances(t *testing.T) {
	html := `<div id="book"><span class="name">三体</span></div>`

	if v := extractByRuleFromHTML(html, `@css:#book .name@text`, ""); v != "三体" {
		t.Fatalf("@css entrance got=%q", v)
	}
	if v := extractByRuleFromHTML(html, `css:#book .name@text`, ""); v != "三体" {
		t.Fatalf("css: entrance got=%q", v)
	}
	if v := extractByRuleFromHTML(html, `#book .name@text`, ""); v != "三体" {
		t.Fatalf("raw css entrance got=%q", v)
	}
}

func TestRuleSupport_FieldExtractors(t *testing.T) {
	html := `<div class="item"><a href="/book/1" data-src="/cover.jpg"><span>第一本</span></a><p><b>粗体</b></p></div>`

	if v := extractByRuleFromHTML(html, `@css:.item a@href`, ""); v != "/book/1" {
		t.Fatalf("@href got=%q", v)
	}
	if v := extractByRuleFromHTML(html, `@css:.item a@data-src`, ""); v != "/cover.jpg" {
		t.Fatalf("@data-src got=%q", v)
	}
	if v := extractByRuleFromHTML(`<img src="/a.png">`, `@css:img@src`, ""); v != "/a.png" {
		t.Fatalf("@src got=%q", v)
	}
	if v := extractByRuleFromHTML(html, `@css:.item p@html`, ""); v != `<b>粗体</b>` {
		t.Fatalf("@html got=%q", v)
	}
	if v := extractByRuleFromHTML(html, `@css:.item@textNodes`, ""); v != "第一本粗体" {
		t.Fatalf("@textNodes got=%q", v)
	}
}

func TestRuleSupport_CSSAliasAndPseudo(t *testing.T) {
	html := `
<div id="root">
  <ul class="list">
    <li><a>第1章</a></li>
    <li><a>第2章</a></li>
    <li><a>第3章</a></li>
  </ul>
</div>`

	if v := extractByRuleFromHTML(html, `id.root@css:.list li:eq(1) a@text`, ""); v != "第2章" {
		t.Fatalf("id.xxx got=%q", v)
	}
	if v := extractByRuleFromHTML(html, `@css:.list li:gt(1) a@text`, ""); v != "第3章" {
		t.Fatalf(":gt got=%q", v)
	}
	if v := extractByRuleFromHTML(html, `tag.li a@text`, ""); v != "第1章第2章第3章" {
		t.Fatalf("tag.xxx/:lt got=%q", v)
	}
}

func TestRuleSupport_JSONAdvanced(t *testing.T) {
	jsonText := `{
		"data":{"id":"b1","book":{"title":"三体","authorName":"刘慈欣"}},
		"items":[{"name":"A","id":"1"},{"name":"B","id":"2"}],
		"nested":{"x":{"name":"递归命中"}}
	}`

	if v := extractByRuleFromHTML(jsonText, `$.data.book.title`, ""); v != "三体" {
		t.Fatalf("$.path got=%q", v)
	}
	if v := extractByRuleFromHTML(jsonText, `$..name`, ""); v != "A\nB\n递归命中" {
		t.Fatalf("$..name got=%q", v)
	}
	if v := extractByRuleFromHTML(jsonText, `$.items[*].name`, ""); v != "A\nB" {
		t.Fatalf("[*] got=%q", v)
	}
	if v := extractByRuleFromHTML(`{"book":{"name":"裸字段值"}}`, `@json:book.name`, ""); v != "裸字段值" {
		t.Fatalf("bare key got=%q", v)
	}
	if v := extractByRuleFromHTML(jsonText, `https://api.demo.com/book/{{$.data.id}}`, ""); v != "https://api.demo.com/book/b1" {
		t.Fatalf("{{}} template got=%q", v)
	}
	if v := extractByRuleFromHTML(jsonText, `$.data.id&&$.data.book.title`, ""); v != "b1三体" {
		t.Fatalf("json && got=%q", v)
	}
	if v := extractByRuleFromHTML(jsonText, `@put:{k:$..name}`, ""); v != "" {
		t.Fatalf("@put should be ignored, got=%q", v)
	}
}

func TestRuleSupport_ParseSearchRule_HTML(t *testing.T) {
	src := Source{BookSourceURL: "https://demo.com"}
	src.RuleSearch.BookList = "@css:.book-item"
	src.RuleSearch.Name = "@css:.title@text"
	src.RuleSearch.Author = "@css:.author@text"
	src.RuleSearch.Intro = "@css:.intro@text"
	src.RuleSearch.BookURL = "@css:a@href"

	html := `<div class="book-item"><a href="/b1"><span class="title">书A</span></a><span class="author">作A</span><p class="intro">介A</p></div>`
	items, parser := parseSearchByRule(src, html, src.BookSourceURL)
	if parser != "rule" || len(items) != 1 {
		t.Fatalf("search html parser=%q len=%d", parser, len(items))
	}
	if items[0]["name"] != "书A" || items[0]["url"] != "https://demo.com/b1" {
		t.Fatalf("search html item=%v", items[0])
	}
}

func TestRuleSupport_ParseSearchRule_JSON(t *testing.T) {
	src := Source{BookSourceURL: "https://api.demo.com"}
	src.RuleSearch.BookList = "$.data.list[*]"
	src.RuleSearch.Name = "name"
	src.RuleSearch.Author = "authorName"
	src.RuleSearch.Intro = "intro"
	src.RuleSearch.BookURL = "https://api.demo.com/book/{{$.book_id}}"

	body := `{"data":{"list":[{"name":"书J","authorName":"作J","intro":"介J","book_id":"99"}]}}`
	items, parser := parseSearchByRule(src, body, src.BookSourceURL)
	if parser != "rule" || len(items) != 1 {
		t.Fatalf("search json parser=%q len=%d", parser, len(items))
	}
	if items[0]["name"] != "书J" || items[0]["url"] != "https://api.demo.com/book/99" {
		t.Fatalf("search json item=%v", items[0])
	}
}

func TestRuleSupport_ParseBookInfoRule(t *testing.T) {
	src := Source{}
	src.RuleBookInfo.Name = "@css:h1@text"
	src.RuleBookInfo.Author = "@css:.author@text"
	src.RuleBookInfo.Intro = "@css:.intro@text"
	src.RuleBookInfo.Cover = "@css:img@src"
	src.RuleBookInfo.TocURL = "@css:a.toc@href"

	html := `<h1>书名A</h1><div class="author">作者A</div><div class="intro">简介A</div><img src="/c.jpg"><a class="toc" href="/toc/1"></a>`
	book := parseDetailByRule(src, html, "https://demo.com/book/1")
	if book["name"] != "书名A" || book["author"] != "作者A" {
		t.Fatalf("detail basic=%v", book)
	}
	if book["cover"] != "https://demo.com/c.jpg" || book["tocUrl"] != "https://demo.com/toc/1" {
		t.Fatalf("detail urls=%v", book)
	}
}

func TestRuleSupport_ParseTocRule_HTML(t *testing.T) {
	src := Source{}
	src.RuleToc.ChapterList = "@css:.chap li"
	src.RuleToc.ChapterName = "@css:a@text"
	src.RuleToc.ChapterURL = "@css:a@href"

	html := `<ul class="chap"><li><a href="/c1">第一章</a></li><li><a href="/c1">第一章重复</a></li><li><a href="/c2">第二章</a></li></ul>`
	chs := parseChaptersByRule(src, html, "https://demo.com")
	if len(chs) != 2 {
		t.Fatalf("toc html len=%d data=%v", len(chs), chs)
	}
}

func TestRuleSupport_ParseTocRule_JSONAndSingleBraceURL(t *testing.T) {
	src := Source{}
	src.RuleToc.ChapterList = "$.data.chapters[*]"
	src.RuleToc.ChapterName = "title"
	src.RuleToc.ChapterURL = "https://api.demo.com/chapter/{id}"

	body := `{"data":{"chapters":[{"title":"第1章","id":"11"},{"title":"第2章","id":"12"}]}}`
	chs := parseChaptersByRule(src, body, "https://api.demo.com")
	if len(chs) != 2 {
		t.Fatalf("toc json len=%d data=%v", len(chs), chs)
	}
	if chs[0]["url"] != "https://api.demo.com/chapter/11" {
		t.Fatalf("toc json url=%v", chs[0]["url"])
	}
}

func TestRuleSupport_ParseContentRule(t *testing.T) {
	src := Source{}
	src.RuleContent.Content = "@css:#chapterContent@html"
	html := `<div id="chapterContent"><p>第一段</p><p>第二段</p></div>`
	out := extractByRuleWithPostProcess(html, src.RuleContent.Content, "https://demo.com/c1")
	if out != "<p>第一段</p><p>第二段</p>" {
		t.Fatalf("content rule got=%q", out)
	}
}
