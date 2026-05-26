let config = { profiles: [], activeProfileId: "", port: 0 };

const $ = (id) => document.getElementById(id);
const profileSelect = $("profileSelect");
const profileName = $("profileName");
const processList = $("processList");
const processSearch = $("processSearch");
const searchResults = $("searchResults");
const logEl = $("log");
const statusEl = $("status");

function activeProfile() {
  return config.profiles.find((p) => p.id === config.activeProfileId);
}

function syncEditorFromProfile() {
  const p = activeProfile();
  if (!p) return;
  profileName.value = p.name || "";
  processList.value = (p.processes || []).join("\n");
}

function syncProfileFromEditor() {
  const p = activeProfile();
  if (!p) return;
  p.name = profileName.value.trim() || "未命名";
  p.processes = processList.value
    .split("\n")
    .map((s) => s.trim())
    .filter(Boolean);
}

function renderProfiles() {
  profileSelect.innerHTML = "";
  config.profiles.forEach((p) => {
    const opt = document.createElement("option");
    opt.value = p.id;
    opt.textContent = p.name || "未命名";
    if (p.id === config.activeProfileId) opt.selected = true;
    profileSelect.appendChild(opt);
  });
  syncEditorFromProfile();
}

async function loadConfig() {
  const res = await fetch("/api/config");
  config = await res.json();
  renderProfiles();
}

async function saveConfig() {
  syncProfileFromEditor();
  const res = await fetch("/api/config", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(config),
  });
  if (!res.ok) throw new Error(await res.text());
  config = await res.json();
  renderProfiles();
  setStatus("配置已保存", "ok");
}

async function killProcesses() {
  syncProfileFromEditor();
  await fetch("/api/config", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(config),
  });

  setStatus("正在结束进程…", "");
  logEl.textContent = "";

  const res = await fetch("/api/kill", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ profileId: config.activeProfileId }),
  });
  const data = await res.json();
  if (!res.ok) {
    setStatus("执行失败", "err");
    return;
  }

  const lines = (data.results || []).map((r) => {
    const cls = r.success ? "ok" : "fail";
    const mark = r.success ? "✓" : "✗";
    return `<span class="${cls}">${mark} ${r.name}: ${r.message}</span>`;
  });
  logEl.innerHTML = lines.join("\n") || "(无进程名)";
  setStatus(data.summary || "完成", "ok");
}

function setStatus(msg, kind) {
  statusEl.textContent = msg;
  statusEl.className = "status" + (kind ? " " + kind : "");
}

function newId() {
  return crypto.randomUUID ? crypto.randomUUID() : String(Date.now());
}

profileSelect.addEventListener("change", () => {
  syncProfileFromEditor();
  config.activeProfileId = profileSelect.value;
  syncEditorFromProfile();
});

$("btnSave").addEventListener("click", () => saveConfig().catch((e) => setStatus(e.message, "err")));
$("btnKill").addEventListener("click", () => killProcesses().catch((e) => setStatus(e.message, "err")));

$("btnAdd").addEventListener("click", () => {
  syncProfileFromEditor();
  const id = newId();
  config.profiles.push({ id, name: "新配置", processes: [] });
  config.activeProfileId = id;
  renderProfiles();
});

$("btnDelete").addEventListener("click", () => {
  if (config.profiles.length <= 1) {
    setStatus("至少保留一套配置", "err");
    return;
  }
  const idx = config.profiles.findIndex((p) => p.id === config.activeProfileId);
  if (idx < 0) return;
  config.profiles.splice(idx, 1);
  config.activeProfileId = config.profiles[0].id;
  renderProfiles();
  saveConfig().catch(() => {});
});

function escapeHtml(s) {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function linesInTextarea() {
  return processList.value
    .split("\n")
    .map((s) => s.trim().toLowerCase())
    .filter(Boolean);
}

function addProcessToList(name) {
  const key = name.trim().toLowerCase();
  if (!key) return false;
  const normalized = key.endsWith(".exe") ? key : key + ".exe";
  const existing = linesInTextarea();
  const compare = normalized.replace(/\.exe$/i, "");
  if (existing.some((line) => {
    const base = line.replace(/\.exe$/i, "");
    return base === compare || line === normalized;
  })) {
    return false;
  }
  const display = name.trim();
  const text = processList.value.trim();
  processList.value = text ? text + "\n" + display : display;
  syncProfileFromEditor();
  return true;
}

function renderSearchResults(items) {
  searchResults.innerHTML = "";
  if (!items || items.length === 0) {
    const li = document.createElement("li");
    li.className = "empty";
    li.textContent = "未找到匹配的进程";
    searchResults.appendChild(li);
    searchResults.classList.remove("hidden");
    return;
  }
  items.forEach((item) => {
    const li = document.createElement("li");
    const countLabel = item.count > 1 ? ` ×${item.count}` : "";
    li.innerHTML =
      `<span class="name">${escapeHtml(item.name)}</span>` +
      `<span class="meta">PID ${item.pid}${countLabel}</span>`;
    li.addEventListener("click", () => {
      if (addProcessToList(item.name)) {
        setStatus(`已添加: ${item.name}`, "ok");
      } else {
        setStatus(`已在列表中: ${item.name}`, "");
      }
    });
    searchResults.appendChild(li);
  });
  searchResults.classList.remove("hidden");
}

async function searchProcesses() {
  const q = processSearch.value.trim();
  setStatus("正在搜索…", "");
  const res = await fetch("/api/search?q=" + encodeURIComponent(q));
  if (!res.ok) throw new Error(await res.text());
  const data = await res.json();
  renderSearchResults(data.items || []);
  const n = (data.items || []).length;
  setStatus(n ? `找到 ${n} 个进程` : "无匹配结果", n ? "ok" : "");
}

$("btnSearch").addEventListener("click", () =>
  searchProcesses().catch((e) => setStatus(e.message, "err"))
);
processSearch.addEventListener("keydown", (e) => {
  if (e.key === "Enter") {
    e.preventDefault();
    searchProcesses().catch((err) => setStatus(err.message, "err"));
  }
});

loadConfig().catch((e) => setStatus("加载失败: " + e.message, "err"));
