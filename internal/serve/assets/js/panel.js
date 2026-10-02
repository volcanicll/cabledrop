/* CableDrop — desktop panel. Uses the shared helpers in api.js. */

function shortPath(p) {
  // Home-relative paths are far easier to read in a 380px panel.
  const m = /^\/Users\/[^/]+/.exec(p);
  return m ? '~' + p.slice(m[0].length) : String(p).replace(/^[A-Za-z]:\\Users\\[^\\]+/, '~');
}

/* ---------------- views ---------------- */

const VIEWS = ['home', 'files', 'text'];
let view = 'home';

function show(v) {
  if (v === view) return;
  view = v;
  VIEWS.forEach((name) => { $('v-' + name).hidden = name !== v; });
  // One shared enter transition; the section simply appears when the user
  // asks for reduced motion.
  const el = $('v-' + v);
  el.classList.remove('view-enter');
  void el.offsetWidth; // restart the animation
  el.classList.add('view-enter');
  $('scroll').scrollTop = 0;
}

/* ---------------- helpers ---------------- */

// "77G" from `df -h` becomes "77 GB"; anything unexpected passes through.
function fmtFree(s) {
  const m = /^([\d.]+)\s*([KMGT])B?$/i.exec(String(s || '').trim());
  if (!m) return s ? String(s) : '';
  const unit = { K: 'KB', M: 'MB', G: 'GB', T: 'TB' }[m[2].toUpperCase()];
  return m[1] + ' ' + unit;
}

function relTime(at) {
  if (!at) return '';
  const s = Math.max(0, Date.now() / 1000 - at);
  if (s < 45) return '刚刚';
  if (s < 3600) return Math.round(s / 60) + ' 分钟前';
  if (s < 86400) return Math.round(s / 3600) + ' 小时前';
  if (s < 172800) return '昨天';
  if (s < 604800) return Math.round(s / 86400) + ' 天前';
  const d = new Date(at * 1000);
  return (d.getMonth() + 1) + ' 月 ' + d.getDate() + ' 日';
}

function elapsedText(startedAt) {
  if (!startedAt) return '';
  const s = Math.max(0, Math.round((Date.now() - startedAt) / 1000));
  if (s < 60) return s + 's';
  return Math.floor(s / 60) + ':' + String(s % 60).padStart(2, '0');
}

/* ---------------- state ---------------- */

let St = null;
let busy = false;

