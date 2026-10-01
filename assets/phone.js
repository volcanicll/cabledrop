/* USBBridge — phone page.
 *
 * Reached at http://localhost:<port> on the device, tunnelled over the USB
 * cable by `adb reverse`. `localhost` counts as a secure context, which is
 * what makes navigator.clipboard available here — the reason the clipboard is
 * handed over through this page instead of written by adb, which Android 10
 * and later forbid.
 */

const $ = (id) => document.getElementById(id);

async function api(path, opts) {
  const res = await fetch(path, Object.assign({ cache: 'no-store' }, opts));
  const raw = await res.text();
  let data = null;
  try { data = raw ? JSON.parse(raw) : null; } catch (_) { /* non-JSON body */ }
  if (!res.ok) throw new Error((data && data.error) || `HTTP ${res.status}`);
  return data;
}

const post = (path, body) => api(path, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify(body || {}),
});

const esc = (s) => String(s == null ? '' : s).replace(/[&<>"']/g,
  (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

function sizeText(n) {
  if (!n) return '';
  const u = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
  return (i === 0 ? n : n.toFixed(n < 10 ? 1 : 0)) + ' ' + u[i];
}

/* Copying needs a user gesture on iOS and some Android browsers, so every
 * copy goes through a real tap rather than happening on load. The button's own
 * markup is restored afterwards, which is why the button — not the text — is
 * the second argument. */
async function copyText(text, btn) {
  if (!text) return;
  const original = btn.innerHTML;
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
    try { document.execCommand('copy'); } catch (_) { }
    document.body.removeChild(ta);
  }
  btn.textContent = '✓ 已复制';
  setTimeout(() => { btn.innerHTML = original; }, 1400);
}

/* ---------------- polling ---------------- */

let lastNoteAt = 0;
let lastClip = '';

async function refresh() {
  try {
    const [st, clip, note] = await Promise.all([
      api('/api/state').catch(() => null),
      api('/api/clip'),
      api('/api/note'),
    ]);

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
    $('sub').textContent = '未连接到电脑';
  }
}

function loadFiles() {
  return api('/api/shared').then((d) => {
    $('files').innerHTML = (d.entries && d.entries.length)
      ? d.entries.map((e) => `
        <a class="p-item" href="${e.dir ? '#' : '/dl?p=' + encodeURIComponent(e.path)}"
           ${e.dir ? 'data-dir="1"' : ''}>
          <svg class="ic" style="width:17px;height:17px;color:${e.dir ? 'var(--accent)' : 'var(--muted)'}">
            <use href="#${e.dir ? 'i-folder' : 'i-doc'}"/></svg>
          <span class="p-item-main">
            <span class="p-item-title">${esc(e.name)}</span>
            <span class="p-item-sub">${e.dir ? '文件夹' : sizeText(e.size)}</span>
          </span>
          ${e.dir ? '' : '<svg class="ic" style="width:16px;height:16px;color:var(--faint)"><use href="#i-down"/></svg>'}
        </a>`).join('')
      : '<div class="empty">电脑上还没有文件</div>';
  }).catch((e) => {
    $('files').innerHTML = `<div class="empty">${esc(e.message)}</div>`;
  });
}

/* ---------------- upload ---------------- */

let chosen = [];

$('fileInput').onchange = (e) => {
  chosen = Array.from(e.target.files || []);
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
    if (xhr.status === 200) {
      $('upload').textContent = '✓ 已传到电脑';
      setTimeout(() => { $('upload').textContent = '上传'; }, 1800);
      loadFiles();
    } else {
      let msg = '上传失败';
      try { msg = JSON.parse(xhr.responseText).error || msg; } catch (_) { }
      $('upload').textContent = msg;
      setTimeout(() => { $('upload').textContent = '上传'; }, 2600);
    }
  };

  xhr.onerror = () => {
    $('upProgress').hidden = true;
    $('upload').disabled = false;
    $('upload').textContent = '上传失败';
    setTimeout(() => { $('upload').textContent = '上传'; }, 2600);
  };

  xhr.send(fd);
};

/* ---------------- wiring ---------------- */

$('copyClip').onclick = (e) => copyText(lastClip, e.currentTarget);

$('copyNote').onclick = (e) => copyText($('noteText').textContent, e.currentTarget);

$('sendMac').onclick = async (e) => {
  const text = $('toMac').value;
  if (!text.trim()) return;
  const btn = e.currentTarget;
  try {
    await post('/api/clip', { text });
    btn.textContent = '✓ 已发送到电脑';
    $('toMac').value = '';
    lastClip = ''; // force the next poll to re-read
    setTimeout(() => { btn.innerHTML = '<svg class="ic" style="width:16px;height:16px"><use href="#i-send"/></svg>发送到电脑'; }, 1600);
  } catch (err) {
    btn.textContent = '发送失败';
    setTimeout(() => { btn.innerHTML = '<svg class="ic" style="width:16px;height:16px"><use href="#i-send"/></svg>发送到电脑'; }, 1600);
  }
};

$('reload').onclick = () => { loadFiles(); refresh(); };

/* ---------------- boot ---------------- */

refresh();
loadFiles();
setInterval(refresh, 2500);
