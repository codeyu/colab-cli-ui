# Colab CLI UI

[google-colab-cli](https://github.com/googlecolab/google-colab-cli)（`colab` 命令）的桌面图形客户端，基于 [MyGo](https://github.com/egoist/mygo)（Go + 系统 WebView）构建，界面风格参考 t3code，功能组织参考 EasyColab。

## 功能

| 界面 | 对应的 CLI 命令 |
| --- | --- |
| 新建会话：CPU / T4 / L4 / G4 / A100 / H100 / TPU v5e-1 / v6e-1，可选高内存 | `colab new -s NAME --gpu/--tpu --high-mem` |
| 侧栏会话列表（含服务器上未在本机登记的运行时），状态自动刷新 | `colab status`、`colab sessions` |
| 计算单元余额与每小时消耗 | `colab usage` |
| 控制台：Python / Shell 代码流式输出、ANSI 颜色、matplotlib 图片内联显示，可中止 | `colab exec -s NAME`（Shell 模式使用 `%%bash`） |
| 运行本地 `.py` / `.ipynb` | `colab exec -s NAME -f FILE` |
| 模型：搜索 HuggingFace，勾选文件（GGUF 默认 Q4_K_M），下载到 VM，插入加载代码 | `colab exec`（huggingface_hub）；token 经 `colab upload` 传给 VM，不进历史 |
| 文件浏览器：浏览、上传（支持拖拽）、下载、删除 | `colab ls / upload / download / rm` |
| 包管理：安装包或 requirements.txt | `colab install` |
| 资源监控：CPU、内存、磁盘、GPU 显存/利用率/温度/功耗曲线 | `colab exec`（psutil + nvidia-smi 采样） |
| 历史记录浏览与导出（ipynb / md / txt / jsonl） | 读取 `~/.config/colab-cli/history`，`colab log -o` |
| 在 Colab 网页中打开会话 | `colab url` |
| REPL / SSH / 挂载 Drive / VM 端 GCP 授权（在新终端窗口中打开） | `colab repl / ssh / drivemount / auth` |
| 重启内核、释放会话、全部释放 | `colab restart-kernel`、`colab stop [--all]` |
| 一次性任务：新建 VM → 运行脚本 → 自动释放 | `colab run` |
| CLI 命令日志（Ctrl+\`）：界面执行过的每条命令及其输出 | — |
| 设置：colab 路径、`--auth oauth2/adc`、`--config`、`-c` | 全局参数 |

## 开发

需要 Go 1.27+、Bun，以及已安装的 `colab`（`uv tool install google-colab-cli`）。

```sh
bun install
bun run dev        # 实时重载开发
bun run build      # 打包到 build/windows-amd64/（exe + 安装程序）
go test ./...      # 后端解析与输出流测试
```

## 结构

- `main.go` — 应用与窗口
- `colab.go` — 绑定给前端的 `Colab` 服务（每个方法对应一组 CLI 操作）
- `runner.go` — 调用 `colab` 子进程、流式回传输出、识别 `[Image saved to: …]` 图片
- `settings.go` — 偏好设置（存放在用户数据目录）
- `proc_*.go` — 隐藏子进程控制台窗口、在新终端中运行交互式命令
- `src/` — React + Tailwind 前端；`src/mygo.ts` 由 `mygo generate` 从 Go 代码生成
