/* CableDrop — the bilingual string table and the language switch, shared by
 * the phone page and the desktop panel.
 *
 * Every user-facing string on either surface lives in the table below, keyed
 * by dotted path ("phone.upload.send") rather than glued together from
 * fragments, so the two languages can diverge in word order. Values take
 * {placeholder} slots, filled from the optional second argument of t().
 *
 * Three ways a string reaches the screen:
 *   - static elements declare data-i18n (textContent), data-i18n-ph
 *     (placeholder), data-i18n-alt (alt) or data-i18n-title / data-i18n-aria
 *     (attributes) and are filled by applyLang();
 *   - strings the page scripts build call t() directly;
 *   - the language toggle is wired here, so a page only registers an
 *     i18nOnChange callback to repaint the state it rendered itself.
 *
 * The choice persists in localStorage and wins over the system locale, which
 * is otherwise read from navigator.languages — the same signal in a desktop
 * WebView and a phone browser. Entries are written one per line in strict
 * 'key': 'value', form because internal/serve/i18n_test.go parses them to
 * prove both languages stay complete.
 */

const I18N = {
  zh: {
    'common.copy': '复制',
    'common.copied': '已复制',
    'common.retry': '重试',
    'common.back': '返回',
    'common.loading': '读取中…',
    'common.empty': '（空）',
    'common.folder': '文件夹',
    'common.unknownReason': '未知原因',
    'common.langTitle': '切换到英文界面',

    'phone.conn.connecting': '连接中…',
    'phone.conn.offline': '未连接到电脑',
    'phone.conn.connectedDevice': '已连接 · {device}',
    'phone.conn.ready': '已连接 · 准备好接收',
    'phone.trust': 'USB 直连 · 数据不经过网络',
    'phone.offline.hint': '请确认数据线已插好、电脑端 CableDrop 正在运行，然后下拉或点刷新。',
    'phone.note.label': '电脑发来的内容',
    'phone.clip.label': '电脑剪贴板',
    'phone.clip.labelImage': '电脑剪贴板 · 图片',
    'phone.clip.imageAlt': '电脑剪贴板里的图片',
    'phone.copy.toPhone': '复制到手机',
    'phone.send.label': '发到电脑',
    'phone.send.placeholder': '粘贴或输入文本，发送后直接进电脑剪贴板',
    'phone.send.button': '发送到电脑',
    'phone.upload.label': '传文件到电脑',
    'phone.upload.pick': '选择要上传的文件',
    'phone.upload.selected': '已选 {n} 个文件',
    'phone.upload.send': '上传',
    'phone.upload.sendCount': '上传 {n} 个文件',
    'phone.upload.progress': '上传中 {pct}%',
    'phone.files.label': '电脑上的文件',
    'phone.files.up': '上一级',
    'phone.files.rootCrumb': '共享目录',
    'phone.files.emptyHere': '这里没有文件',
    'phone.files.reload': '刷新列表',
    'phone.footer': 'CableDrop · 一根线传文件与文字',
    'phone.toast.nothingToCopy': '没有可复制的内容',
    'phone.toast.copied': '已复制到手机',
    'phone.toast.copyFailed': '复制失败，请长按手动选择',
    'phone.toast.uploaded': '已传到电脑',
    'phone.toast.uploadFailed': '上传失败（HTTP {code}）',
    'phone.toast.streamDied': '上传失败：连接中断。请检查数据线后重试；若刚选完文件，可能是这个文件读不出来，换个文件试试',
    'phone.toast.sentToMac': '已发送到电脑剪贴板',
    'phone.toast.sendFailed': '发送失败：{why}',
    'phone.image.share': '分享图片',
    'phone.image.copy': '复制图片到手机',
    'phone.image.save': '保存到手机',
    'phone.image.shareTitle': 'CableDrop 图片',
    'phone.image.nothingToCopy': '没有可复制的图片',
    'phone.image.copyUnsupported': '这台手机的浏览器不支持复制图片，请用「分享图片」或「保存到手机」',
    'phone.image.copied': '图片已复制到手机',
    'phone.image.noPermission': '没有获得剪贴板权限',
    'phone.image.copyFailed': '复制图片失败（{why}），请用「分享图片」或「保存到手机」',
    'phone.image.nothingToShare': '没有可分享的图片',
    'phone.image.preparing': '正在准备分享…',
    'phone.image.shareFailed': '分享失败：{why}',
    'phone.image.shareUnsupported': '这台手机的网页引擎不支持直接分享，请用「保存到手机」后从相册分享',
    'phone.image.nothingToSave': '没有可保存的图片',
    'phone.image.saving': '已交给系统下载',

    'panel.detecting': '正在检测…',
    'panel.sub.unplugged': '插上 USB 线，手机选「传输文件」',
    'panel.refresh': '重新检测手机',
    'panel.close': '关闭面板',
    'panel.closeTitle': '关闭面板（Esc，点击面板外也可关闭）',
    'panel.send.title': '发送文件',
    'panel.send.sub': '拖入文件或文件夹',
    'panel.send.micro': '支持多文件 · 文件夹 · 大文件',
    'panel.drop.tag': '松开即可发送到手机',
    'panel.files.title': '手机文件',
    'panel.files.sub': '浏览并取回文件',
    'panel.files.micro': '查看手机存储 · 下载到 Mac',
    'panel.files.up': '上级目录',
    'panel.files.hint': '点文件夹进入 · 点文件存到 Mac',
    'panel.files.emptyDir': '（空目录）',
    'panel.files.fetch': '取回',
    'panel.files.delete': '删除',
    'panel.files.deleteName': '删除 {name}',
    'panel.text.title': '剪贴板',
    'panel.text.sub': 'Mac ↔ Android 双向复制',
    'panel.text.micro': '支持独立便签',
    'panel.text.crumb': '剪贴板与便签',
    'panel.text.sendTitle': '发到手机',
    'panel.text.placeholder': '在这里粘贴或输入文本，点「发送到手机」\n手机上打开网页后即可一键复制',
    'panel.text.useClip': '用当前剪贴板',
    'panel.text.send': '发送到手机',
    'panel.text.hint': '发送后，在手机网页顶部就能看到并复制。',
    'panel.text.sent': '已发送 · 现在在手机上打开网页即可复制',
    'panel.text.clipTitle': '电脑剪贴板',
    'panel.text.clipHint': '手机网页会读到这段内容，点一下就能复制进手机。',
    'panel.serve.title': '手机网页',
    'panel.serve.sub': '在手机浏览器中打开 CableDrop',
    'panel.serve.copyTitle': '复制手机网页地址',
    'panel.dest.title': '文件保存位置',
    'panel.dest.note': '手机取回到 Mac 的文件会保存到这里',
    'panel.dest.open': '打开',
    'panel.dest.change': '更改',
    'panel.tx.title': '最近传输',
    'panel.tx.clear': '清除',
    'panel.tx.emptyTitle': '还没有传输记录',
    'panel.tx.emptyHint': '拖文件到这里，或点「发送文件」',
    'panel.tx.collapse': '收起',
    'panel.tx.showAll': '查看全部 {n}',
    'panel.tx.pushingElapsed': '发送中 · 已用 {t}',
    'panel.tx.pullingElapsed': '接收中 · 已用 {t}',
    'panel.tx.toPhone': '发送到手机',
    'panel.tx.fromPhone': '从手机取回',
    'panel.tx.transferring': '传输中',
    'panel.tx.done': '已完成',
    'panel.tx.failed': '失败',
    'panel.time.justNow': '刚刚',
    'panel.time.minAgo': '{n} 分钟前',
    'panel.time.hourAgo': '{n} 小时前',
    'panel.time.yesterday': '昨天',
    'panel.time.dayAgo': '{n} 天前',
    'panel.time.date': '{m} 月 {d} 日',
    'panel.dev.noAdb': '未找到 adb',
    'panel.dev.connected': '已连接手机',
    'panel.dev.disconnected': '未连接手机',
    'panel.dev.serial': '序列号 {serial}',
    'panel.dev.installAdb': '请安装 Android Platform Tools',
    'panel.dev.usbConnected': 'USB 已连接',
    'panel.dev.connectedFree': 'USB 已连接 · {free} 可用',
    'panel.error.noAdb': '没有找到 adb\n安装 Android Platform Tools，或运行 brew install android-platform-tools',
    'panel.confirm.delete': '删除「{name}」？此操作无法撤销。',
    'panel.trust': 'USB 直连 · 数据不经过网络或云端',
    'panel.trustRight': '安全 · 私密 · 无需安装 App',
  },
  en: {
    'common.copy': 'Copy',
    'common.copied': 'Copied',
    'common.retry': 'Retry',
    'common.back': 'Back',
    'common.loading': 'Loading…',
    'common.empty': '(empty)',
    'common.folder': 'Folder',
    'common.unknownReason': 'Unknown reason',
    'common.langTitle': 'Switch to 中文',

    'phone.conn.connecting': 'Connecting…',
    'phone.conn.offline': 'Not connected',
    'phone.conn.connectedDevice': 'Connected · {device}',
    'phone.conn.ready': 'Connected · ready to receive',
    'phone.trust': 'Direct USB · data never touches the network',
    'phone.offline.hint': 'Check that the cable is plugged in and CableDrop is running on the computer, then pull down or tap retry.',
    'phone.note.label': 'Sent from the computer',
    'phone.clip.label': 'Computer clipboard',
    'phone.clip.labelImage': 'Computer clipboard · image',
    'phone.clip.imageAlt': 'Image from the computer clipboard',
    'phone.copy.toPhone': 'Copy to phone',
    'phone.send.label': 'To the computer',
    'phone.send.placeholder': 'Paste or type text — sending puts it straight into the computer’s clipboard',
    'phone.send.button': 'Send to computer',
    'phone.upload.label': 'Files to the computer',
    'phone.upload.pick': 'Choose files to upload',
    'phone.upload.selected': '{n} files chosen',
    'phone.upload.send': 'Upload',
    'phone.upload.sendCount': 'Upload {n} files',
    'phone.upload.progress': 'Uploading {pct}%',
    'phone.files.label': 'Files on the computer',
    'phone.files.up': 'Up one level',
    'phone.files.rootCrumb': 'Shared folder',
    'phone.files.emptyHere': 'Nothing here',
    'phone.files.reload': 'Refresh list',
    'phone.footer': 'CableDrop · one cable for files and text',
    'phone.toast.nothingToCopy': 'Nothing to copy',
    'phone.toast.copied': 'Copied to phone',
    'phone.toast.copyFailed': 'Copy failed — press and hold to select manually',
    'phone.toast.uploaded': 'Uploaded to the computer',
    'phone.toast.uploadFailed': 'Upload failed (HTTP {code})',
    'phone.toast.streamDied': 'Upload failed: the connection dropped. Check the cable and try again; if it failed right after picking a file, that file may be unreadable — try another one.',
    'phone.toast.sentToMac': 'Sent to the computer clipboard',
    'phone.toast.sendFailed': 'Send failed: {why}',
    'phone.image.share': 'Share image',
    'phone.image.copy': 'Copy image to phone',
    'phone.image.save': 'Save to phone',
    'phone.image.shareTitle': 'CableDrop image',
    'phone.image.nothingToCopy': 'No image to copy',
    'phone.image.copyUnsupported': 'This browser cannot copy images — use “Share image” or “Save to phone”',
    'phone.image.copied': 'Image copied to phone',
    'phone.image.noPermission': 'Clipboard permission not granted',
    'phone.image.copyFailed': 'Copying the image failed ({why}) — use “Share image” or “Save to phone”',
    'phone.image.nothingToShare': 'No image to share',
    'phone.image.preparing': 'Preparing to share…',
    'phone.image.shareFailed': 'Share failed: {why}',
    'phone.image.shareUnsupported': 'This browser cannot share directly — use “Save to phone”, then share from the gallery',
    'phone.image.nothingToSave': 'No image to save',
    'phone.image.saving': 'Handed to the system download',

    'panel.detecting': 'Detecting…',
    'panel.sub.unplugged': 'Plug in the USB cable and choose “File transfer” on the phone',
    'panel.refresh': 'Re-detect the phone',
    'panel.close': 'Close panel',
    'panel.closeTitle': 'Close panel (Esc, or click outside it)',
    'panel.send.title': 'Send files',
    'panel.send.sub': 'Drop files or folders in',
    'panel.send.micro': 'Multiple files · folders · big files',
    'panel.drop.tag': 'Release to send to the phone',
    'panel.files.title': 'Phone files',
    'panel.files.sub': 'Browse and fetch files',
    'panel.files.micro': 'Browse phone storage · download to the Mac',
    'panel.files.up': 'Parent folder',
    'panel.files.hint': 'Tap a folder to enter · tap a file to save it to the Mac',
    'panel.files.emptyDir': '(empty folder)',
    'panel.files.fetch': 'Fetch',
    'panel.files.delete': 'Delete',
    'panel.files.deleteName': 'Delete {name}',
    'panel.text.title': 'Clipboard',
    'panel.text.sub': 'Copy both ways, Mac ↔ Android',
    'panel.text.micro': 'Works as a standalone note',
    'panel.text.crumb': 'Clipboard and note',
    'panel.text.sendTitle': 'To the phone',
    'panel.text.placeholder': 'Paste or type text here, then tap “Send to phone”\nOnce the phone page is open, copying it is one tap away',
    'panel.text.useClip': 'Use current clipboard',
    'panel.text.send': 'Send to phone',
    'panel.text.hint': 'After sending, it appears at the top of the phone page, ready to copy.',
    'panel.text.sent': 'Sent · open the page on the phone to copy it',
    'panel.text.clipTitle': 'Computer clipboard',
    'panel.text.clipHint': 'The phone page reads this — one tap copies it into the phone.',
    'panel.serve.title': 'Phone page',
    'panel.serve.sub': 'Open CableDrop in the phone’s browser',
    'panel.serve.copyTitle': 'Copy the phone page address',
    'panel.dest.title': 'Save location',
    'panel.dest.note': 'Files fetched from the phone are saved here',
    'panel.dest.open': 'Open',
    'panel.dest.change': 'Change',
    'panel.tx.title': 'Recent transfers',
    'panel.tx.clear': 'Clear',
    'panel.tx.emptyTitle': 'No transfers yet',
    'panel.tx.emptyHint': 'Drop files here, or tap “Send files”',
    'panel.tx.collapse': 'Show less',
    'panel.tx.showAll': 'Show all {n}',
    'panel.tx.pushingElapsed': 'Sending · {t} elapsed',
    'panel.tx.pullingElapsed': 'Receiving · {t} elapsed',
    'panel.tx.toPhone': 'Sent to the phone',
    'panel.tx.fromPhone': 'Fetched from the phone',
    'panel.tx.transferring': 'Transferring',
    'panel.tx.done': 'Done',
    'panel.tx.failed': 'Failed',
    'panel.time.justNow': 'just now',
    'panel.time.minAgo': '{n} min ago',
    'panel.time.hourAgo': '{n} hr ago',
    'panel.time.yesterday': 'yesterday',
    'panel.time.dayAgo': '{n} d ago',
    'panel.time.date': '{m}/{d}',
    'panel.dev.noAdb': 'adb not found',
    'panel.dev.connected': 'Phone connected',
    'panel.dev.disconnected': 'No phone',
    'panel.dev.serial': 'Serial {serial}',
    'panel.dev.installAdb': 'Install Android Platform Tools',
    'panel.dev.usbConnected': 'USB connected',
    'panel.dev.connectedFree': 'USB connected · {free} free',
    'panel.error.noAdb': 'adb was not found\nInstall Android Platform Tools, or run brew install android-platform-tools',
    'panel.confirm.delete': 'Delete “{name}”? This cannot be undone.',
    'panel.trust': 'Direct USB · data never touches the network or cloud',
    'panel.trustRight': 'Private · secure · no app to install',
  },
};

