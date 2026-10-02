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

  xhr.upload.onprogress = (ev) => {
    if (!ev.lengthComputable) return;
    const pct = Math.round((ev.loaded / ev.total) * 100);
    $('upBar').style.width = pct + '%';
    $('upload').textContent = `上传中 ${pct}%`;
  };

  xhr.onload = () => {
    $('upProgress').hidden = true;
    $('upBar').style.width = '0%';
    $('upload').disabled = false;
    chosen = [];
    $('fileInput').value = '';
    $('upload').textContent = '上传';
    if (xhr.status === 200) {
      toast('已传到电脑');
      loadFiles(curSub);
    } else {
      let msg = '上传失败';
      try { msg = JSON.parse(xhr.responseText).error || msg; } catch (_) { }
      toast(msg);
    }
  };

  xhr.onerror = () => {
    $('upProgress').hidden = true;
    $('upload').disabled = false;
    $('upload').textContent = '上传';
    toast('上传失败');
  };

  xhr.send(fd);
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
