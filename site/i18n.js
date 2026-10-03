/* CableDrop landing page — bilingual (zh / en).
 *
 * Dependency-free, like the rest of the page. Every string the page shows lives
 * here under a flat key; index.html marks the elements with data-i18n (plain
 * text) or data-i18n-html (values that carry inline markup: <em>, <code>, <b>,
 * <a>). Values in this file are the page's own copy, so injecting them as HTML
 * is not a risk — but it is why the two attributes are kept separate.
 *
 * The default follows the system: a Chinese browser gets Chinese, anything else
 * gets English. An explicit choice is remembered and wins from then on.
 */

(function () {
  'use strict';

  var DICT = {
    zh: {
      'html.lang': 'zh-CN',
      'meta.title': 'CableDrop — 一根 USB 线，在电脑和安卓手机之间传文件与文字',
      'meta.desc': 'CableDrop 通过一根 USB 线在 Mac 或 PC 与安卓手机之间传文件和剪贴板文字。不联网、不过云、手机端无需安装。',

      'nav.screenshots': '界面',
      'nav.how': '原理',
      'nav.features': '功能',
      'nav.apk': '安卓 App',
      'nav.download': '下载',
      'nav.langLabel': '语言',

      'hero.eyebrow': 'USB 直连 · 数据不经过网络',
      'hero.title': '用<em>一根 USB 线</em>，在电脑和安卓手机之间传文件与文字。',
      'hero.lede': '不联网，不过云，手机端无需安装。插上线，在手机上打开一个网页，两个方向就都通了。',
      'hero.ctaMac': '下载 macOS 版',
      'hero.ctaAll': '所有平台',
      'hero.ctaSource': '查看源码',
      'hero.note': 'MIT 许可 · macOS 12+、Windows 10+、Linux（GTK3）· 安卓 7+',

      'pillar.files.title': '文件双向传',
      'pillar.files.body': '拖到面板上，或选择文件发送。浏览手机存储，把任何东西拉回来。',
      'pillar.clip.title': '剪贴板双向通',
      'pillar.clip.body': '电脑上复制，手机上点一下就粘贴；手机上粘贴，自动落到电脑。另有一条独立便签，绝不覆盖你正在用的内容。',
      'pillar.progress.title': '真实的进度',
      'pillar.progress.body': '多文件按字节加权算百分比，adb 不给进度时显示已用时间，手机上传给真实百分比。',

      'shots.title': '两个界面，一套设计',
      'shots.lede': '桌面面板和手机页面共用同一套颜色、圆角与字号——把一份样式表的决定，同时用在一个 380×540 的窗口和一块 390×844 的屏幕上。',
      'shots.panel.caption': '<strong>桌面面板</strong> —— 菜单栏窗口，380×540。深色模式跟随系统。',
      'shots.phone.caption': '<strong>手机页面</strong> —— 390×844 视口下的整页。任何手机浏览器，无需安装。',

      // 截图路径也走字典：页面是双语的，图却只有一种语言的话，
      // 切到另一种语言就会看到「英文界面配中文截图」这种明显穿帮。
      // 英文用 docs/ 下的原名（README 和旧链接都指向它们），中文在 docs/zh/。
      'shot.hero.src': '../docs/zh/panel-light.png',
      'shot.hero.alt': 'CableDrop 桌面面板：顶部是已连接的安卓手机，四张操作卡片、保存位置卡片、最近传输列表，以及 USB 直连提示条。',
      'shot.panel.src': '../docs/zh/panel-dark.png',
      'shot.panel.alt': 'CableDrop 桌面面板，深色模式。',
      'shot.phone.src': '../docs/zh/phone-light.png',
      'shot.phone.alt': 'CableDrop 手机页面整页：顶部为已连接设备，一条 USB 直连提示，以及电脑发来的内容、电脑剪贴板、发到电脑、上传文件、浏览电脑共享目录等卡片。',
      'shot.apk.src': '../docs/zh/apk-light.png',
      'shot.apk.alt': 'CableDrop 作为已安装的安卓应用运行：已连接头部、绿色 USB 直连提示条、剪贴板卡片、发送文本卡片、上传卡片与文件浏览。',

      'how.title': '它是怎么工作的',
      'how.lede': '一个 Go 二进制。整条链路就是那根线。',
      'how.phone': '手机',
      'how.phoneSub': '浏览器或 APK',
      'how.linkAdb': 'adb reverse tcp:8765',
      'how.linkUsb': 'USB 数据线',
      'how.computer': '电脑',
      'how.computerSub': '127.0.0.1:8765 · Go HTTP',
      'how.aria': '手机浏览器通过 USB 线访问电脑：adb reverse 把手机的 localhost:8765 映射到电脑的 127.0.0.1:8765，由同一个 Go HTTP 服务同时提供手机页面和桌面面板。',
      'how.fact1': '电脑只监听 <code>127.0.0.1</code>。不弹防火墙，局域网里什么都看不到。',
      'how.fact2': '前端是纯 HTML/CSS/JS，用 <code>go:embed</code> 打进二进制——没有 npm，没有构建步骤。',
      'how.fact3': '剪贴板走页面，不走 <code>adb shell</code>：安卓 10+ 禁止通过 adb 写设备剪贴板，而 <code>localhost</code> 属于安全上下文，浏览器 Clipboard API 在那里可用。',
      'how.fact4': '设备在线靠一条常驻的 <code>adb track-devices</code> 连接——空闲时不反复 fork 进程。',
      'how.fact5': '图标由程序内一个极小的光栅化器画出来。仓库里没有任何二进制美术资源。',

      'feat.title': '都有什么',
      'feat.1.title': '收发文件',
      'feat.1.body': '把文件或整个文件夹拖到面板上，推送会落到手机的 Download 目录。拉取则落到电脑的共享目录。',
      'feat.2.title': '剪贴板桥',
      'feat.2.body': '双向，外加一条独立便签，发文字永远不会覆盖你正在用的剪贴板。',
      'feat.3.title': '手机端文件浏览',
      'feat.3.body': '电脑上的共享目录在手机端就是一个真正的文件浏览器——子文件夹、大小、下载都有。',
      'feat.4.title': '诚实的进度',
      'feat.4.body': '按字节加权的百分比，adb 给不出进度时显示已用时间，失败用人话说明而不是错误码。',
      'feat.5.title': '真实的连接状态',
      'feat.5.body': '未连接、连接中、USB 模式不对、缺少 adb、隧道建立失败——每一种都告诉你下一步该做什么。',
      'feat.6.title': '<a href="#apk">可选的安卓 App</a>',
      'feat.6.body': '一个约 845 KB 的 WebView 外壳，想让它「像个装好的应用」时用它：系统文件选择器、下载进入下载 App、断线重连页。',
      'feat.7.title': '浅色与深色',
      'feat.7.body': '两个界面都跟随 <code>prefers-color-scheme</code>。这是一个媒体查询，不是一个设置项。',
      'feat.8.title': '跨平台构建',
      'feat.8.body': '一份代码出 macOS、Windows、Linux。Windows 可以从 Mac 交叉编译；Linux 构建需要 GTK3 和 WebKitGTK 4.1。',

      'apk.title': '安卓 App 是可选的',
      'apk.lede': '上面所有功能在手机自带浏览器里都能用——无需安装。APK 只是把同一个页面套进一个小 WebView 外壳，适合你更愿意点图标而不是点书签的时候。它自带的服务端为零：插线方式和之前完全一样。',
      'apk.icon': '<strong>启动图标</strong> —— 和其他地方同一份美术，由代码画出，而不是塞一堆位图。',
      'apk.gain1': '<b>规矩的文件选择。</b>上传走系统选择器，下载进入下载 App，非 ASCII 文件名完整保留。',
      'apk.gain2': '<b>断线重连页。</b>线拔了会明说，而不是甩一个 WebView 报错。',
      'apk.gain3': '<b>跟随主题的窗口。</b>窗口背景和状态栏跟随系统浅色/深色，深色手机冷启动不会闪白。',
      'apk.specSize.dt': '体积',
      'apk.specSize.dd': '约 845 KB —— 只有平台 WebView，别无其他',
      'apk.specRange.dt': '支持范围',
      'apk.specRange.dd': '安卓 7.0+（minSdk 24），targetSdk 36',
      'apk.specPerm.dt': '权限',
      'apk.specPerm.dd': '只有 <code>INTERNET</code>，且仅用于本机回环隧道',
      'apk.specBuild.dt': '构建',
      'apk.specBuild.dd': '<code>make apk</code> &rarr; <code>dist/CableDrop.apk</code>',

      'trust.title': '你的数据不会离开这根线',
      'trust.body': '每一个字节都走 USB 连接。没有账号，没有服务器，除了你的包管理器本来就知道的以外没有任何遥测。手机上不安装任何东西——你只是打开一个页面。',

      'dl.title': '下载安装',
      'dl.lede': '选你的平台。桌面端已经打包好，安卓端是可选的。',
      'dl.mac': 'macOS',
      'dl.macNote': '通用版（Apple 芯片 + Intel），.dmg',
      'dl.win': 'Windows',
      'dl.winNote': '64 位（amd64），便携版 .zip',
      'dl.winArm': 'Windows ARM',
      'dl.winArmNote': 'ARM64，便携版 .zip',
      'dl.linux': 'Linux',
      'dl.linuxNote': 'amd64，需要 GTK3 与 WebKitGTK 4.1',
      'dl.apk': '安卓 App（可选）',
      'dl.apkNote': '调试签名，仅用于测试',
      'dl.download': '下载',
      'dl.allReleases': '所有版本与校验和',
      'dl.sums': 'SHA256 校验和',

      'steps.title': '自己跑起来',
      'step1': '<b>准备 adb。</b>安装 Android Platform Tools（<code>brew install android-platform-tools</code>），或把 <code>platform-tools</code> 目录放在二进制旁边。',
      'step2b': '构建。',
      'step3': '<b>插上线</b>，把手机的 USB 模式设为<em>文件传输 / MTP</em>。「仅充电」对 adb 是不可见的。',
      'step4': '<b>打开面板</b>（菜单栏），然后在手机上打开 <code>http://localhost:8765</code>。手机端的全部界面就是这个。',

      'foot.tagline': 'CableDrop · 一根线传文件与文字',
      'foot.license': 'MIT',
      'foot.readme': 'README'
    },

    en: {
      'html.lang': 'en',
      'meta.title': 'CableDrop — move files and text between your computer and an Android phone over one USB cable',
      'meta.desc': 'CableDrop moves files and clipboard text between a Mac or PC and an Android phone over a single USB cable. No network, no cloud, nothing to install on the phone.',

      'nav.screenshots': 'Screenshots',
      'nav.how': 'How it works',
      'nav.features': 'Features',
      'nav.apk': 'Android app',
      'nav.download': 'Download',
      'nav.langLabel': 'Language',

      'hero.eyebrow': 'USB direct · nothing in the middle',
      'hero.title': 'Move files and text between your computer and an Android&nbsp;phone over <em>one USB cable</em>.',
      'hero.lede': 'No network. No cloud. Nothing to install on the phone. Plug the cable in, open a page on the phone, and both directions work.',
      'hero.ctaMac': 'Download for macOS',
      'hero.ctaAll': 'All platforms',
      'hero.ctaSource': 'View source',
      'hero.note': 'MIT licensed · macOS 12+, Windows 10+, Linux (GTK3) · Android 7+',

      'pillar.files.title': 'Files, both directions',
      'pillar.files.body': "Drag onto the panel or pick files to send. Browse the phone's storage and pull anything back.",
      'pillar.clip.title': 'Clipboard, both directions',
      'pillar.clip.body': "Copy on the computer, tap to copy on the phone. Paste on the phone, it lands on the computer. A separate note slot never clobbers what you're using.",
      'pillar.progress.title': 'Real progress',
      'pillar.progress.body': 'Byte-weighted percentage for multi-file batches, elapsed time where adb gives no stream progress, a real percentage for phone uploads.',

      'shots.title': 'Two surfaces, one design language',
      'shots.lede': "The desktop panel and the phone page share the same colour tokens, radii and type scale — one stylesheet's worth of design decisions, applied to a 380×540 window and a 390×844 screen.",
      'shots.panel.caption': '<strong>Desktop panel</strong> — a menu bar window, 380×540. Dark mode follows the system.',
      'shots.phone.caption': '<strong>Phone page</strong> — the whole page at a 390×844 viewport. Any mobile browser, no install.',

      // The screenshot paths live in the dictionary too: the page is bilingual,
      // and a screenshot that stays in one language is the giveaway — English
      // copy sitting over a Chinese UI. English keeps the docs/*.png names
      // (README.md and older links point at them); Chinese lives in docs/zh/.
      'shot.hero.src': '../docs/panel-light.png',
      'shot.hero.alt': 'The CableDrop menu bar panel: a connected Android phone at the top, four action cards, a save-location card, the recent transfers list, and a USB trust banner.',
      'shot.panel.src': '../docs/panel-dark.png',
      'shot.panel.alt': 'The CableDrop desktop panel in dark mode.',
      'shot.phone.src': '../docs/phone-light.png',
      'shot.phone.alt': "The CableDrop phone page, full page: header with the connected device, a USB trust strip, and cards for content from the computer, the computer clipboard, sending text back, uploading files, and browsing the computer's shared folder.",
      'shot.apk.src': '../docs/apk-light.png',
      'shot.apk.alt': 'CableDrop running as an installed Android app: the connected header, the green USB-only strip, the clipboard card, the send-text card, the upload card and the file browser.',

      'how.title': 'How it works',
      'how.lede': 'One Go binary. The cable is the whole transport.',
      'how.phone': 'Phone',
      'how.phoneSub': 'browser or APK',
      'how.linkAdb': 'adb reverse tcp:8765',
      'how.linkUsb': 'USB cable',
      'how.computer': 'Computer',
      'how.computerSub': '127.0.0.1:8765 · Go HTTP',
      'how.aria': "The phone's browser reaches the computer over the USB cable: adb reverse maps the phone's localhost:8765 to the computer's 127.0.0.1:8765, where the Go HTTP server serves both the phone page and the desktop panel from the same JSON API.",
      'how.fact1': 'The computer listens on <code>127.0.0.1</code> only. No firewall prompt, nothing on the LAN.',
      'how.fact2': 'The frontend is plain HTML/CSS/JS embedded with <code>go:embed</code> — no npm, no build step.',
      'how.fact3': 'The clipboard goes through the page, not <code>adb shell</code>: Android 10+ forbids writing the device clipboard over adb, and <code>localhost</code> is a secure context, so the browser Clipboard API works there.',
      'how.fact4': 'Device presence rides a persistent <code>adb track-devices</code> connection — no process forking while idle.',
      'how.fact5': 'Icons are drawn in code by a small in-process rasteriser. There is no binary artwork in the repository.',

      'feat.title': "What's in the box",
      'feat.1.title': 'Send and receive files',
      'feat.1.body': "Drop files or whole folders on the panel; pushes land in the phone's Download folder. Pulls land in the shared folder on the computer.",
      'feat.2.title': 'Clipboard bridge',
      'feat.2.body': 'Both directions, including a dedicated note slot so sending text never overwrites the clipboard you\'re working from.',
      'feat.3.title': 'Phone file browser',
      'feat.3.body': 'The shared folder on the computer is a real file browser on the phone — subfolders, sizes, downloads included.',
      'feat.4.title': 'Honest progress',
      'feat.4.body': 'Byte-weighted percentages, elapsed time when adb offers nothing, and failures reported in words rather than error codes.',
      'feat.5.title': 'Real connection states',
      'feat.5.body': 'Not connected, connecting, cable in the wrong USB mode, adb missing, tunnel failed — each says what to do next.',
      'feat.6.title': '<a href="#apk">Optional Android app</a>',
      'feat.6.body': 'A ~845 KB WebView shell for when you want it to feel installed: real file picking, downloads into the Downloads app, a reconnect screen.',
      'feat.7.title': 'Light and dark',
      'feat.7.body': 'Both surfaces follow <code>prefers-color-scheme</code>. It is a media query, not a setting.',
      'feat.8.title': 'Portable build',
      'feat.8.body': 'macOS, Windows and Linux from one tree. Windows cross-compiles from a Mac; the Linux build needs GTK3 and WebKitGTK 4.1.',

      'apk.title': 'The Android app is optional',
      'apk.lede': "Everything above works in the phone's own browser — nothing to install. The APK is a small WebView shell around the very same page, for when you would rather tap an icon than a bookmark. It bundles no server of its own: plug the cable in exactly as before.",
      'apk.icon': '<strong>Launcher icon</strong> — the same artwork as everywhere else, drawn in code rather than shipped as bitmaps.',
      'apk.gain1': '<b>File picking that behaves.</b> Uploads use the system picker, and downloads land in the Downloads app with non-ASCII names intact.',
      'apk.gain2': '<b>A reconnect screen.</b> Cable out says so plainly instead of showing a WebView error.',
      'apk.gain3': '<b>A themed window.</b> The window background and status bar follow the system light/dark setting, so a dark phone never flashes white on cold start.',
      'apk.specSize.dt': 'Size',
      'apk.specSize.dd': '~845 KB — the platform WebView and nothing else',
      'apk.specRange.dt': 'Range',
      'apk.specRange.dd': 'Android 7.0+ (minSdk 24), targetSdk 36',
      'apk.specPerm.dt': 'Permissions',
      'apk.specPerm.dd': '<code>INTERNET</code> alone, used only by the loopback tunnel',
      'apk.specBuild.dt': 'Build',
      'apk.specBuild.dd': '<code>make apk</code> &rarr; <code>dist/CableDrop.apk</code>',

      'trust.title': "Your data doesn't leave the cable",
      'trust.body': 'Every byte goes over the USB connection. No account, no server, no telemetry beyond what your package manager already knows. Nothing is installed on the phone — you open a page.',

      'dl.title': 'Download',
      'dl.lede': 'Pick your platform. The desktop builds are ready to run; the Android app is optional.',
      'dl.mac': 'macOS',
      'dl.macNote': 'Universal (Apple silicon + Intel), .dmg',
      'dl.win': 'Windows',
      'dl.winNote': '64-bit (amd64), portable .zip',
      'dl.winArm': 'Windows ARM',
      'dl.winArmNote': 'ARM64, portable .zip',
      'dl.linux': 'Linux',
      'dl.linuxNote': 'amd64, needs GTK3 and WebKitGTK 4.1',
      'dl.apk': 'Android app (optional)',
      'dl.apkNote': 'Debug-signed, for testing only',
      'dl.download': 'Download',
      'dl.allReleases': 'All releases & checksums',
      'dl.sums': 'SHA256 checksums',

      'steps.title': 'Run it yourself',
      'step1': '<b>Get adb.</b> Install Android Platform Tools (<code>brew install android-platform-tools</code>), or drop the <code>platform-tools</code> folder next to the binary.',
      'step2b': 'Build.',
      'step3': '<b>Plug in the cable</b> and set the phone\'s USB mode to <em>File transfer / MTP</em>. "Charging only" is invisible to adb.',
      'step4': '<b>Open the panel</b> from the menu bar, then open <code>http://localhost:8765</code> on the phone. That\'s the whole phone UI.',

      'foot.tagline': 'CableDrop · files and text over one cable',
      'foot.license': 'MIT',
      'foot.readme': 'README'
    }
  };

  var STORE_KEY = 'cabledrop.lang';
  var LANGS = ['zh', 'en'];

  function detect() {
    var saved = null;
    try { saved = localStorage.getItem(STORE_KEY); } catch (e) { /* private mode */ }
    if (saved && LANGS.indexOf(saved) !== -1) return saved;

    var tags = (navigator.languages && navigator.languages.length)
      ? navigator.languages
      : [navigator.language || ''];
    for (var i = 0; i < tags.length; i++) {
      var t = String(tags[i]).toLowerCase();
      if (t.indexOf('zh') === 0) return 'zh';
      if (t.indexOf('en') === 0) return 'en';
    }
    return 'en';
  }

  var current = detect();

  function t(lang, key) {
    var table = DICT[lang] || DICT.en;
    if (Object.prototype.hasOwnProperty.call(table, key)) return table[key];
    // Falling back to English is better than showing a bare key, but the miss
    // is still a bug — leave a trace a test can find.
    if (typeof console !== 'undefined') console.warn('i18n: missing key', lang, key);
    return (DICT.en[key] !== undefined) ? DICT.en[key] : key;
  }

  function apply(lang) {
    current = lang;
    document.documentElement.lang = t(lang, 'html.lang');

    var title = t(lang, 'meta.title');
    if (document.title !== title) document.title = title;
    var desc = document.querySelector('meta[name="description"]');
    if (desc) desc.setAttribute('content', t(lang, 'meta.desc'));

    document.querySelectorAll('[data-i18n]').forEach(function (el) {
      el.textContent = t(lang, el.getAttribute('data-i18n'));
    });
    document.querySelectorAll('[data-i18n-html]').forEach(function (el) {
      el.innerHTML = t(lang, el.getAttribute('data-i18n-html'));
    });
    document.querySelectorAll('[data-i18n-attr]').forEach(function (el) {
      // Format: "attr:key,attr:key" — for placeholders and aria-labels.
      el.getAttribute('data-i18n-attr').split(',').forEach(function (pair) {
        var i = pair.indexOf(':');
        if (i < 0) return;
        el.setAttribute(pair.slice(0, i).trim(), t(lang, pair.slice(i + 1).trim()));
      });
    });

    // The switch shows the language you would get, and marks the current one.
    document.querySelectorAll('[data-lang-btn]').forEach(function (btn) {
      var code = btn.getAttribute('data-lang-btn');
      btn.setAttribute('aria-pressed', String(code === lang));
    });

    try { localStorage.setItem(STORE_KEY, lang); } catch (e) { /* private mode */ }
  }

  function set(lang) {
    if (LANGS.indexOf(lang) === -1 || lang === current) return;
    // A short cross-fade so the swap reads as a change, not a glitch. The
    // class is removed on the next frame; CSS owns the timing.
    var root = document.documentElement;
    root.classList.add('lang-switching');
    apply(lang);
    window.requestAnimationFrame(function () {
      window.setTimeout(function () { root.classList.remove('lang-switching'); }, 40);
    });
  }

  function init() {
    document.querySelectorAll('[data-lang-btn]').forEach(function (btn) {
      btn.addEventListener('click', function () { set(btn.getAttribute('data-lang-btn')); });
    });
    apply(current);
    // Expose a tiny hook for the test that checks every key resolves.
    window.cabledropI18n = { dict: DICT, langs: LANGS, t: t, apply: apply, set: set, get: function () { return current; } };
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();