const I18N_STORE = 'cabledrop.lang';
let i18nLang = '';
const i18nListeners = [];

/* Register a callback that repaints what the page rendered without
 * data-i18n: connection lines, list rows, relative times. Fired on every
 * switch so a language change is live, not deferred to the next poll. */
function i18nOnChange(fn) { i18nListeners.push(fn); }

/* Look the key up in the current language, fall back to the other one and
 * finally return the key itself — a missing entry must be visible, not
 * silent. i18n_test.go is the guard that keeps this branch unreachable. */
function t(key, params) {
  let s = I18N[i18nLang] ? I18N[i18nLang][key] : null;
  if (s == null) {
    for (const lang of Object.keys(I18N)) {
      if (lang !== i18nLang && I18N[lang][key] != null) { s = I18N[lang][key]; break; }
    }
  }
  if (s == null) return key;
  if (params) {
    for (const k in params) {
      s = s.split('{' + k + '}').join(String(params[k]));
    }
  }
  return s;
}

function i18nDetect() {
  try {
    const saved = localStorage.getItem(I18N_STORE);
    if (saved === 'zh' || saved === 'en') return saved;
  } catch (_) { /* storage refused (private mode): the locale still works */ }
  const candidates = (navigator.languages && navigator.languages.length)
    ? navigator.languages : [navigator.language || ''];
  for (const c of candidates) {
    if (/^zh([_-]|$)/i.test(c)) return 'zh';
  }
  return 'en';
}

