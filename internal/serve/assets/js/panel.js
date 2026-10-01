/* CableDrop — desktop panel. Uses the shared helpers in api.js. */

function shortPath(p) {
  // Home-relative paths are far easier to read in a 380px panel.
  const m = /^\/Users\/[^/]+/.exec(p);
  return m ? '~' + p.slice(m[0].length) : String(p).replace(/^[A-Za-z]:\\Users\\[^\\]+/, '~');
}

/* ---------------- views ---------------- */

const VIEWS = ['home', 'files', 'text'];

function show(v) {
  view = v;
  VIEWS.forEach((name) => { $('v-' + name).hidden = name !== v; });
  $('scroll').scrollTop = 0;
}

/* ---------------- state ---------------- */

let St = null;
let busy = false;
let view = 'home';

function render(s) {
  St = s;

  // header
  const on = s.connected;
  $('glyph').classList.toggle('off', !on);
  $('devName').textContent = !s.adbFound ? '找不到 adb'
    : on ? s.device : '未连接手机';
  $('devSub').innerHTML = !s.adbFound
    ? '请安装 Android Platform Tools'
    : on ? `<span class="dot on"></span>已连接` + (s.storageFree ? ` · 剩余 ${esc(s.storageFree)}` : '')
         : '<span class="dot"></span>插上 USB 线，手机选「传输文件」';

  // tiles
  $('tFiles').disabled = !on;
  $('tSend').disabled = !on;

  const serving = s.serving;
  $('tServe').classList.toggle('live', serving);
  $('tServe').disabled = !on && !serving;
  $('serveTitle').textContent = serving ? '网页已开启' : '手机网页';
  // Short on purpose: the tile is half the panel wide, and the full URL is
  // shown in the section below when the server is up.
  $('serveSub').textContent = serving ? 'localhost:' + s.port : '在手机上打开';

  // shared dir
  $('dirPath').textContent = shortPath(s.serveDir);

  // transfers — patched incrementally: rebuilding this list every two
  // seconds flickered and threw away the scroll position mid-read.
  renderTransfers(s.transfers || []);

  // error
  $('errorBox').innerHTML = s.error
    ? `<div class="alert"><svg class="ic"><use href="#i-alert"/></svg><span>${esc(s.error)}</span></div>`
    : view === 'home' && !s.adbFound
      ? `<div class="alert"><svg class="ic"><use href="#i-alert"/></svg><span>没有找到 adb。装一个 Android Studio，或 <code>brew install android-platform-tools</code>。</span></div>`
      : '';

  $('footAdb').textContent = !s.adbFound ? '未找到 adb' : (s.connected ? '' : '等待设备');
}

/* ---------------- transfers, incremental ---------------- */

// txRows maps transfer id → its row element plus a signature of everything
// the row displays, so unchanged rows are untouched across polls.
const txRows = new Map();

function elapsedText(startedAt) {
  if (!startedAt) return '';
  const s = Math.max(0, Math.round((Date.now() - startedAt) / 1000));
  if (s < 60) return s + 's';
  return Math.floor(s / 60) + ':' + String(s % 60).padStart(2, '0');
}

