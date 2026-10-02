/* CableDrop — desktop panel. Uses the shared helpers in api.js and i18n.js. */

function shortPath(p) {
  // Home-relative paths are far easier to read in a 500px panel.
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
  if (s < 45) return t('panel.time.justNow');
  if (s < 3600) return t('panel.time.minAgo', { n: Math.round(s / 60) });
  if (s < 86400) return t('panel.time.hourAgo', { n: Math.round(s / 3600) });
  if (s < 172800) return t('panel.time.yesterday');
  if (s < 604800) return t('panel.time.dayAgo', { n: Math.round(s / 86400) });
  const d = new Date(at * 1000);
  return t('panel.time.date', { m: d.getMonth() + 1, d: d.getDate() });
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
    name.textContent = t('panel.dev.noAdb');
    name.classList.add('off');
    name.title = '';
  } else if (on) {
    name.textContent = s.device || t('panel.dev.connected');
    name.classList.remove('off');
    // The serial is secondary information: useful when two identical phones
    // are around, never worth a line of its own.
    name.title = s.serial ? t('panel.dev.serial', { serial: s.serial }) : '';
  } else {
    name.textContent = t('panel.dev.disconnected');
    name.classList.add('off');
    name.title = '';
  }
  renderSub(s);

  // The diagram dims when the link is down; refresh/close keep a
  // deliberately lower visual weight than the device identity.
  $('linkDiagram').classList.toggle('off', !on);

  // tiles
  $('tFiles').disabled = !on;
  $('tSend').disabled = !on;
  $('tText').setAttribute('aria-disabled', 'false');

  const serving = s.serving;
  $('tServe').setAttribute('aria-disabled', (!on && !serving) ? 'true' : 'false');
  // The real URL, scheme included: it goes into a browser, and the phone page
  // says to type exactly this. Shown even when idle — it is the tile's subject.
  $('serveURL').textContent = s.phoneURL && s.phoneURL !== 'http://localhost:0'
    ? s.phoneURL
    : 'http://localhost:' + (s.port || 8765);

  // shared dir — <bdi> keeps the path reading order intact inside the
  // rtl-direction container (which puts the ellipsis at the head-hiding end).
  $('dirPath').innerHTML = '<bdi>' + esc(shortPath(s.serveDir)) + '</bdi>';

  // error card
  renderError(s);

  // transfers — patched incrementally, never rebuilt wholesale. The error
  // card is a sibling that shares the fixed 740px height, so when it is up
  // the list yields a row: 500×740 must never scroll.
  renderTransfers(s.transfers || [], s.error || (view === 'home' && !s.adbFound) ? 2 : TX_VISIBLE);
}

// The subtitle under the device name. A function of the state alone, so the
// pre-poll boot, every poll and a language switch all land on the same words.
function renderSub(s) {
  const el = $('devSub');
  if (s && !s.adbFound) {
    el.textContent = t('panel.dev.installAdb');
  } else if (s && s.connected) {
    el.innerHTML = '<span class="dot on"></span>' + esc(s.storageFree
      ? t('panel.dev.connectedFree', { free: fmtFree(s.storageFree) })
      : t('panel.dev.usbConnected'));
  } else {
    el.innerHTML = '<span class="dot"></span>' + esc(t('panel.sub.unplugged'));
  }
}

/* ---------------- error card ---------------- */