function applyLang() {
  document.documentElement.lang = i18nLang === 'zh' ? 'zh-CN' : 'en';
  document.querySelectorAll('[data-i18n]').forEach((el) => { el.textContent = t(el.dataset.i18n); });
  document.querySelectorAll('[data-i18n-ph]').forEach((el) => { el.placeholder = t(el.dataset.i18nPh); });
  document.querySelectorAll('[data-i18n-alt]').forEach((el) => { el.alt = t(el.dataset.i18nAlt); });
  document.querySelectorAll('[data-i18n-title]').forEach((el) => { el.title = t(el.dataset.i18nTitle); });
  document.querySelectorAll('[data-i18n-aria]').forEach((el) => { el.setAttribute('aria-label', t(el.dataset.i18nAria)); });
  // The switcher carries the name of the language it would switch to — the
  // one word in that language that needs no translation.
  document.querySelectorAll('[data-i18n-toggle]').forEach((el) => {
    el.textContent = i18nLang === 'zh' ? 'English' : '中文';
  });
}

function setLang(lang) {
  if ((lang !== 'zh' && lang !== 'en') || lang === i18nLang) return;
  i18nLang = lang;
  try { localStorage.setItem(I18N_STORE, lang); } catch (_) { /* keep going unpersisted */ }
  applyLang();
  i18nListeners.forEach((fn) => { fn(); });
}

// Scripts sit at the end of <body>, so the DOM is parseable and the first
// applyLang() lands before first paint — no Chinese flash for an English
// system, and vice versa.
i18nLang = i18nDetect();
applyLang();
document.querySelectorAll('[data-i18n-toggle]').forEach((el) => {
  el.addEventListener('click', () => setLang(i18nLang === 'zh' ? 'en' : 'zh'));
});
