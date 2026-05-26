# 进程终结器 (kills)

Windows 托盘小程序：内嵌 Web 配置页，支持多套进程列表，一键 `taskkill` 结束进程。

## 功能

- 系统托盘：打开页面、重启、退出
- Web 界面：大文本框填写进程名（每行一个）
- 多套配置：新建 / 切换 / 删除 / 保存
- 配置持久化：与 `kills.exe` 同目录的 `kills-config.json`

## 构建

```powershell
cd d:\Code\TEST\kills
.\build.ps1
```

或：

```powershell
go build -ldflags="-H windowsgui -s -w" -o kills.exe .
```

## 使用

1. 运行 `kills.exe`，托盘出现红色图标
2. 右键托盘 → **打开页面**（默认 http://127.0.0.1:17890）
3. 选择或新建配置，输入进程名，点击 **Kill 全部进程**

## 说明

- 仅支持 Windows（使用 `taskkill`）
- 进程名会自动补全 `.exe` 后缀
- 修改端口等高级项可直接编辑 `kills-config.json` 后重启