function renderError(s) {
  const box = $('errorBox');
  let msg = s.error;

  if (!msg && view === 'home' && !s.adbFound) {
    msg = t('panel.error.noAdb');
  }
  // Every card that appears here describes a state a refresh may clear, so
  // each one carries the retry action.
  const action = msg ? t('common.retry') : null;

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
    ${action ? `<button class="btn sm ghost" id="errorRetry">${esc(action)}</button>` : ''}
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
// the 500×740 panel cannot spare the height for all forty records. When the
// error card is also on screen the cap drops to two, so the two never push
// the panel into a scroll.
const TX_VISIBLE = 3;
let txExpanded = false;
let txCap = TX_VISIBLE;

function relTimeKey(at) {
  // Relative time moves without a state change; bucket it so the row only
  // re-patches when the displayed string actually changes.
  return Math.floor((Date.now() / 1000 - at) / 60);
}

function txSig(tr) {
  return [tr.state, tr.detail || '', tr.percent ?? '', tr.indeterminate ? 1 : 0,
          tr.indeterminate ? elapsedText(tr.startedAt) : '', tr.size ?? '',
          relTimeKey(tr.at)].join('|');
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
        <span class="empty-title">${esc(t('panel.tx.emptyTitle'))}</span>
        <span class="empty-hint">${esc(t('panel.tx.emptyHint'))}</span>
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
  for (const tr of shown) {
    let rec = txRows.get(tr.id);
    if (!rec) {
      const el = document.createElement('div');
      el.className = 'tx';
      el.dataset.txId = tr.id;
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
      txRows.set(tr.id, rec = { el, sig: '' });
    }
    const sig = txSig(tr);
    if (rec.sig !== sig) {
      patchTxRow(rec.el, tr);
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

  clearBtn.hidden = !tx.some((tr) => tr.state !== 'running');
  if (tx.length > txCap) {
    toggleBtn.hidden = false;
    toggleBtn.textContent = txExpanded ? t('panel.tx.collapse')
      : t('panel.tx.showAll', { n: tx.length });
  } else {
    toggleBtn.hidden = true;
  }
}

function patchTxRow(el, tr) {
  // Direction decides the chip; state decides the icon inside it.
  const dirClass = tr.kind === 'push' ? 'push' : 'pull';
  const chip = el.querySelector('.tx-chip');
  const icon = tr.state === 'running' ? 'i-refresh'
    : tr.state === 'failed' ? 'i-alert'
    : tr.kind === 'push' ? 'i-up' : 'i-down';
  chip.className = 'tx-chip ' + (tr.state === 'failed' ? 'failed' : dirClass + (tr.state === 'running' ? ' running' : ''));
  chip.innerHTML = `<svg class="ic"><use href="#${icon}"/></svg>`;

  el.querySelector('.tx-name').textContent = tr.name;

  // Sub line: what happened and where, in that order. Failed rows show the
  // human-readable first line of the error instead.
  const sub = el.querySelector('.tx-sub');
  if (tr.state === 'failed' && tr.detail) {
    sub.textContent = tr.detail.split('\n')[0];
  } else if (tr.state === 'running' && tr.percent == null) {
    sub.textContent = tr.kind === 'push'
      ? t('panel.tx.pushingElapsed', { t: elapsedText(tr.startedAt) })
      : t('panel.tx.pullingElapsed', { t: elapsedText(tr.startedAt) });
  } else {
    sub.textContent = tr.detail || (tr.kind === 'push' ? t('panel.tx.toPhone') : t('panel.tx.fromPhone'));
  }

  el.querySelector('.tx-size').textContent = tr.size ? sizeText(tr.size) : '';

  const time = el.querySelector('.tx-time');
  time.textContent = relTime(tr.at);
  // An indeterminate run already shows its elapsed time in the sub line; the
  // time cell would only repeat it.
  if (tr.state === 'running' && tr.indeterminate) time.textContent = '';

  const state = el.querySelector('.tx-state');
  if (tr.state === 'running') {
    if (tr.percent != null) {
      // Bar leads, the number annotates it — the eye reads progress first.
      state.innerHTML = `<span class="tx-bar"><i></i></span>
        <span class="tx-pct">${Math.round(tr.percent)}%</span>`;
      state.querySelector('.tx-bar i').style.width =
        Math.max(4, Math.min(100, tr.percent)) + '%';
    } else {
      state.innerHTML = `<span class="tx-bar indet"><i></i></span>
        <span class="tx-pct">${esc(t('panel.tx.transferring'))}</span>`;
    }
  } else if (tr.state === 'done') {
    state.innerHTML = `<span class="badge done"><span class="b-dot"><svg class="ic"><use href="#i-check"/></svg></span>${esc(t('panel.tx.done'))}</span>`;
  } else if (tr.state === 'failed') {
    state.innerHTML = `<span class="badge failed"><span class="b-dot"><svg class="ic"><use href="#i-alert"/></svg></span>${esc(t('panel.tx.failed'))}</span>`;
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
    $('filesPath').innerHTML = '<bdi>' + esc(shortPath(d.path)) + '</bdi>';
    $('filesList').innerHTML = (d.entries && d.entries.length)
      ? d.entries.map(fileRow).join('')
      : `<div class="empty">
           <svg class="ic"><use href="#i-folder"/></svg>
           <span class="empty-title">${esc(t('panel.files.emptyDir'))}</span>
         </div>`;
  } catch (e) {
    $('filesList').innerHTML = `<div class="empty">${esc(e.message)}</div>`;
  }
}

function fileRow(en) {
  const icon = en.dir ? 'i-folder' : 'i-doc';
  const action = en.dir ? '' : `<span class="row-meta">${esc(t('panel.files.fetch'))}</span>`;
  return `<div class="row" data-path="${esc(en.path)}" data-dir="${en.dir ? 1 : 0}">
    <svg class="ic sm-ic" style="color:${en.dir ? 'var(--accent-text)' : 'var(--text-3)'}">
      <use href="#${icon}"/></svg>
    <span class="row-main">
      <span class="row-title">${esc(en.name)}</span>
      <span class="row-sub">${en.dir ? esc(t('common.folder')) : sizeText(en.size)}</span>
    </span>
    ${action}
    <button class="icon-btn" data-del="${esc(en.path)}" title="${esc(t('panel.files.delete'))}"
            aria-label="${esc(t('panel.files.deleteName', { name: en.name }))}">
      <svg class="ic sm-ic"><use href="#i-trash"/></svg>
    </button>
  </div>`;
}

/* ---------------- text ---------------- */

async function refreshClip() {
  try {
    const d = await api('/api/clip');
    $('clipView').textContent = d.text || t('common.empty');
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
    btn.textContent = t('common.copied');
    setTimeout(() => { btn.textContent = t('common.copy'); }, 1200);
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
    if (!confirm(t('panel.confirm.delete', { name: p.split('/').pop() }))) return;
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
    $('copyClip').textContent = t('common.copied');
    setTimeout(() => { $('copyClip').textContent = t('common.copy'); }, 1200);
  } catch (_) { /* clipboard may be blocked in the webview */ }
};

$('sendText').onclick = async () => {
  const text = $('textOut').value;
  if (!text) return;
  await post('/api/note', { text });
  $('textHint').textContent = t('panel.text.sent');
  setTimeout(() => {
    $('textHint').textContent = t('panel.text.hint');
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
overlay.textContent = t('panel.drop.tag');
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

/* ---------------- language ---------------- */

// applyLang() repaints every string declared with data-i18n; this repaints
// the ones this file renders from state. Rows are only patched when their
// signature changes, so a switch drops every signature first and lets the
// render rebuild them in the new language.
i18nOnChange(() => {
  if (St) {
    const empty = $('txCard').querySelector('.empty');
    if (empty) empty.remove();
    for (const rec of txRows.values()) rec.sig = '';
    render(St);
  } else {
    renderSub(null);
  }
  overlay.textContent = t('panel.drop.tag');
  $('textHint').textContent = t('panel.text.hint');
  if (view === 'text') refreshClip();
  if (view === 'files') loadFiles(curDir);
});

/* ---------------- boot ---------------- */

renderSub(null);
poll(true);
setInterval(poll, 2000);
setInterval(() => { if (view === 'text') refreshClip(); }, 2000);
