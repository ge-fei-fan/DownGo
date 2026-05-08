# DownGo Build Guide

## 目标

构建 Windows GUI 可执行文件 `DownGo.exe`，并确保最新前端资源已经嵌入二进制。

## 环境要求

- Windows
- Go
- Node.js + npm

## 构建前准备

1. 安装前端依赖：

```powershell
cd frontend
npm install
```

2. 返回仓库根目录：

```powershell
cd ..
```

## 标准构建命令

1. 构建前端资源：

```powershell
cd frontend
npm run build
cd ..
```

前端产物输出到 `webui/dist/`，Go 程序会通过嵌入资源的方式将其打进 EXE。

2. 构建 Windows 可执行文件：

```powershell
go build -buildvcs=false -ldflags="-H windowsgui" -o DownGo.exe ./cmd/server
```

说明：

- `-H windowsgui` 用于隐藏控制台窗口。
- `-buildvcs=false` 用于避免在受限或非 safe.directory 环境下因 VCS stamping 失败而中断构建。

## 当前已验证产物

- 输出文件：`DownGo.exe`
- 输出路径：仓库根目录
- SHA256：`5CFF5C66F7F50B95FE2B820898FE5F10FE674603C6E5BD729A6A937F830FB76F`

## 运行依赖

程序启动后默认从以下位置查找下载依赖：

- `data/bin/yt-dlp.exe`
- `data/bin/ffmpeg.exe`

如果缺失，也可以在程序设置页中下载依赖。

## 建议的验证步骤

```powershell
go test ./...
```

然后启动：

```powershell
.\DownGo.exe
```

验证项：

- 可以正常打开下载页面
- 下载列表实时刷新
- 删除进行中的任务时会中断下载并移除记录
- 失败任务保留且可以重试

## 常见问题

### `error obtaining VCS status`

使用下面的命令构建：

```powershell
go build -buildvcs=false -ldflags="-H windowsgui" -o DownGo.exe ./cmd/server
```

### 前端构建成功但 EXE 仍是旧页面

先执行：

```powershell
cd frontend
npm run build
cd ..
```

再重新执行 Go 构建。因为 EXE 只会嵌入 `webui/dist/` 中最新的前端产物。
