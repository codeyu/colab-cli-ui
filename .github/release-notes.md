## 本版更新

- **任务库**：把常做的事保存成任务，一键运行——自动开机、上传文件、在 VM 后台处理、把结果下载到本地、释放虚拟机。可以新建、编辑、导入导出任务。
- **批量语音识别**：内置任务，把一批音频或视频转成文字和字幕（txt / srt / vtt / json），可选 Whisper large-v3-turbo、large-v3、Kotoba-Whisper（日语）、SenseVoice（中日韩粤英）等模型，GPU 和 CPU 都能跑。
- **创建失败自动重试**：Colab 暂时没有资源（如 G4、A100 抢不到）时自动重试，次数和间隔在设置里或新建会话时选择。
- 文件可以直接拖到任务运行窗口里；大文件改用 SSH 传输。

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
