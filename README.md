# CableDrop

用一根 USB 线在电脑和 Android 手机之间传数据、传文本。**全程不经过网络** —— 电脑的外网出口可以一直是关的。

## 能做什么

- **文件**：电脑 → 手机，手机 → 电脑，双向
- **剪贴板**：电脑复制的内容，手机上打开网页一键取走；手机上粘贴文本，一键写进电脑剪贴板
- **手机当电脑的文件浏览器**：手机上直接看电脑共享目录里的文件并下载
- **拖拽发送**：把文件拖到电脑端面板上就发到手机

## 怎么用

1. USB 线插上，手机上把 USB 用途改成「传输文件」
2. 电脑上点菜单栏的 CableDrop 图标 → 面板弹出
3. 手机上打开 `http://localhost:8765` —— 这就是手机端的完整界面；或者装 APK（见下文）

手机端**不需要装任何东西**，网页和 APK 是同一个界面的两种打开方式。

### 面板操作

- 面板弹出后，**点击面板外的任何地方、按 `Esc`、或点右上角 ×** 都会收起面板；再点菜单栏图标也能收起
- 点「发送文件」或「更改」共享目录时，面板会自动让位给系统文件选择框，选完再弹回来
- 「共享目录 · 打开」直接在 Finder / 资源管理器里打开共享目录

## Android APK（可选）

手机端除了浏览器，也可以装一个 858 KB 的 APK：`make apk` 产物在 `dist/CableDrop.apk`，传到手机安装即可。

它就是一个 WebView 壳，打开的还是 `127.0.0.1:8765` 那个页面，额外做了三件浏览器做不到/做不好的事：

- 上传文件走系统文件选择器（`<input type=file>` 在 WebView 里本来是死的）
- 下载文件自动进系统「下载」应用，中文文件名正常
- 连不上时给出明确提示和端口设置，而不是白屏

APK 不捆绑任何服务端 —— 它仍然依赖电脑端的 CableDrop 通过 `adb reverse` 建隧道，所以**插着 USB 线才能用**，和网页一样不经过网络。

构建需要 Android SDK（compileSdk 36）和 JDK 17+：

```bash
make apk   # 通过项目自带 gradle wrapper 构建，JDK 自动找 Android Studio 的 JBR
```

## 原理

```
   手机                     USB 线                    电脑
┌─────────┐                                    ┌──────────────┐
│ 浏览器   │ ←── adb reverse tcp:8765 ────────  │ 127.0.0.1:8765│
│localhost │                                    │  Go 内置 HTTP │
└─────────┘                                    └──────────────┘
                                                      ↑
                                                同一个 API 也喂
                                                      ↓
                                               ┌──────────────┐
                                               │  原生面板 UI  │
                                               └──────────────┘
```

- 内置 HTTP 服务只监听 `127.0.0.1`，**不在局域网上暴露**，也不会弹防火墙授权
- 手机通过 `adb reverse` 把手机的 localhost 指到电脑，所以手机访问 `localhost` 等于访问电脑
- **同一套网页资源 + 同一个 JSON API** 服务两端：原生面板和手机浏览器看到的是同一份界面
- 剪贴板走网页而不是 adb：**Android 10 起禁止 adb 写设备剪贴板**，而 `localhost` 属于安全上下文，浏览器 Clipboard API 在那里可用

## 技术栈

- **Go** + **Wails v3**（把 Wails 当库直接用，没有 `wails.json`，没有代码生成）
- 前端是 `assets/` 下的**纯 HTML/CSS/JS**，`go:embed` 嵌进二进制。**没有 npm，没有打包步骤**
- 图标由 `icon.go` **运行时绘制**，仓库里没有二进制美术资源
- 前端通过 `fetch("/api/...")` 调后端，不走 Wails bindings

## 构建

```bash
make build     # 本机二进制
make app       # macOS .app（菜单栏应用，无 Dock 图标）
make windows   # Windows exe（交叉编译，从 macOS 就能出）
make linux     # Linux（需要 GTK3 + WebKitGTK 4.1，得在 Linux 上构建）
make apk       # Android APK（需要 Android SDK + JDK 17+）
make icons     # 重新生成图标（含 `make icons-android` 出 APK 启动图标）
```

各平台构建方式不一样，原因是 **macOS 和 Linux 的 GUI 要经 cgo 链接系统 WebView，Windows 走纯 syscall 所以能交叉编译**：

- macOS —— 本机原生构建
- Windows —— `CGO_ENABLED=0`，amd64 / arm64 都能从 Mac 交叉编译
- Linux —— 必须在 Linux 上原生构建

## 依赖 adb

程序不捆绑 adb，会按顺序找：

1. 环境变量 `CABLEDROP_ADB`
2. 程序同目录 / 同目录下的 `platform-tools/`
3. Android Studio 的 SDK 路径
4. `PATH`

没有 adb 时面板会直接告诉你去装。

## 目录

```
main.go      托盘、面板窗口、拖拽
app.go       状态、设备轮询、传输记录、面板显隐
adb.go       adb 封装
files.go     设备文件读写
clip.go      各平台剪贴板
picker.go    系统文件选择框（子进程实现）
server.go    HTTP API + 静态页
icon.go      图标绘制
gen.go       --gen-icons / --gen-android-icons
assets/      前端（index.html 面板 / phone.html 手机端）
android/     Android APK（WebView 壳，纯平台 API，无第三方依赖）
```
