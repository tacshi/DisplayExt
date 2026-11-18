<p align="center">
  <img src="docs/images/logo.png" width="128" height="128" alt="DisplayExt">
</p>

<h1 align="center">DisplayExt</h1>

<p align="center">
  一款小巧的 macOS 菜单栏应用：创建虚拟显示器，<br>
  并在 UU远程 会话连接时自动挂载。
</p>

<p align="center">
  <a href="README.md">English</a> · <b>简体中文</b>
</p>

<p align="center">
  <a href="https://github.com/tacshi/DisplayExt/actions/workflows/ci.yml"><img src="https://github.com/tacshi/DisplayExt/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/tacshi/DisplayExt/releases/latest"><img src="https://img.shields.io/github/v/release/tacshi/DisplayExt" alt="最新版本"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue" alt="MIT 许可证"></a>
</p>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/images/settings-dark.zh-Hans.png">
  <img src="docs/images/settings-light.zh-Hans.png" alt="DisplayExt 设置窗口">
</picture>

## 为什么做它

用 iPad Pro 通过 UU远程 控制 Mac 时，如果 Mac 上有一块和 iPad 形状一致的显示器（4:3、Retina
级清晰度），体验会好很多。BetterDisplay 等应用可以创建这样的显示器，但同时附带许多你可能用不到的功能。
DisplayExt 只做这一件事，并且全部在菜单栏里完成。

## 功能

- **与连接所用的设备同形。** 从约 800 款 iPad 和手机中按名称选择，横向或竖向，显示器便是它的形状；
  手机上会为 UU远程 的工具栏留出位置。其他屏幕可选 4:3、16:10、16:9、3:2、21:9，或任意自定义比例。
