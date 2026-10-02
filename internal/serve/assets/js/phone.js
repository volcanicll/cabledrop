/* CableDrop — phone page.
 *
 * Reached at http://localhost:<port> on the device, tunnelled over the USB
 * cable by `adb reverse`. `localhost` counts as a secure context, which is
 * what makes navigator.clipboard available here — the reason the clipboard is
 * handed over through this page instead of written by adb, which Android 10
 * and later forbid. Uses the shared helpers in api.js.
 */

/* ---------------- toast ---------------- */

// One feedback surface for the whole page. Buttons keep their labels; state
// changes are announced here instead of rewriting button text.
let toastTimer = null;

function toast(msg) {
  const el = $('toast');
  el.textContent = msg;
  el.hidden = false;
  requestAnimationFrame(() => el.classList.add('show'));
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => {
    el.classList.remove('show');
    setTimeout(() => { el.hidden = true; }, 220);
  }, 1800);
}

/* Copying needs a user gesture on iOS and some Android browsers, so every
 * copy goes through a real tap rather than happening on load. */
async function copyText(text) {
  if (!text) { toast('没有可复制的内容'); return; }
  let ok = true;
  try {
    await navigator.clipboard.writeText(text);
  } catch (_) {
    // Fallback for browsers that refuse the async API outside a secure
    // context: a hidden textarea plus the older execCommand path.
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.select();
    try { ok = document.execCommand('copy'); } catch (_) { ok = false; }
    document.body.removeChild(ta);
  }
  toast(ok ? '已复制到手机' : '复制失败，请长按手动选择');
}

/* ---------------- polling ---------------- */

let lastNoteAt = 0;
let lastClip = '';
let lastImageSum = null; // null = never fetched; '' = clipboard has no image
let imageBlob = null;
let imageUrl = '';
let online = null;

async function refresh() {
  try {
    const [st, clip, note] = await Promise.all([
      api('/api/state').catch(() => null),
      api('/api/clip'),
      api('/api/note'),
    ]);

    setOnline(true);
    $('sub').textContent = st && st.device
      ? '已连接 · ' + st.device
      : '已连接 · 准备好接收';

    // Clipboard
    const clipText = clip.text || '';
    if (clipText !== lastClip) {
      lastClip = clipText;
      $('clipText').textContent = clipText || '（空）';
    }

    // The clipboard's image, signalled by a fingerprint on /api/clip so the
    // bytes are only fetched when the picture actually changed.
    await syncClipImage(clip.image || '');

    // A note only steals attention when it is new, so scrolling back to a
    // previously read note isn't fought by the poll.
    const noteText = note.text || '';
    if (noteText && note.at !== lastNoteAt) {
      lastNoteAt = note.at;
      $('noteCard').hidden = false;
      $('noteText').textContent = noteText;
    }
    if (!noteText) $('noteCard').hidden = true;
  } catch (e) {
    setOnline(false);
  }
}

// Swaps the clipboard card between text and picture. The text card stays for
// anything typed or copied as text — the page reads exactly as before when the
// clipboard never holds an image — and only gives way when the clipboard is a
// picture and there is no text to show beside it.
async function syncClipImage(sum) {
  if (sum === lastImageSum) return;
  // A poll may have re-read a newer fingerprint by the time the bytes arrive;
  // bail out of showing stale data for a sum we no longer want.
  const stale = (got) => got !== lastImageSum;
  lastImageSum = sum;

  if (!sum) {
    if (imageUrl) { URL.revokeObjectURL(imageUrl); imageUrl = ''; }
    imageBlob = null;
    $('clipImageCard').hidden = true;
    $('clipCard').hidden = false;
    return;
  }

  try {
    const res = await fetch('/api/clip/image', { cache: 'no-store' });
    if (stale(sum)) return;
    if (!res.ok) throw new Error('HTTP ' + res.status);
    const blob = await res.blob();
    if (stale(sum)) return;
    imageBlob = blob;
    if (imageUrl) URL.revokeObjectURL(imageUrl);
    imageUrl = URL.createObjectURL(blob);
    $('clipImage').src = imageUrl;
    $('clipImageCard').hidden = false;
    // A picture and no words: the text card would just read （空）.
    $('clipCard').hidden = !lastClip;
  } catch (e) {
    if (!stale(sum)) $('clipImageCard').hidden = true;
  }
}

