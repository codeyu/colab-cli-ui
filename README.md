<p align="center"><img src="resources/icon.svg" width="96" alt="Colab CLI UI"></p>

# Colab CLI UI

[google-colab-cli](https://github.com/googlecolab/google-colab-cli)（`colab` 命令）的桌面图形客户端：申请 CPU / GPU / TPU 运行时、执行代码、管理文件、从 HuggingFace 选模型，用完一键释放。基于 [MyGo](https://github.com/egoist/mygo)（Go + 系统 WebView）构建，界面风格参考 t3code，功能组织参考 EasyColab。

> 非 Google 官方软件。目前只在 Windows 11 上构建和测试过；google-colab-cli 官方只支持 Linux 和 macOS，但在 Windows 上实测可用。

## 安装与首次使用

1. 构建安装包（见下文「开发」），运行 `build/windows-amd64/colab-cli-ui-setup-<版本>-amd64.exe`；也可以直接运行同目录下的 `Colab CLI UI.exe`。需要 WebView2 运行时（Windows 11 自带）。
2. 第一次打开会显示三步引导，已完成的步骤会自动跳过：
   1. **安装 uv**：给出官方安装命令，复制到 PowerShell 运行，再点「重新检测」。
   2. **安装 Colab CLI**：点「安装」，软件执行 `uv tool install google-colab-cli`。
   3. **登录 Google**：点「登录 Google」，在浏览器里同意授权，把页面上的授权码粘贴回软件。也可以改用 gcloud 的 ADC。
3. 进入主界面后，点「新建会话」申请虚拟机。**GPU / TPU 按小时消耗计算单元，用完记得「释放」。**

## 功能

| 界面 | 对应的 CLI 命令 |
| --- | --- |
| 新建会话：CPU / T4 / L4 / G4 / A100 / H100 / TPU v5e-1 / v6e-1，可选高内存；账号无资格的加速器会变灰 | `colab new -s NAME --gpu/--tpu --high-mem` |
| 启动脚本：从历史记录里挑选步骤保存，新建会话时选择，开机后自动执行（需要时先挂载 Drive） | `colab exec` |
| 侧栏会话列表（含服务器上未在本机登记的运行时），状态自动刷新 | `colab status`、`colab sessions` |
| 计算单元余额、每小时消耗、可用的加速器 | `colab usage`（读取服务器 ccu-info 的完整回复） |
| 控制台：Python / Shell 流式输出、ANSI 颜色、matplotlib 图片内联显示，可中止；长时间无输出也不会超时 | `colab exec --timeout 0 -s NAME`（Shell 模式用 `%%bash`） |
| 运行本地 `.py` / `.ipynb` | `colab exec -s NAME -f FILE` |
| 模型：搜索 HuggingFace，勾选文件（默认只选 PyTorch 权重；GGUF 默认 Q4_K_M），下载到 VM，插入加载代码 | `colab exec`（huggingface_hub） |
| 文件浏览器：浏览、上传（支持拖拽）、下载、删除 | `colab ls / upload / download / rm` |
| 包管理：安装包或 requirements.txt | `colab install` |
| 资源监控：CPU、内存、磁盘、GPU 显存 / 利用率 / 温度 / 功耗曲线 | `colab exec`（psutil + nvidia-smi 采样） |
| 历史记录：浏览、导出（ipynb / md / txt / jsonl）、单条重新执行 | 读取 `~/.config/colab-cli/history`，`colab log -o` |
| 在 Colab 网页中打开会话 | `colab url` |
| REPL / SSH / 挂载 Drive / VM 端 GCP 授权（在新终端窗口中打开） | `colab repl / ssh / drivemount / auth` |
| 重启内核、释放会话、全部释放 | `colab restart-kernel`、`colab stop [--all]` |
| 一次性任务：新建 VM → 运行脚本 → 自动释放 | `colab run` |
| CLI 命令日志（Ctrl+\`）：界面执行过的每条命令及其输出 | — |
| 设置：colab 路径、`--auth oauth2/adc`、`--config`、`-c`、HuggingFace token 与搜索地址、启动脚本管理 | 全局参数 |

## 数据与凭据

- **Google 登录令牌**由 CLI 自己保存在系统凭据管理器（Windows 凭据管理器）中，本软件不读取。
- **HuggingFace token** 保存在系统凭据管理器中。下载需授权的模型时，它以文件形式经 `colab upload` 传给 VM，不会出现在会话历史里（`colab exec` 执行的代码会被写进历史）。
- **设置与启动脚本**保存在 `%APPDATA%\Colab CLI UI\` 下（`settings.json`、`startup-scripts.json`）。
- 界面里的 CLI 命令日志会隐藏含凭据的调试行；CLI 自己的 `~/.config/colab-cli/colab.log` 也会把令牌替换成 `REDACTED`。

## 开发

需要 Go 1.27+、Bun，以及已安装的 `colab`。

```sh
bun install
bun run dev        # 实时重载开发
bun run build      # 打包到 build/windows-amd64/（exe + 安装程序）
go test ./...      # 后端测试：输出解析、流式输出、脚本生成、凭据隐藏
COLAB_UI_LOGIN_TEST=1 go test -run TestLoginFlowSignedOut ./...
                   # 用真实 CLI 测登录流程：临时主目录 + 空凭据存储，不读也不改你的登录
```

改了 `resources/icon.svg` 后重新生成 PNG（需要带 librsvg 的 ImageMagick）：

```sh
magick -background none RSVG:resources/icon.svg -resize 1024x1024 -depth 8 PNG32:resources/icon.png
```

## 结构

- `main.go`：应用与窗口
- `colab.go`：绑定给前端的 `Colab` 服务（会话、执行、文件、监控、历史）
- `onboarding.go`：首次使用引导（检测 uv、安装 CLI、在软件内登录）
- `hf.go`：HuggingFace 搜索、模型信息、下载到 VM、token
- `scripts.go`：启动脚本的存储
- `runner.go`：调用子进程、流式回传输出、识别 `[Image saved to: …]` 图片、隐藏凭据
- `settings.go`：偏好设置
- `proc_*.go`：隐藏子进程控制台窗口、在新终端中运行交互式命令
- `resources/icon.svg`：图标源文件（`icon.png` 由它生成）
- `src/`：React + Tailwind 前端；`src/mygo.ts` 由 `mygo generate` 从 Go 代码生成
