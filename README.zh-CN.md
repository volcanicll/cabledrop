<p align="center">
  <img src="docs/icon-256.png" width="88" alt="CableDrop icon">
</p>

<h1 align="center">CableDrop</h1>

<p align="center">
  用一根 USB 线在电脑和 Android 手机之间传文件、传文本。<br>
  不经过网络。不经过云。手机上什么都不用装。
</p>

<p align="center">
  <a href="README.md">English</a> ·
  <a href="#30-秒上手">30 秒上手</a> ·
  <a href="#排障">排障</a> ·
  <a href="CONTRIBUTING.md">参与开发</a>
</p>

---

## 能做什么

| | |
|---|---|
| ![](docs/panel-dark.png) | ![](docs/phone-light.png) |
| **电脑端面板** —— macOS 菜单栏,380×540 | **手机端网页** —— 任意手机浏览器打开 |

- **文件双向**。拖文件到面板或点选发送;在手机上直接浏览电脑的共享目录(含子文件夹)并下载。手机里的文件也能一键取回到电脑。
- **剪贴板双向**。电脑复制,手机打开网页一键粘贴;手机上输入,直接进电脑剪贴板。另有独立「便签」通道,发文本不会覆盖你正在用的剪贴板。
- **真实进度**。多文件批次按字节数给百分比;adb 不吐流式进度时显示已用时间;手机上传是真实百分比。

所有数据走 USB 线上的 `adb reverse` 隧道:电脑只监听 `127.0.0.1`,手机通过 `localhost:8765` 访问。不弹防火墙,不在局域网暴露任何东西。

## 30 秒上手

1. **装 adb** —— [Android Platform Tools](https://developer.android.com/tools/releases/platform-tools)(`brew install android-platform-tools`),或把 `platform-tools` 文件夹放到程序同目录。
2. **构建**(macOS):
   ```bash
   make app          # CableDrop.app —— 菜单栏应用,无 Dock 图标
   open CableDrop.app
   ```
3. **插线**,手机把 USB 用途改成**传输文件(MTP)**。
4. 手机浏览器打开 **http://localhost:8765** —— 这就是完整的手机端界面。

就这么简单。面板显示连接状态;手机端和面板共用同一套 API,只是换了移动布局。

### 各平台构建

```bash
make build     # 本机二进制
make app       # macOS .app(菜单栏,LSUIElement)
make windows   # Windows exe(从 macOS 交叉编译,无 cgo)
make linux     # Linux(需要 GTK3 + WebKitGTK 4.1)
make apk       # Android APK(可选,需要 Android SDK + JDK 17+)
```

### APK(可选)

手机端用浏览器就够。APK 是一个 858 KB 的 WebView 壳,打开的还是同一个页面,额外做了三件浏览器做不到的事:上传走系统文件选择器、下载自动进系统「下载」且中文文件名正常、断线时有明确的重连界面。它不捆绑任何服务端 —— 和网页一样,插着线才能用。

```bash
make apk        # → dist/CableDrop.apk
```

## 原理

```
   手机                     USB 线                      电脑
┌─────────┐                                             ┌───────────────┐
│ 浏览器   │ ←── adb reverse tcp:8765 ───────────────── │ 127.0.0.1:8765│
└─────────┘                                             │  Go HTTP      │
                                                        └──────┬────────┘
                                            同一套 JSON API 同时喂 │
                                                        ┌──────┴────────┐
                                                        │  原生面板 UI   │
                                                        └───────────────┘
```

- 单个 Go 二进制;前端是纯 HTML/CSS/JS,`go:embed` 嵌入 —— 没有 npm,没有打包步骤。
- 剪贴板走网页而不是 `adb shell`:Android 10 起禁止 adb 写设备剪贴板,而 `localhost` 属于安全上下文,浏览器 Clipboard API 在那里可用。
- 设备在线状态走常驻的 `adb track-devices` 连接 —— 空闲时不 fork 任何进程。
- 图标由代码里的一个小型光栅器绘制,仓库里没有二进制美术资源。

## adb 从哪找

环境变量 `CABLEDROP_ADB` → 程序同目录 → 同目录 `platform-tools/` → Android Studio 的 SDK 路径 → `PATH`。

## 排障

| 现象 | 处理 |
|---|---|
| 面板显示「找不到 adb」 | 装 Platform Tools,然后点面板上的「重新检测手机」。查找路径见上一节。 |
| 面板显示「未连接手机」 | 插线后把手机的 USB 用途改成**传输文件(MTP)**。「仅充电」模式 adb 看不见。 |
| 手机打不开页面 | 面板必须显示「已连接」。如果提示隧道建立失败,拔插一次 USB 线,每次重连都会自动重建隧道。 |
| 手机上打开了电脑面板的样子 | 你开的是 `/panel` —— 那是桌面页。用 `/`。 |
| 8765 端口被别的程序占了 | CableDrop 会自动换端口,面板上显示的是实际地址。 |
| 上传的文件在手机上找不到 | 手机的拉取永远存到电脑的共享目录(默认 ~/CableDrop);电脑发送的文件落在手机的 Download 文件夹。 |

## 目录结构

```
main.go             入口:flag 分发与装配
internal/model      共享数据类型
internal/device     adb 封装、设备文件操作、路径白名单
internal/clipboard  桌面剪贴板(平台命令)
internal/picker     系统文件选择框(子进程实现)
internal/serve      HTTP API + 内嵌静态资源
internal/app        状态机(实现 serve.Backend)
internal/icon       代码绘制图标与光栅器
internal/ui         唯一直接调用 Wails 的包
android/            可选 APK 壳(纯平台 API)
```

## 状态与兼容

- macOS 12+(universal)、Windows 10+(amd64/arm64)、Linux(GTK3 + WebKitGTK 4.1)、Android 7.0+(APK)。
- 按当前 Platform Tools 的 adb 测试;过旧的 adb 会自动回退到低频轮询。

## 参与开发

欢迎 PR —— 先读 [CONTRIBUTING.md](CONTRIBUTING.md);里面的几条硬约束来自真实踩过的坑。

## 协议

[MIT](LICENSE)