function renderTransfers(tx) {
  const card = $('txCard');

  if (!tx.length) {
    for (const rec of txRows.values()) rec.el.remove();
    txRows.clear();
    if (!card.querySelector('.empty')) {
      card.innerHTML = '<div class="empty">还没有传输记录</div>';
    }
    $('clearTx').hidden = true;
    return;
  }
  const empty = card.querySelector('.empty');
  if (empty) empty.remove();

  let ref = card.firstElementChild;
  for (const t of tx) {
    let rec = txRows.get(t.id);
    if (!rec) {
      const el = document.createElement('div');
      el.className = 'tx';
      el.dataset.txId = t.id;
      el.innerHTML = `<svg class="ic tx-ic"><use href="#i-up"/></svg>
        <span class="row-main">
          <span class="row-title"></span>
          <span class="row-sub"></span>
        </span>
        <span class="tx-pct" hidden></span>
        <span class="tx-bar" hidden><i></i></span>`;
      txRows.set(t.id, rec = { el, sig: '' });
    }
    const sig = [t.state, t.detail || '', t.percent ?? '',
                 t.indeterminate ? 1 : 0, t.indeterminate ? elapsedText(t.startedAt) : ''].join('|');
    if (rec.sig !== sig) {
      patchTxRow(rec.el, t);
      rec.sig = sig;
    }
    if (rec.el === ref) {
      ref = ref.nextSibling;
    } else {
      card.insertBefore(rec.el, ref);
    }
  }
  // Anything still queued after the placed rows is stale.
  while (ref) {
    const next = ref.nextSibling;
    if (ref.dataset && ref.dataset.txId) txRows.delete(ref.dataset.txId);
    ref.remove();
    ref = next;
  }

  $('clearTx').hidden = !tx.some((t) => t.state !== 'running');
}

function patchTxRow(el, t) {
  const icon = t.state === 'running' ? 'i-refresh'
    : t.state === 'failed' ? 'i-alert'
    : t.kind === 'push' ? 'i-up' : 'i-down';
  el.querySelector('.tx-ic').outerHTML =
    `<svg class="ic tx-ic ${t.state}"><use href="#${icon}"/></svg>`;
  el.querySelector('.row-title').textContent = t.name;

  const sub = el.querySelector('.row-sub');
  const pct = el.querySelector('.tx-pct');
  const bar = el.querySelector('.tx-bar');

  if (t.state === 'running') {
    if (t.percent != null) {
      sub.textContent = '';
      pct.hidden = false;
      pct.textContent = Math.round(t.percent) + '%';
      bar.hidden = false;
      bar.classList.remove('indet');
      bar.firstElementChild.style.width = Math.max(2, Math.min(100, t.percent)) + '%';
    } else {
      sub.textContent = '传输中 · ' + elapsedText(t.startedAt);
      pct.hidden = true;
      bar.hidden = false;
      bar.classList.add('indet');
      bar.firstElementChild.style.width = '';
    }
  } else {
    sub.textContent = t.detail || '';
    pct.hidden = true;
    bar.hidden = true;
  }
}

async function poll(force) {
  if (busy && !force) return;
  busy = true;
  try {
    render(await api('/api/state'));
  } catch (e) {
    // A failed poll usually means the process is going away; keep the last
    // good render on screen rather than blanking the panel.
  } finally {
    busy = false;
  }
}

/* ---------------- device files ---------------- */

let curDir = '/sdcard';
let curParent = '';

async function loadFiles(dir) {
  $('filesList').innerHTML = '<div class="empty">读取中…</div>';
  try {
    const d = await api('/api/device/files?path=' + encodeURIComponent(dir));
    curDir = d.path;
    curParent = d.parent;
    $('filesPath').textContent = shortPath(d.path);
    $('filesList').innerHTML = (d.entries && d.entries.length)
      ? d.entries.map(fileRow).join('')
      : '<div class="empty">（空目录）</div>';
  } catch (e) {
    $('filesList').innerHTML = `<div class="empty">${esc(e.message)}</div>`;
  }
}

function fileRow(en) {
  const icon = en.dir ? 'i-folder' : 'i-doc';
  const action = en.dir ? '' : '<span class="row-meta">取回</span>';
  return `<div class="row" data-path="${esc(en.path)}" data-dir="${en.dir ? 1 : 0}">
    <svg class="ic" style="width:16px;height:16px;color:${en.dir ? 'var(--accent)' : 'var(--muted)'}">
      <use href="#${icon}"/></svg>
    <span class="row-main">
      <span class="row-title">${esc(en.name)}</span>
      <span class="row-sub">${en.dir ? '文件夹' : sizeText(en.size)}</span>
    </span>
    ${action}
    <button class="icon-btn" data-del="${esc(en.path)}" title="删除" style="width:24px;height:24px">
      <svg class="ic" style="width:14px;height:14px"><use href="#i-trash"/></svg>
    </button>
  </div>`;
}

