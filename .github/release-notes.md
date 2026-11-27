## 本版更新

- **多账号**：添加多个 Google 账号，一键切换，同时查看各账号的余额和运行中的虚拟机。
- **模型 API**：在 VM 上用 Ollama 一键部署模型，得到 OpenAI 兼容 API 并转发到本机。
- **端口转发**：把 VM 上的 Gradio、ComfyUI 等服务转发到本机 `127.0.0.1`。
- **托盘与通知**：托盘显示运行中的虚拟机和花费；长任务在后台结束时弹出系统通知。
- **会话花费**：每个会话显示已运行时长和大约花了多少 CU。
- 登录时 Google 授权页没勾选全部权限，会给出明确提示。

## 下载

| 系统 | 文件 |
| --- | --- |
| Windows（x64 / ARM） | `colab-cli-ui-setup-<版本>-amd64.exe` / `-arm64.exe` 安装程序 |
| macOS（Apple 芯片 / Intel） | `colab-cli-ui-<版本>-arm64.dmg` / `-amd64.dmg` |
| Linux（x64 / ARM） | `.deb`、`.rpm`，或 `.tar.gz` 配合 `install.sh`（`sh install.sh`，装到当前用户） |

第一次打开时，软件会引导你安装 uv 和 google-colab-cli，并登录 Google 账号。

**安装包没有代码签名**，首次打开会有系统提示：

- Windows：SmartScreen 提示时，点「更多信息」→「仍要运行」。
- macOS：系统提示无法验证开发者时，在终端运行 `xattr -cr "/Applications/Colab CLI UI.app"` 后再打开。

只在 Windows 11 上测试过；macOS 和 Linux 版本由同一份代码构建，但未经实测。