function render(s) {
  const first = St === null;
  St = s;
  if (first) {
    document.body.classList.remove('is-loading');
    $('skeleton').remove();
  }

  // top bar — the phone's real name is the headline; the serial never is.
  const on = s.connected;
  const glyph = $('devGlyph');
  glyph.classList.toggle('off', !on);
  const name = $('devName');
  if (!s.adbFound) {
    name.textContent = '未找到 adb';
    name.classList.add('off');
    name.title = '';
  } else if (on) {
    name.textContent = s.device || '已连接手机';
    name.classList.remove('off');
    // The serial is secondary information: useful when two identical phones
    // are around, never worth a line of its own.
    name.title = s.serial ? '序列号 ' + s.serial : '';
  } else {
    name.textContent = '未连接手机';
    name.classList.add('off');
    name.title = '';
  }
  $('devSub').innerHTML = !s.adbFound
    ? '请安装 Android Platform Tools'
    : on ? `<span class="dot on"></span>USB 已连接` + (s.storageFree ? ` · ${esc(fmtFree(s.storageFree))} 可用` : '')
         : `<span class="dot"></span>插上 USB 线，手机选「传输文件」`;

  // The diagram's Android end lights up with the link; refresh/close keep a
  // deliberately lower visual weight than the device identity.
  $('linkDiagram').classList.toggle('off', !on);

  // tiles
  $('tFiles').disabled = !on;
  $('tSend').disabled = !on;
  $('tText').setAttribute('aria-disabled', 'false');

  const serving = s.serving;
  $('tServe').classList.toggle('live', serving);
  $('tServe').setAttribute('aria-disabled', (!on && !serving) ? 'true' : 'false');
  // The real port, whatever the fallback picked; shown as secondary info.
  $('serveURL').textContent = s.phoneURL && s.phoneURL !== 'http://localhost:0'
    ? s.phoneURL.replace(/^http:\/\//, '')
    : 'localhost:' + (s.port || 8765);

  // shared dir
  $('dirPath').textContent = shortPath(s.serveDir);

  // error card
  renderError(s);

  // transfers — patched incrementally, never rebuilt wholesale. The error
  // card is a sibling that shares the fixed 540px height, so when it is up
  // the list yields a row: 380×540 must never scroll.
  renderTransfers(s.transfers || [], s.error || (view === 'home' && !s.adbFound) ? 2 : TX_VISIBLE);
}

/* ---------------- error card ---------------- */

function renderError(s) {
  const box = $('errorBox');
  let msg = s.error;

  if (!msg && view === 'home' && !s.adbFound) {
    msg = '没有找到 adb\n安装 Android Platform Tools，或运行 brew install android-platform-tools';
  }
  // Every card that appears here describes a state a refresh may clear, so
  // each one carries the retry action.
  const action = msg ? '重试' : null;

  if (!msg) {
    box.hidden = true;
    box.innerHTML = '';
    return;
  }
  box.hidden = false;
  // First line is what to do; the rest is secondary technical detail.
  const lines = msg.split('\n');
  const head = esc(lines[0]);
  const detail = lines.length > 1
    ? `<span class="error-detail">${esc(lines.slice(1).join('\n'))}</span>` : '';
  const next = `<div class="error-card" role="alert">
    <svg class="ic"><use href="#i-alert"/></svg>
    <span class="error-body">${head}${detail}</span>
    ${action ? `<button class="btn sm ghost" id="errorRetry">${action}</button>` : ''}
  </div>`;
  if (box.dataset.sig !== next) {
    box.dataset.sig = next;
    box.innerHTML = next;
    const retry = $('errorRetry');
    if (retry) retry.onclick = async () => {
      try { render(await post('/api/refresh')); } catch (_) { /* next poll retries */ }
    };
  }
}

/* ---------------- transfers, incremental ---------------- */

// txRows maps transfer id → its row element plus a signature of everything
// the row displays, so unchanged rows are untouched across polls.
const txRows = new Map();
// The list shows the three newest transfers until asked for everything —
// the 380×540 panel cannot spare the height for all forty records. When the
// error card is also on screen the cap drops to two, so the two never push
// the panel into a scroll.
const TX_VISIBLE = 3;
let txExpanded = false;
let txCap = TX_VISIBLE;

function relTimeKey(t) {
  // Relative time moves without a state change; bucket it so the row only
  // re-patches when the displayed string actually changes.
  return Math.floor((Date.now() / 1000 - t) / 60);
}

function txSig(t) {
  return [t.state, t.detail || '', t.percent ?? '', t.indeterminate ? 1 : 0,
          t.indeterminate ? elapsedText(t.startedAt) : '', t.size ?? '',
          relTimeKey(t.at)].join('|');
}

function renderTransfers(tx, cap) {
  const card = $('txCard');
  const clearBtn = $('clearTx');
  const toggleBtn = $('toggleTx');
  if (cap != null) txCap = cap;

  if (!tx.length) {
    for (const rec of txRows.values()) rec.el.remove();
    txRows.clear();
    if (!card.querySelector('.empty')) {
      card.innerHTML = `<div class="empty">
        <svg class="ic"><use href="#i-send"/></svg>
        <span class="empty-title">还没有传输记录</span>
        <span class="empty-hint">拖文件到这里，或点「发送文件」</span>
      </div>`;
    }
    clearBtn.hidden = true;
    toggleBtn.hidden = true;
    return;
  }
  const empty = card.querySelector('.empty');
  if (empty) empty.remove();

  const visible = txExpanded ? tx.length : Math.min(txCap, tx.length);
  const shown = tx.slice(0, visible);

  let ref = card.firstElementChild;
  for (const t of shown) {
    let rec = txRows.get(t.id);
    if (!rec) {
      const el = document.createElement('div');
      el.className = 'tx';
      el.dataset.txId = t.id;
      el.innerHTML = `<span class="tx-chip"><svg class="ic"><use href="#i-up"/></svg></span>
        <span class="tx-main">
          <span class="tx-name"></span>
          <span class="tx-sub"></span>
        </span>
        <span class="tx-side">
          <span class="tx-size"></span>
          <span class="tx-time"></span>
        </span>
        <span class="tx-state"></span>
        <span class="tx-chev"><svg class="ic"><use href="#i-chev-r"/></svg></span>`;
      txRows.set(t.id, rec = { el, sig: '' });
    }
    const sig = txSig(t);
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

  clearBtn.hidden = !tx.some((t) => t.state !== 'running');
  if (tx.length > txCap) {
    toggleBtn.hidden = false;
    toggleBtn.textContent = txExpanded ? '收起' : `查看全部 ${tx.length}`;
  } else {
    toggleBtn.hidden = true;
  }
}

function patchTxRow(el, t) {
  // Direction decides the chip; state decides the icon inside it.
  const dirClass = t.kind === 'push' ? 'push' : 'pull';
  const chip = el.querySelector('.tx-chip');
  const icon = t.state === 'running' ? 'i-refresh'
    : t.state === 'failed' ? 'i-alert'
    : t.kind === 'push' ? 'i-up' : 'i-down';
  chip.className = 'tx-chip ' + (t.state === 'failed' ? 'failed' : dirClass + (t.state === 'running' ? ' running' : ''));
  chip.innerHTML = `<svg class="ic"><use href="#${icon}"/></svg>`;

  el.querySelector('.tx-name').textContent = t.name;

  // Sub line: what happened and where, in that order. Failed rows show the
  // human-readable first line of the error instead.
  const sub = el.querySelector('.tx-sub');
  if (t.state === 'failed' && t.detail) {
    sub.textContent = t.detail.split('\n')[0];
  } else if (t.state === 'running' && t.percent == null) {
    sub.textContent = (t.kind === 'push' ? '发送中' : '接收中') + ' · 已用 ' + elapsedText(t.startedAt);
  } else {
    sub.textContent = t.detail || (t.kind === 'push' ? '发送到手机' : '从手机取回');
  }

  el.querySelector('.tx-size').textContent = t.size ? sizeText(t.size) : '';

  const time = el.querySelector('.tx-time');
  time.textContent = t.state === 'running' && t.indeterminate
    ? '已用 ' + elapsedText(t.startedAt) : relTime(t.at);
  if (t.state === 'running' && t.indeterminate) time.textContent = '';

  const state = el.querySelector('.tx-state');
  if (t.state === 'running') {
    if (t.percent != null) {
      state.innerHTML = `<span class="tx-pct">${Math.round(t.percent)}%</span>
        <span class="tx-bar"><i></i></span>`;
      state.querySelector('.tx-bar i').style.width =
        Math.max(4, Math.min(100, t.percent)) + '%';
    } else {
      state.innerHTML = `<span class="tx-pct">传输中</span>
        <span class="tx-bar indet"><i></i></span>`;
    }
  } else if (t.state === 'done') {
    state.innerHTML = `<span class="badge done"><span class="b-dot"><svg class="ic"><use href="#i-check"/></svg></span>已完成</span>`;
  } else if (t.state === 'failed') {
    state.innerHTML = `<span class="badge failed"><span class="b-dot"><svg class="ic"><use href="#i-alert"/></svg></span>失败</span>`;
  }
}

/* ---------------- polling ---------------- */

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

function fileListSkeleton() {
  return `<div class="fs-skel">${'<div class="skel fs-skel-row"></div>'.repeat(6)}</div>`;
}

async function loadFiles(dir) {
  $('filesList').innerHTML = fileListSkeleton();
  try {
    const d = await api('/api/device/files?path=' + encodeURIComponent(dir));
    curDir = d.path;
    curParent = d.parent;
    $('filesPath').textContent = shortPath(d.path);
    $('filesList').innerHTML = (d.entries && d.entries.length)
      ? d.entries.map(fileRow).join('')
      : `<div class="empty">
           <svg class="ic"><use href="#i-folder"/></svg>
           <span class="empty-title">（空目录）</span>
         </div>`;
  } catch (e) {
    $('filesList').innerHTML = `<div class="empty">${esc(e.message)}</div>`;
  }
}

function fileRow(en) {
  const icon = en.dir ? 'i-folder' : 'i-doc';
  const action = en.dir ? '' : '<span class="row-meta">取回</span>';
  return `<div class="row" data-path="${esc(en.path)}" data-dir="${en.dir ? 1 : 0}">
    <svg class="ic sm-ic" style="color:${en.dir ? 'var(--accent-text)' : 'var(--text-3)'}">
      <use href="#${icon}"/></svg>
    <span class="row-main">
      <span class="row-title">${esc(en.name)}</span>
      <span class="row-sub">${en.dir ? '文件夹' : sizeText(en.size)}</span>
    </span>
    ${action}
    <button class="icon-btn" data-del="${esc(en.path)}" title="删除" aria-label="删除 ${esc(en.name)}">
      <svg class="ic sm-ic"><use href="#i-trash"/></svg>
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
// button all end up at; Escape is native (HideOnEscape).
const hidePanel = () => { post('/api/panel/hide').catch(() => { }); };

$('panelClose').onclick = hidePanel;

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

// The two div-tiles: same activation surface as a button, because they carry
// nested interactive controls a real <button> cannot contain.
function wireTile(el, onActivate) {
  el.addEventListener('click', (ev) => {
    if (ev.target.closest('button')) return; // inner controls first
    if (el.getAttribute('aria-disabled') === 'true') return;
    onActivate(ev);
  });
  el.addEventListener('keydown', (ev) => {
    if (ev.key !== 'Enter' && ev.key !== ' ') return;
    if (ev.target.closest('button')) return;
    ev.preventDefault();
    if (el.getAttribute('aria-disabled') === 'true') return;
    onActivate(ev);
  });
}

wireTile($('tText'), () => { show('text'); refreshClip(); });

wireTile($('tServe'), async () => {
  try {
    render(await post('/api/serve', { on: !(St && St.serving) }));
  } catch (e) { alert(e.message); }
});

$('copyURL').onclick = async (ev) => {
  ev.stopPropagation();
  try {
    await navigator.clipboard.writeText(St ? St.phoneURL : $('serveURL').textContent);
    const btn = ev.currentTarget;
    btn.textContent = '已复制';
    setTimeout(() => { btn.textContent = '复制'; }, 1200);
  } catch (_) { /* clipboard may be blocked in the webview */ }
};

$('changeDir').onclick = async () => {
  try {
    const picked = await post('/api/pick/dir');
    if (picked.path) render(await post('/api/shared/dir', { dir: picked.path }));
  } catch (e) { alert(e.message); }
};

$('clearTx').onclick = async () => { render(await post('/api/transfers/clear')); };

$('toggleTx').onclick = () => {
  txExpanded = !txExpanded;
  if (St) renderTransfers(St.transfers || []);
};

// A row is a disclosure: the chevron points at the one thing the panel had
// to hide — the tail of a long name or a failure detail.
$('txCard').addEventListener('click', (ev) => {
  const row = ev.target.closest('.tx');
  if (row) row.classList.toggle('open');
});

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
  setTimeout(() => {
    $('textHint').textContent = '发送后，在手机网页顶部就能看到并复制。';
  }, 3000);
};

/* ---------------- drag feedback ---------------- */

// The window-level file drop is native (Wails hands us real paths); this is
// the visual acknowledgment for the moments the webview does see the drag.
// The send tile becomes the target while hovering it; everywhere else the
// whole-panel overlay covers it. A counter handles nested enter/leave pairs.
let dragDepth = 0;
let hotDrop = false;
const overlay = document.createElement('div');
overlay.className = 'drop-overlay';
overlay.textContent = '松开即可发送到手机';
overlay.hidden = true;
document.body.appendChild(overlay);

const sendTile = $('tSend');
const dropTag = sendTile.querySelector('.drop-tag');

function setHot(hot) {
  hotDrop = hot;
  sendTile.classList.toggle('drop-hot', hot);
  dropTag.hidden = !hot;
  overlay.hidden = hot || dragDepth === 0;
}

addEventListener('dragenter', (ev) => {
  ev.preventDefault();
  dragDepth++;
  setHot(ev.target && ev.target.closest && ev.target.closest('#tSend'));
});
addEventListener('dragover', (ev) => {
  ev.preventDefault();
  const over = ev.target && ev.target.closest && ev.target.closest('#tSend');
  if (over !== hotDrop) setHot(over);
});
addEventListener('dragleave', (ev) => {
  dragDepth = Math.max(0, dragDepth - 1);
  if (dragDepth === 0) setHot(false);
});
addEventListener('drop', () => {
  dragDepth = 0;
  setHot(false);
});

/* ---------------- boot ---------------- */

poll(true);
setInterval(poll, 2000);
setInterval(() => { if (view === 'text') refreshClip(); }, 2000);