- **HiDPI。** 显示为 2048 × 1536，以 4096 × 3072 像素渲染。macOS 无法提供 HiDPI 的过小尺寸不会列出。
- **任意刷新率**，范围 1–240 Hz，并提供最高 144 Hz 的预设。
- **随 UU远程 挂载。** 显示器可以跟随 UU远程 会话：会话连接时挂载，会话结束几秒后卸载。
- **可在会话期间关闭其他屏幕。** UU远程 会话连接且显示器已挂载时，可以让这台 Mac 的其他屏幕变黑，窗口会移到虚拟显示器上。会话结束时屏幕会恢复，DisplayExt 意外退出时也会。没有会话时挂载显示器，其他屏幕保持开启，你仍然看得到自己在做什么。
- **常驻菜单栏。** 在菜单中挂载、卸载显示器；有显示器挂载时图标会变为实心。不占用程序坞。
- **登录时打开**，支持英文和简体中文，跟随浅色与深色模式。
- **小巧。** 纯 Go 编写，约 10 MB，基于 [MyGo](https://github.com/egoist/mygo) 构建。

## 系统要求

- macOS 13 或更高版本。已在 Apple 芯片的 macOS 27 上测试。
- 随会话自动挂载和关闭其他屏幕需要 UU远程 4.47 或更高版本。使用其他远程软件时，可以在菜单中手动挂载，这台 Mac 的屏幕会保持开启。

## 安装

1. 从[最新版本](https://github.com/tacshi/DisplayExt/releases/latest)下载
   `DisplayExt-<版本>-universal.dmg`。它适用于所有 Mac；`arm64`（Apple 芯片）和 `amd64`（Intel）版本体积更小。
2. 打开磁盘映像，把 DisplayExt 拖到“应用程序”文件夹。

发布版本均使用 Developer ID 签名并经过 Apple 公证。每个版本都在 `SHA256SUMS` 中列出了磁盘映像的校验和。

DisplayExt 会自动保持最新：启动几秒后及此后每 4 小时检查一次新版本，并在后台下载。下载完成后，菜单中会出现
**重启以更新**。在菜单中选择 **检查更新…** 可立即检查并查看发布说明。

## 使用

1. 打开 DisplayExt，它的图标会出现在菜单栏中；选择 **设置…**。
2. 首次启动时会添加一个名为 **iPad** 的显示器，对应横向的 13 英寸 iPad Pro：显示为 2048 × 1536，
   HiDPI，60 Hz，挂载方式为 **随 UU远程**。在 **设备** 中选择你自己的设备，或用 **添加显示器** 再添加更多。
3. 在 iPad 上用 UU远程 连接这台 Mac。DisplayExt 会在几秒内挂载显示器；在 UU远程 的屏幕切换中选择它即可。
   断开连接后，显示器会被卸载。
4. 想手动挂载时，在菜单中勾选显示器，或在设置中点按 **挂载**。挂载方式为 **手动** 的显示器会在下次启动时恢复之前的挂载状态。

在 **设备** 中可以按名称搜索，例如“ipad pro 11”或“17 pro max”，也可以按品牌搜索，例如“华为”。
手机请选择 **竖向**，显示器会恰好落在 UU远程 的工具栏之间。

<img src="docs/images/settings-picker.zh-Hans.png" alt="选择设备">

在“通用”页面中可以打开 **登录时打开**、选择界面语言，并查看 UU远程 会话是否已连接。

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/images/general-dark.zh-Hans.png">
  <img src="docs/images/general-light.zh-Hans.png" alt="“通用”页面">
</picture>

## 工作原理

- **虚拟显示器。** macOS 没有创建显示器的公开 API。DisplayExt 与 BetterDisplay、DeskPad 以及
  UU远程 本身一样，使用 CoreGraphics 中私有的 `CGVirtualDisplay` 类，并通过
  [purego](https://github.com/ebitengine/purego) 在纯 Go 中调用，无需 cgo。
- **每个显示器一个进程。** 每个已挂载的显示器都运行在一个小的辅助进程中（即以辅助模式启动的应用自身）。
  卸载时结束该进程，显示器总会随之移除，即使 macOS 已经改变过它的显示模式；DisplayExt 退出或崩溃时，
  它创建的显示器也会一并消失。
- **UU远程 会话。** macOS 没有面向第三方应用的全局“远程会话”状态。会话连接期间，UU远程 的
  `UURemoteServer` 会持有一个名为 `UURemote Disable Display Sleep` 的电源断言；DisplayExt 每两秒检查一次，
  并在断言消失五秒后才卸载显示器，因此短暂的重连不会影响显示器。它还会像 [Snug](https://github.com/zephyr4123/snug)
  一样跟踪系统日志中 `UURemoteServer` 的屏幕捕获：串流一开始就算作会话，设置中会显示 UU远程 正在显示哪块屏幕，
  若不是虚拟显示器，会提示在 UU远程 中选择它。会话中途打开 DisplayExt 时，会读取最近四小时的日志以接上状态。
- **其他屏幕。** DisplayExt 并不真正关闭这台 Mac 的屏幕，而是让它们镜像虚拟显示器并显示为全黑。
  屏幕一旦消失，绑定在它上面的屏幕录制就会结束；UU远程 通过会话连接时的那块屏幕录制 Mac 的声音，
  屏幕改为镜像后，iPad 上的声音就不会中断。会话开始和结束时，辅助进程在镜像与正常之间切换，无需重新挂载显示器。

## 须知

- 私有 API 可能在任何 macOS 版本中发生变化。届时 DisplayExt 会在菜单和设置中提示不可用，而不会崩溃。
- UU远程 的会话信号同样不是公开接口，目前在 UU远程 4.47 上验证过；运行 `go run ./cmd/uuwatch`
  可以查看 DisplayExt 检测到的状态。
- macOS 会按显示器的标识记住它的排列位置和分辨率。DisplayExt 为每个显示器保留固定的标识，
  所以它总会回到你摆放的位置。macOS 还会为每个标识在 `/Library/ColorSync/Profiles/Displays`
  中写入一个颜色描述文件。
- 设置保存在 `~/Library/Application Support/DisplayExt/config.json`，日志位于
  `~/Library/Logs/DisplayExt/DisplayExt.log`。如果设置文件无法读取，DisplayExt
  会把它另存为同目录下的 `config-damaged-<日期>.json`，并恢复默认设置。
- 挂载失败的显示器不会自动重试，直到你将它关闭再打开、修改它的设置，或开始新的 UU远程 会话。

## 从源码构建

需要 macOS 和 Go 1.27 或更高版本。

```bash
git clone https://github.com/tacshi/DisplayExt.git
cd DisplayExt
make test
make debug
```

`make debug` 会构建 `build/mac-debug/DisplayExt Debug.app`，它可以与正式版同时运行，并拥有独立的设置和显示器。

| 命令 | 作用 |
|---|---|
| `make check` | gofmt、golangci-lint、govulncheck、gosec，以及脚本语法检查 |
| `make test` | 带竞态检测的测试，以及 `mygo vet` |
| `make debug` | 构建 ad hoc 签名的 DisplayExt Debug（Intel 用 `ARCH=x64`） |
| `make release` | 构建 arm64、amd64 和 universal 的已签名、已公证磁盘映像（`VERSION=`、`ARCH=`） |
| `make publish` | 构建、打标签并在 GitHub 上发布版本（`VERSION=`、`DRY_RUN=1`） |
| `make icons` | 从 `design/` 渲染应用图标和菜单栏图标 |
| `make screenshots` | 渲染 `docs/images` 中的图片 |
| `make clean` | 删除 `build/` |

`cmd/vdspike` 用于检测当前 Mac 接受哪些创建虚拟显示器的方式，在新版 macOS 上应首先运行它。

## 发布

发布版本在钥匙串中装有 Developer ID 的 Mac 上构建、签名并公证，然后发布到 GitHub：

```bash
make publish VERSION=0.2.0 DRY_RUN=1
make publish VERSION=0.2.0
```

[`scripts/publish.sh`](scripts/publish.sh) 会检查提交位于 `main`、已推送且工作区干净，版本号也晚于上一个版本。
它构建磁盘映像，连同 `SHA256SUMS` 以及已安装副本用来更新的已签名更新归档和清单上传到草稿版本，然后为该提交打标签、推送标签并正式发布。
发布说明取自 `CHANGELOG.md` 中该版本的章节，没有时则根据提交记录起草并请你确认。试运行只做检查，并显示发布说明和将执行的步骤。

它使用钥匙串中的 Developer ID Application 身份（或 `DEVELOPER_ID_APPLICATION`）签名，
并使用 notarytool 钥匙串配置 `DisplayExt`（或 `DISPLAYEXT_NOTARY_PROFILE`）公证。该配置只需创建一次：

```bash
xcrun notarytool store-credentials DisplayExt --apple-id <id> --team-id <team>
```

更新使用 `~/Library/Application Support/mygo/update-keys/DisplayExt/mygo-update.key` 中的密钥签名，
也可以用 `DISPLAYEXT_UPDATE_KEY` 指定文件，或设置 `MYGO_UPDATER_PRIVATE_KEY`。它的公钥是
[`mygo.json`](mygo.json) 中的 `updates.publicKey`。已安装的副本只接受用它签名的更新，请妥善备份。调试版本不会自动更新。

## 翻译

界面文字位于 [`locales/en.json`](locales/en.json) 和
[`locales/zh-Hans.json`](locales/zh-Hans.json)，采用 ICU MessageFormat。添加新语言只需新增对应的目录文件，
再用 `go tool mygo i18n check` 检查。

## 致谢

虚拟显示器的实现参考了 [go-macos/virtualdisplay](https://github.com/go-macos/virtualdisplay)
（BSD-3-Clause）和 Chromium 的 `virtual_display_util_mac.mm`。设备列表、为 UU远程 工具栏留出的位置和
HiDPI 的尺寸下限来自 [Snug](https://github.com/zephyr4123/snug)（MIT），其许可证见
[`internal/devices/NOTICE`](internal/devices/NOTICE)。应用基于 [MyGo](https://github.com/egoist/mygo) 构建。

## 许可证

[MIT](LICENSE) © 2026 Rocky Shi