// One switch drives the dot, the subtitle and the offline banner, so the
// page can never show "connected" in three places and "offline" in a fourth.
function setOnline(on) {
  if (on === online) return;
  online = on;
  $('connDot').classList.toggle('on', on);
  $('connDot').classList.toggle('err', !on);
  $('offline').hidden = on;
  if (!on) $('sub').textContent = '未连接到电脑';
}

/* ---------------- shared folder, with subfolders ---------------- */

let curSub = '';

function crumbFor(sub) {
  if (!sub) return '共享目录';
  const parts = sub.split('/').filter(Boolean);
  return '… / ' + parts.slice(-2).join(' / ');
}

async function loadFiles(sub) {
  curSub = sub || '';
  $('files').innerHTML =
    `<div class="fs-skel">${'<div class="skel fs-skel-row"></div>'.repeat(4)}</div>`;
  try {
    const q = curSub ? '?path=' + encodeURIComponent(curSub) : '';
    const d = await api('/api/shared' + q);
    const sub = d.sub || '';
    $('filePath').hidden = !sub;
    $('filePathCrumb').textContent = crumbFor(sub);
    $('files').innerHTML = (d.entries && d.entries.length)
      ? d.entries.map((e) => fileItem(e, sub)).join('')
      : `<div class="empty">
           <svg class="ic"><use href="#i-folder"/></svg>
           <span class="empty-title">这里没有文件</span>
         </div>`;
  } catch (e) {
    $('files').innerHTML = `<div class="empty">${esc(e.message)}</div>`;
  }
}

// Directories are buttons that navigate into the subfolder; files link
// straight to /dl, which knows how to stay inside the shared root.
function fileItem(e, sub) {
  const icon = e.dir ? 'i-folder' : 'i-doc';
  const subPath = sub ? sub.replace(/\/+$/, '') + '/' + e.name : e.name;
  if (e.dir) {
    return `<button class="p-item" data-dir="${esc(subPath)}">
      <svg class="ic sm-ic" style="color:var(--accent-text)"><use href="#${icon}"/></svg>
      <span class="p-item-main">
        <span class="p-item-title">${esc(e.name)}</span>
        <span class="p-item-sub">文件夹</span>
      </span>
      <svg class="ic sm-ic" style="color:var(--text-3)"><use href="#i-chev"/></svg>
    </button>`;
  }
  return `<a class="p-item" href="/dl?p=${encodeURIComponent(e.path)}">
    <svg class="ic sm-ic" style="color:var(--text-3)"><use href="#${icon}"/></svg>
    <span class="p-item-main">
      <span class="p-item-title">${esc(e.name)}</span>
      <span class="p-item-sub">${sizeText(e.size)}</span>
    </span>
    <svg class="ic sm-ic" style="color:var(--text-3)"><use href="#i-down"/></svg>
  </a>`;
}

$('files').addEventListener('click', (ev) => {
  const dir = ev.target.closest('[data-dir]');
  if (!dir) return;
  ev.preventDefault();
  loadFiles(dir.dataset.dir);
});

$('filesUp').onclick = () => {
  const parts = curSub.split('/').filter(Boolean);
  parts.pop();
  loadFiles(parts.join('/'));
};

/* ---------------- upload ---------------- */

let chosen = [];

$('fileInput').onchange = (e) => {
  chosen = Array.from(e.target.files || []);
  $('filePickText').textContent = chosen.length
    ? (chosen.length === 1 ? chosen[0].name : `已选 ${chosen.length} 个文件`)
    : '选择要上传的文件';
  $('upload').disabled = chosen.length === 0;
  $('upload').textContent = chosen.length
    ? `上传 ${chosen.length} 个文件`
    : '上传';
};

