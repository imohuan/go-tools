# single_source_functional_test.go 测试规范

## 1. 目标

这个测试用于“按指定书源做全链路功能验证”，固定走 `main.go` 内函数链路（不走 HTTP 接口）：

1. 搜索
2. 详情（或 API 目录直连回退）
3. 目录（章节列表）
4. 章节正文

核心要求：每一步都尽量拿到“可用数据”。

## 2. 指定配置与书源

通过环境变量指定测试目标：

- `TEST_CONFIG_PATH`：书源配置文件路径（如 `D:/CodeX/go-legado-demo/data-all.json`）
- `TEST_SOURCE_NAME`：书源名称（精确匹配）
- `TEST_KEYWORD`：搜索关键词

配套脚本：

- `test/run_single_source_functional.ps1`

示例：

```powershell
powershell -ExecutionPolicy Bypass -File .\test\run_single_source_functional.ps1 `
  -ConfigPath "D:/CodeX/go-legado-demo/data-all.json" `
  -SourceName "纵横中文网api[搜书专用]" `
  -Keyword "姐"
```

## 3. 测试流程

`TestSingleSourceFlowByFunctions` 按下面顺序执行：

1. 读取配置文件，找到指定 `SourceName`
2. 搜索：拼接搜索 URL，抓取原始响应，按规则解析搜索列表
3. 详情：取第一本书 URL，抓取详情响应，按规则解析详情
4. 目录：
   - 正常：用 `tocUrl` 抓目录并解析章节列表
   - 回退：当 `ruleBookInfo.tocUrl` 为空且 `ruleToc.chapterList` 是 JSON 规则时，直接把第一本书 URL 当 TOC
5. 正文：取第一章 URL 抓正文，并按正文规则解析内容

## 4. 通过/失败判定

## 4.1 强校验（默认）

以下任一条件不满足，先判失败并进入“证据复核”：

1. 搜索列表为空
2. 详情阶段没有可用 TOC 地址
3. 章节列表为空
4. 第一章 URL 为空
5. 第一章正文为空

## 4.2 证据复核（关键规则）

如果强校验失败，必须先看测试缓存（`debug_logs/...`）中的原始响应：

1. 搜索失败：看搜索阶段的 `raw_body` 原始响应
2. 详情失败：看详情阶段的 `raw_body` 原始响应
3. 目录失败：看目录阶段的 `raw_body` 原始响应
4. 正文失败：看正文阶段的 `raw_body` 原始响应

判定原则（原始响应可能是 HTML / JSON / 纯文本，不限格式）：

1. 如果原始响应里本身就没有目标数据（上游确实没给），可判“规则不背锅”，该用例可记为通过（或软通过）
2. 如果原始响应里有数据，但解析结果为空或错误，判“规则引擎问题”，测试必须失败

一句话总结：  
上游没数据，不算解析器错；上游有数据但没取出来，就是解析器要修。

复核顺序（必须按这个顺序）：

1. 先看 `raw_body` 源文件（优先看 HTML/原始文本，确认页面或响应里到底有没有数据）
2. 再看 `*_parsed.json` / `*_items.json` / `*_chapters.json` / `*_content.json`（这是解析结果）
3. 只看 JSON 结果不看源文件，不允许直接下结论

## 5. 缓存与调试输出

每次运行都会生成调试目录：

- `debug_logs/YYYYMMDD/single_source_func_HHMMSS/`

主要文件（原始响应文件是 `raw_body` 概念，可能是 HTML、JSON 或纯文本）：

1. 搜索阶段：`01_search_raw_body.*` / `01_search_items.json`
2. 详情阶段：`02_detail_raw_body.*` / `02_detail_parsed.json`
3. 目录阶段：`03_toc_raw_body.*` / `03_toc_chapters.json`
4. 正文阶段：`04_chapter_raw_body.*` / `04_chapter_content.json`

说明：

1. `*` 后缀不固定，可以是 `.txt` / `.html` / `.json`
2. 复核时以文件实际内容为准，不以后缀名判断
3. 排查字段为空时，第一时间打开 `*_raw_body.*`（常见是 HTML），确认源里是否有目标字段
4. 只有在源文件确认“有数据”的前提下，才用对应 JSON 结果判断解析是否正确

这些文件是“失败复盘”的唯一依据，不能删。

## 6. 当前约束

1. 测试基于“第一本书 + 第一章”做链路验证，不覆盖全量章节正确性
2. 某些“搜书专用 API 源”没有传统详情页，依赖 TOC 回退逻辑
3. 如果需要做“上游无数据自动软通过”，建议后续在测试里新增显式开关（例如 `ALLOW_UPSTREAM_EMPTY_PASS=1`），避免默认行为太宽松

## 7. 后续建议（可选）

1. 增加“证据复核自动判定”实现（自动扫描 raw 内容是否存在关键字段）
2. 区分 `FAIL(解析错误)` 与 `SKIP(上游无数据)` 两种结果
3. 新增 sourceIndex 定向运行能力，方便批量回归时精准复现
