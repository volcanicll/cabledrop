# site/ — CableDrop 产品落地页

仓库里这个目录是 CableDrop 的**静态产品介绍页**。它和产品本体是同一套设计语言：
配色、圆角、字号层级都对着 `internal/serve/assets/css/tokens.css` 抄的，
所以改产品配色时这里也应该跟着改。

## 内容

```
site/
├── index.html    单页落地页（Hero / 三卖点 / 截图 / 架构流程 / 功能网格 / Android 应用 / 隐私 / 上手步骤）
├── style.css     全部样式，含 prefers-color-scheme 深浅色
└── README.md     本文件
```

**没有构建步骤，没有任何外部依赖** —— 没有框架、没有 CDN、没有外部字体、
没有图标库。图标是内联 SVG。图片直接引用仓库里的 `../docs/*.png`。
唯一的 JS 是 `i18n.js`（中英字典 + 切换），不联网、无依赖。

`index.html` 引用的图（都在 `docs/`，除 icon 外都是生成物）。
**中英各一套**：切语言时 `src` 也跟着换，否则会出现「英文文案配中文截图」。
英文用 `docs/` 下的原名（主 README 和旧链接都指向它们），中文在 `docs/zh/`：

| 文件 | 用途 |
|---|---|
| `icon-256.png` | favicon、页头 logo、Android 应用段的启动图标（不分语言） |
| `panel-light.png` / `panel-dark.png` | 桌面面板，浅色 / 深色 |
| `phone-light.png` | 手机网页整页 |
| `apk-light.png` | 安装版 APK 在真机上运行（已裁掉状态栏） |

路径写在 `i18n.js` 的字典里（`shot.*.src` / `shot.*.alt`），由 `data-i18n-attr`
驱动——和文案走同一套机制，所以加语言只改字典。`internal/serve/site_test.go`
守着它：任一语言的 `shot.*.src` 缺了、指向不存在的文件、或者两语言指向同一张图，
CI 都会红。

`apk-light.png` 是**真机截图**，不是渲染图：`adb shell screencap` 拿 1080×2340，
再用 `magick` 切掉状态栏、缩到 780 宽。它跟 `phone-light.png` 内容几乎
一样 —— 这是事实，APK 就是同一个页面套了个 WebView 壳，页面里不要把它写成两种界面。
拍的时候让 WebView 临时指向 demo fixture（`?lang=` / `cabledrop.demo.lang` cookie），
**不要对着真实服务拍**，否则会把你自己的文件名拍进去。

## 本地预览

因为它引用 `../docs/` 下的图片，**不能把 index.html 单独拷走**，要在仓库里预览：

```bash
open site/index.html            # 直接双击打开就行
# 或者起个静态服务（路径更像线上环境）：
python3 -m http.server -d . 8080   # 然后访问 http://localhost:8080/site/
```

## 部署（GitHub Pages）

用 Pages 的 `/docs` 或分支方式都行，但**这个目录不是仓库根，别直接把它当 Pages 根** ——
页面对 `../docs/*.png` 的引用会失效。

两种可行做法：

1. **Actions 打包**：工作流里把 `site/` 和 `docs/` 一起放到一个发布目录
   （`site/*` 放根、`docs/*` 放 `docs/`），再 `upload-pages-artifact`。这样路径不变，最省事。
2. **改引用**：把 `index.html` 里的 `../docs/` 全换成实际的图片 URL（比如 raw 或 CDN），
   然后单独发布 `site/`。

## 改的时候注意

- 图片路径是 `../docs/panel-light.png` 这类，直接从 `docs/` 出图。
  `docs/*.png` 是生成物，重新生成用仓库根的 `python3 scripts/shots.py`（见主 README）。
- 深浅色靠 `prefers-color-scheme`，不要加手动主题切换开关 —— 产品本身也是跟着系统的。
- 文案不要写"绝对安全""100% 安全"这类无法证明的话。产品能证明的事实只有三条：
  不经网络、不经云端、手机端无需安装。
