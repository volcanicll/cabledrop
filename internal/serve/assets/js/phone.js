/* CableDrop — phone page.
 *
 * Reached at http://localhost:<port> on the device, tunnelled over the USB
 * cable by `adb reverse`. `localhost` counts as a secure context, which is
 * what makes navigator.clipboard available here — the reason the clipboard is
 * handed over through this page instead of written by adb, which Android 10
 * and later forbid. Uses the shared helpers in api.js and i18n.js.
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
  if (!text) { toast(t('phone.toast.nothingToCopy')); return; }
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
  toast(ok ? t('phone.toast.copied') : t('phone.toast.copyFailed'));
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
      ? t('phone.conn.connectedDevice', { device: st.device })
      : t('phone.conn.ready');

    // Clipboard
    const clipText = clip.text || '';
    if (clipText !== lastClip) {
      lastClip = clipText;
      $('clipText').textContent = clipText || t('common.empty');
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
  if (!on) $('sub').textContent = t('phone.conn.offline');
}

/* ---------------- shared folder, with subfolders ---------------- */

let curSub = '';

function crumbFor(sub) {
  if (!sub) return t('phone.files.rootCrumb');
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
           <span class="empty-title">${esc(t('phone.files.emptyHere'))}</span>
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
        <span class="p-item-sub">${esc(t('common.folder'))}</span>
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

// Whatever the picker holds, the pick line and the button say the same thing.
// Shared by the picker, the end of an upload and the language switch, which
// all three leave `chosen` in a different state.
function syncUploadUI() {
  $('filePickText').textContent = chosen.length
    ? (chosen.length === 1 ? chosen[0].name : t('phone.upload.selected', { n: chosen.length }))
    : t('phone.upload.pick');
  $('upload').textContent = chosen.length
    ? t('phone.upload.sendCount', { n: chosen.length })
    : t('phone.upload.send');
}

$('fileInput').onchange = (e) => {
  chosen = Array.from(e.target.files || []);
  syncUploadUI();
  $('upload').disabled = chosen.length === 0;
};

$('upload').onclick = () => {
  if (!chosen.length) return;

  const fd = new FormData();
  chosen.forEach((f) => fd.append('files', f, f.name));

  const xhr = new XMLHttpRequest();
  xhr.open('POST', '/upload');
  $('upProgress').hidden = false;
  $('upload').disabled = true;
  // Everything the user picked is gone from the form by the time the UI
  // resets, whether the upload made it or not — a stale "3 files chosen"
  // would point at files the server never received.
  const reset = () => {
    chosen = [];
    $('fileInput').value = '';
    $('upProgress').hidden = true;
    $('upBar').style.width = '0%';
    syncUploadUI();
    $('upload').disabled = false;
  };

  xhr.upload.onprogress = (ev) => {
    if (!ev.lengthComputable) return;
    const pct = Math.round((ev.loaded / ev.total) * 100);
    $('upBar').style.width = pct + '%';
    $('upload').textContent = t('phone.upload.progress', { pct });
  };

  xhr.onload = () => {
    reset();
    if (xhr.status === 200) {
      toast(t('phone.toast.uploaded'));
      loadFiles(curSub);
    } else {
      // The server answers failures with a JSON error body: surface it, never
      // a bare "upload failed" that hides whether it was a bad request or a
      // full disk.
      let msg = t('phone.toast.uploadFailed', { code: xhr.status });
      try { msg = JSON.parse(xhr.responseText).error || msg; } catch (_) { }
      toast(msg);
    }
  };

  // A network-level failure never gets an HTTP status. Over adb reverse that
  // means either the cable/tunnel dropped, or the phone failed to read the
  // chosen file's contents — naming both beats a bare "failed".
  const streamDied = () => {
    const n = chosen.length;
    reset();
    console.error('upload aborted: readyState=%d status=%d files=%d',
      xhr.readyState, xhr.status, n);
    toast(t('phone.toast.streamDied'));
  };
  xhr.upload.onerror = streamDied;
  xhr.onerror = streamDied;

  xhr.send(fd);
};

/* ---------------- copy / share the clipboard image ---------------- */

// Writing an image needs the ClipboardItem form of the API, which is both
// newer and pickier than writeText: the WebView some ROMs ship (this project's
// reference phone serves pages from Chrome 75) has neither. Where it is
// missing the button removes itself rather than sit there collecting
// apologies — every tap it could ever take ends in the same failure toast.
// The guard inside the handler stays for engines the probe judges wrong.
if (!navigator.clipboard || !window.ClipboardItem) {
  $('copyImage').style.display = 'none';
}

$('copyImage').onclick = async () => {
  if (!imageBlob) { toast(t('phone.image.nothingToCopy')); return; }
  if (!navigator.clipboard || !window.ClipboardItem) {
    toast(t('phone.image.copyUnsupported'));
    return;
  }
  try {
    await navigator.clipboard.write([new ClipboardItem({ 'image/png': imageBlob })]);
    toast(t('phone.image.copied'));
  } catch (e) {
    const why = (e && e.name === 'NotAllowedError') ? t('phone.image.noPermission')
      : (e && e.message) || t('common.unknownReason');
    toast(t('phone.image.copyFailed', { why }));
  }
};

// Receiving apps show this name while the share travels, same shape as the
// downloads the server names for the save button.
function imageFileName() {
  const d = new Date(), p = (n) => String(n).padStart(2, '0');
  return 'cabledrop-' + d.getFullYear() + p(d.getMonth() + 1) + p(d.getDate()) +
    '-' + p(d.getHours()) + p(d.getMinutes()) + p(d.getSeconds()) + '.png';
}

// Sharing a picture has three rungs, walked in order of capability:
//
//   1. Web Share with files — the clean path, no native code — but it is
//      Level 2 of the API, Chrome 76+, so the APK's pinned Chrome 75 WebView
//      fails the canShare probe.
//   2. The APK shell's native bridge: the shell fetches the image itself and
//      opens the system share sheet, WeChat and QQ included.
//   3. A named escape hatch. Whatever happens, the tap ends in something
//      readable — a silent no-op reads as "the bridge is broken".
$('shareImage').onclick = async () => {
  if (!imageBlob) { toast(t('phone.image.nothingToShare')); return; }
  const file = new File([imageBlob], imageFileName(), { type: 'image/png' });
  if (navigator.share && navigator.canShare && navigator.canShare({ files: [file] })) {
    try {
      await navigator.share({ files: [file], title: t('phone.image.shareTitle') });
    } catch (e) {
      // The user dismissing the sheet is not a failure worth an alert.
      if (e && e.name !== 'AbortError') {
        toast(t('phone.image.shareFailed', { why: (e && e.message) || t('common.unknownReason') }));
      }
    }
    return;
  }
  if (window.CableDropNative && CableDropNative.shareClipImage) {
    toast(t('phone.image.preparing'));
    CableDropNative.shareClipImage(); // the sheet, or an error toast, comes from native
    return;
  }
  toast(t('phone.image.shareUnsupported'));
};

// The download fallback: the endpoint marks the body as an attachment, which
// the WebView hands to the system DownloadManager and a browser downloads.
$('saveImage').onclick = () => {
  if (!imageBlob) { toast(t('phone.image.nothingToSave')); return; }
  toast(t('phone.image.saving'));
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
    toast(t('phone.toast.sentToMac'));
    $('toMac').value = '';
    lastClip = ''; // force the next poll to re-read
  } catch (err) {
    toast(t('phone.toast.sendFailed', { why: err.message }));
  }
};

$('reload').onclick = () => { loadFiles(curSub); refresh(); };

$('retryConn').onclick = () => { refresh(); loadFiles(curSub); };

/* ---------------- language ---------------- */

// applyLang() repaints every string declared with data-i18n; this repaints
// the ones this file renders from state, right now instead of at the next
// poll — the sub line through refresh(), the list rows through loadFiles().
i18nOnChange(() => {
  if (!lastClip) $('clipText').textContent = t('common.empty');
  syncUploadUI();
  loadFiles(curSub);
  refresh();
});

/* ---------------- boot ---------------- */

refresh();
loadFiles('');
setInterval(refresh, 2500);