/* ---------------- text ---------------- */

async function refreshClip() {
  try {
    const d = await api('/api/clip');
    $('clipView').textContent = d.text || '（空）';
  } catch (_) { /* leave as is */ }
}

/* ---------------- wiring ---------------- */

// Dismiss the panel. Same call the tray icon, an outside click and the ×
// button all end up at; the page only needs it for Escape.
const hidePanel = () => { post('/api/panel/hide').catch(() => { }); };

$('panelClose').onclick = hidePanel;

document.addEventListener('keydown', (ev) => {
  if (ev.key === 'Escape' && St && St.native) hidePanel();
});

$('openDir').onclick = async () => {
  try { await post('/api/shared/open'); } catch (e) { alert(e.message); }
};

$('refresh').onclick = async (ev) => {
  const btn = ev.currentTarget;
  btn.classList.add('spin');
  try { render(await post('/api/refresh')); } catch (_) { }
  btn.classList.remove('spin');
};

$('tFiles').onclick = () => { show('files'); loadFiles(curDir); };

$('tSend').onclick = async () => {
  try {
    const picked = await post('/api/pick/files');
    if (picked.paths && picked.paths.length) {
      await post('/api/send', { paths: picked.paths });
      poll(true);
    }
  } catch (e) { alert(e.message); }
};

$('tServe').onclick = async () => {
  try {
    render(await post('/api/serve', { on: !(St && St.serving) }));
  } catch (e) { alert(e.message); }
};

$('tText').onclick = () => { show('text'); refreshClip(); };

$('changeDir').onclick = async () => {
  try {
    const picked = await post('/api/pick/dir');
    if (picked.path) render(await post('/api/shared/dir', { dir: picked.path }));
  } catch (e) { alert(e.message); }
};

$('clearTx').onclick = async () => { render(await post('/api/transfers/clear')); };

$('filesBack').onclick = () => show('home');
$('filesUp').onclick = () => { if (curParent) loadFiles(curParent); };

$('filesList').onclick = async (ev) => {
  const del = ev.target.closest('[data-del]');
  if (del) {
    ev.stopPropagation();
    const p = del.dataset.del;
    if (!confirm('删除「' + p.split('/').pop() + '」？此操作无法撤销。')) return;
    try {
      await post('/api/device/delete', { path: p });
      loadFiles(curDir);
    } catch (e) { alert(e.message); }
    return;
  }
  const row = ev.target.closest('.row[data-path]');
  if (!row) return;
  if (row.dataset.dir === '1') loadFiles(row.dataset.path);
  else {
    await post('/api/device/pull', { path: row.dataset.path });
    poll(true);
  }
};

$('textBack').onclick = () => show('home');

$('useClip').onclick = async () => {
  const d = await api('/api/clip');
  $('textOut').value = d.text || '';
  $('textOut').focus();
};

$('copyClip').onclick = async () => {
  const d = await api('/api/clip');
  try {
    await navigator.clipboard.writeText(d.text || '');
    $('copyClip').textContent = '已复制';
    setTimeout(() => { $('copyClip').textContent = '复制'; }, 1200);
  } catch (_) { /* clipboard may be blocked in the webview */ }
};

$('sendText').onclick = async () => {
  const text = $('textOut').value;
  if (!text) return;
  await post('/api/note', { text });
  $('textHint').textContent = '已发送 · 现在在手机上打开网页即可复制';
  $('textHint').style.color = 'var(--ok)';
  setTimeout(() => {
    $('textHint').textContent = '发送后，在手机网页顶部就能看到并复制。';
    $('textHint').style.color = '';
  }, 3000);
};

/* ---------------- boot ---------------- */

poll(true);
setInterval(poll, 2000);
setInterval(() => { if (view === 'text') refreshClip(); }, 2000);