$('upload').onclick = () => {
  if (!chosen.length) return;

  const fd = new FormData();
  chosen.forEach((f) => fd.append('files', f, f.name));

  const xhr = new XMLHttpRequest();
  xhr.open('POST', '/upload');
  $('upProgress').hidden = false;
  $('upload').disabled = true;
  const reset = () => {
    $('upProgress').hidden = true;
    $('upBar').style.width = '0%';
    $('upload').disabled = false;
    $('upload').textContent = '上传';
  };

  xhr.upload.onprogress = (ev) => {
    if (!ev.lengthComputable) return;
    const pct = Math.round((ev.loaded / ev.total) * 100);
    $('upBar').style.width = pct + '%';
    $('upload').textContent = `上传中 ${pct}%`;
  };

  xhr.onload = () => {
    reset();
    chosen = [];
    $('fileInput').value = '';
    if (xhr.status === 200) {
      toast('已传到电脑');
      loadFiles(curSub);
    } else {
      // The server answers failures with a JSON error body: surface it, never
      // a bare "上传失败" that hides whether it was a bad request or a full
      // disk.
      let msg = '上传失败（HTTP ' + xhr.status + '）';
      try { msg = JSON.parse(xhr.responseText).error || msg; } catch (_) { }
      toast(msg);
    }
  };

  // A network-level failure never gets an HTTP status. Over adb reverse that
  // means either the cable/tunnel dropped, or the phone failed to read the
  // chosen file's contents — naming both beats a bare "失败".
  const streamDied = () => {
    reset();
    const n = chosen.length;
    chosen = [];
    $('fileInput').value = '';
    console.error('upload aborted: readyState=%d status=%d files=%d',
      xhr.readyState, xhr.status, n);
    toast('上传失败：连接中断。请检查数据线后重试；若刚选完文件，可能是这个文件读不出来，换个文件试试');
  };
  xhr.upload.onerror = streamDied;
  xhr.onerror = streamDied;

  xhr.send(fd);
};

/* ---------------- copy the clipboard image ---------------- */

// Writing an image needs the ClipboardItem form of the API, which is both
// newer and pickier than writeText: the WebView some ROMs ship (this project's
// reference phone serves pages from Chrome 75) has neither. Every failure must
// name itself — a silent dead button reads as "the bridge is broken" — and the
// save button below stays as the path that works everywhere.
$('copyImage').onclick = async () => {
  if (!imageBlob) { toast('没有可复制的图片'); return; }
  if (!navigator.clipboard || !window.ClipboardItem) {
    toast('这台手机的浏览器不支持复制图片，请用下面的「保存到手机」');
    return;
  }
  try {
    await navigator.clipboard.write([new ClipboardItem({ 'image/png': imageBlob })]);
    toast('图片已复制到手机');
  } catch (e) {
    const why = (e && e.name === 'NotAllowedError') ? '没有获得剪贴板权限'
      : (e && e.message) || '未知原因';
    toast('复制图片失败（' + why + '），请用「保存到手机」');
  }
};

// The download fallback: the endpoint marks the body as an attachment, which
// the WebView hands to the system DownloadManager and a browser downloads.
$('saveImage').onclick = () => {
  if (!imageBlob) { toast('没有可保存的图片'); return; }
  toast('已交给系统下载');
  window.location.href = '/api/clip/image?dl=1';
};

/* ---------------- wiring ---------------- */

$('copyClip').onclick = () => copyText(lastClip);

$('copyNote').onclick = () => copyText($('noteText').textContent);

$('sendMac').onclick = async () => {
  const text = $('toMac').value;
  if (!text.trim()) return;
  try {
    await post('/api/clip', { text });
    toast('已发送到电脑剪贴板');
    $('toMac').value = '';
    lastClip = ''; // force the next poll to re-read
  } catch (err) {
    toast('发送失败：' + err.message);
  }
};

$('reload').onclick = () => { loadFiles(curSub); refresh(); };

$('retryConn').onclick = () => { refresh(); loadFiles(curSub); };

/* ---------------- boot ---------------- */

refresh();
loadFiles('');
setInterval(refresh, 2500);
