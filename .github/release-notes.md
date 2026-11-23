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
