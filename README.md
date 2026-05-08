# DownGo

基于 Windows 的本地网页下载服务，使用 `yt-dlp` 下载 YouTube 视频。

## 技术栈

- 后端：Go
- 前端：Vue 3 + Ant Design Vue
- 数据库：SQLite

## 运行方式

1. 在可执行文件同级目录的 `data/bin/` 中放入以下文件：
   - `yt-dlp.exe`
   - `ffmpeg.exe`
   也可以启动后在“系统设置”页点击“下载缺失依赖”自动补齐默认目录下的工具文件。
2. 启动 `DownGo.exe`。
3. 程序会驻留在系统托盘中运行，不再显示控制台窗口。
4. 首次启动时，初始密码会写入 `data/logs/downgo.log`。
5. 点击托盘菜单中的访问地址，或在浏览器中打开 `http://127.0.0.1:12225` / `http://<局域网IP>:12225`。

## 开发

后端：

```powershell
go test ./...
go build -ldflags="-H windowsgui" -o DownGo.exe ./cmd/server
```

前端：

```powershell
cd frontend
npm install
npm run build
```

前端生产资源会输出到 `webui/dist`，并嵌入到 Go 二进制中。

## 日志

- 日志目录：`data/logs/`
- 默认日志文件：`data/logs/downgo.log`
- 托盘菜单可直接打开日志目录

## 打包

Windows 下一键打包：

```powershell
.\build-release.ps1
```

或直接双击 / 运行：

```powershell
.\build-release.cmd
```

脚本会执行：

1. 构建前端资源。
2. 运行 `go test ./...`。
3. 构建 `DownGo.exe`。
4. 组装 `dist/DownGo-<timestamp>-win-amd64/`。
5. 在 `dist/` 中生成 zip 压缩包。

可选参数：

```powershell
.\build-release.ps1 -Version 1.0.0
.\build-release.ps1 -SkipTests
.\build-release.ps1 -SkipZip
```

如果本地已存在 `data/bin/yt-dlp.exe` 和 `data/bin/ffmpeg.exe`，脚本会自动复制到打包目录；否则会创建 `dist/.../data/bin/README.txt` 作为占位说明。
