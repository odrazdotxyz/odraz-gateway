// odrive demo UI: plain JavaScript, no build step. Talks to the gateway's JSON API.
(() => {
  'use strict';

  // ---------- tiny helpers ----------
  const $ = (sel, el = document) => el.querySelector(sel);
  const h = (tag, attrs, ...kids) => {
    const el = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs || {})) {
      if (k === 'class') el.className = v;
      else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
      else if (v === true) el.setAttribute(k, '');
      else if (v !== false && v != null) el.setAttribute(k, v);
    }
    for (const kid of kids.flat()) el.append(kid instanceof Node ? kid : document.createTextNode(kid ?? ''));
    return el;
  };
  const fmtSize = (n) => {
    if (n < 1024) return n + ' B';
    const u = ['KB', 'MB', 'GB', 'TB'];
    let i = -1;
    do { n /= 1024; i++; } while (n >= 1024 && i < u.length - 1);
    return (n >= 100 ? n.toFixed(0) : n.toFixed(1)) + ' ' + u[i];
  };
  const fmtDate = (s) => (s ? new Date(s).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' }) : '');
  const join = (dir, name) => (dir === '/' ? '' : dir) + '/' + name;
  const parentOf = (p) => p.slice(0, p.lastIndexOf('/')) || '/';
  const baseOf = (p) => p.slice(p.lastIndexOf('/') + 1);
  const qs = (o) => new URLSearchParams(o).toString();


  // ---------- icons (Lucide-style line icons, inline so there is nothing to load) ----------
  const ICONS = {
    download: '<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><path d="m7 10 5 5 5-5"/><path d="M12 15V3"/>',
    upload: '<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><path d="m17 8-5-5-5 5"/><path d="M12 3v12"/>',
    pencil: '<path d="M17 3a2.85 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z"/>',
    folder: '<path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/>',
    folderPlus: '<path d="M12 10v6"/><path d="M9 13h6"/><path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/>',
    file: '<path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/>',
    fileText: '<path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/><path d="M10 9H8"/><path d="M16 13H8"/><path d="M16 17H8"/>',
    image: '<rect width="18" height="18" x="3" y="3" rx="2"/><circle cx="9" cy="9" r="2"/><path d="m21 15-3.09-3.09a2 2 0 0 0-2.82 0L6 21"/>',
    table: '<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M3 9h18"/><path d="M3 15h18"/><path d="M12 3v18"/>',
    braces: '<path d="M8 3H7a2 2 0 0 0-2 2v5a2 2 0 0 1-2 2 2 2 0 0 1 2 2v5c0 1.1.9 2 2 2h1"/><path d="M16 21h1a2 2 0 0 0 2-2v-5c0-1.1.9-2 2-2a2 2 0 0 1-2-2V5a2 2 0 0 0-2-2h-1"/>',
    archive: '<rect width="20" height="5" x="2" y="3" rx="1"/><path d="M4 8v11a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8"/><path d="M10 12h4"/>',
    move: '<path d="M3 5v14"/><path d="M21 12H7"/><path d="m15 18 6-6-6-6"/>',
    copy: '<rect width="14" height="14" x="8" y="8" rx="2" ry="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/>',
    trash: '<path d="M3 6h18"/><path d="M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6"/><path d="M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2"/><path d="M10 11v6"/><path d="M14 11v6"/>',
    refresh: '<path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16"/><path d="M8 16H3v5"/>',
    sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2"/><path d="M12 20v2"/><path d="m4.93 4.93 1.41 1.41"/><path d="m17.66 17.66 1.41 1.41"/><path d="M2 12h2"/><path d="M20 12h2"/><path d="m6.34 17.66-1.41 1.41"/><path d="m19.07 4.93-1.41 1.41"/>',
    moon: '<path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/>',
    logout: '<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><path d="m16 17 5-5-5-5"/><path d="M21 12H9"/>',
    user: '<path d="M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/>',
    users: '<path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M22 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/>',
    share: '<circle cx="18" cy="5" r="3"/><circle cx="6" cy="12" r="3"/><circle cx="18" cy="19" r="3"/><path d="m8.59 13.51 6.83 3.98"/><path d="m15.41 6.51-6.82 3.98"/>',
    chevron: '<path d="m9 18 6-6-6-6"/>',
  };
  const icon = (name, size = 16) => {
    const s = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    s.setAttribute('viewBox', '0 0 24 24'); s.setAttribute('width', size); s.setAttribute('height', size);
    s.setAttribute('fill', 'none'); s.setAttribute('stroke', 'currentColor'); s.setAttribute('stroke-width', '1.75');
    s.setAttribute('stroke-linecap', 'round'); s.setAttribute('stroke-linejoin', 'round'); s.setAttribute('aria-hidden', 'true');
    s.setAttribute('class', 'i'); s.innerHTML = ICONS[name]; return s;
  };

  // ---------- state ----------
  const state = { me: null, demo: false, oidc: true, drives: [], driveId: null, path: '/', entries: [], loading: false };
  const KIND = { personal: 'Personal', project: 'Team space', shared: 'Shared with you' };
  const drive = () => state.drives.find((d) => d.id === state.driveId);
  const readOnly = () => !!(drive() && drive().readOnly);

  // ---------- api ----------
  async function api(path, { method = 'GET', body, headers = {}, raw = false } = {}) {
    const res = await fetch(path, { method, body, credentials: 'same-origin', headers: { 'X-Requested-With': 'odraz-demo', ...headers } });
    if (res.status === 401 && state.me) { state.me = null; render(); }
    if (!res.ok) {
      let j = {};
      try { j = await res.json(); } catch { /* not JSON */ }
      throw Object.assign(new Error(j.error || res.statusText), { status: res.status, code: j.code });
    }
    return raw ? res : res.json();
  }
  const fileUrl = (p, extra = {}) => '/api/file?' + qs({ drive: state.driveId, path: p, ...extra });

  // ---------- feedback ----------
  function toast(msg, isErr) {
    const t = h('div', { class: 'toast' + (isErr ? ' err' : '') }, msg);
    $('#toasts').append(t);
    setTimeout(() => t.remove(), isErr ? 7000 : 3500);
  }
  const fail = (e) => toast(e.message || String(e), true);

  const dlg = $('#dlg');
  function openDialog(content, wide) {
    return new Promise((resolve) => {
      dlg.className = wide ? 'wide' : '';
      dlg.replaceChildren(...content(resolve));
      dlg.onclose = () => resolve(null);
      dlg.showModal();
    });
  }
  const closeDialog = () => { if (dlg.open) dlg.close(); };
  function askText(title, label, value, ok = 'OK') {
    return openDialog((done) => {
      const input = h('input', { type: 'text', value, 'aria-label': label });
      const form = h('form', { method: 'dialog', onsubmit: (e) => { e.preventDefault(); done(input.value.trim() || null); closeDialog(); } },
        h('h2', {}, title), h('label', {}, label, input),
        h('div', { class: 'row' }, h('button', { type: 'button', class: 'btn', onclick: () => closeDialog() }, 'Cancel'), h('button', { class: 'btn primary' }, ok)));
      setTimeout(() => { input.focus(); input.select(); }, 0);
      return [form];
    });
  }
  function confirmBox(title, message, ok, danger) {
    return openDialog((done) => [h('h2', {}, title), h('p', {}, message),
      h('div', { class: 'row' }, h('button', { class: 'btn', onclick: () => closeDialog() }, 'Cancel'),
        h('button', { class: 'btn ' + (danger ? 'danger' : 'primary'), onclick: () => { done(true); closeDialog(); } }, ok))]).then((v) => !!v);
  }

  // ---------- theme (system by default, the sun/moon button overrides and is remembered) ----------
  const sysDark = matchMedia('(prefers-color-scheme: dark)');
  let pref = localStorage.getItem('theme'); // 'light' | 'dark' | null = follow the system
  const isDark = () => (pref ? pref === 'dark' : sysDark.matches);
  const applyTheme = () => { document.documentElement.dataset.theme = isDark() ? 'dark' : 'light'; };
  const toggleTheme = () => { pref = isDark() ? 'light' : 'dark'; localStorage.setItem('theme', pref); applyTheme(); render(); };
  const themeBtn = () => h('button', { class: 'iconbtn theme', title: 'Switch between light and dark', 'aria-label': 'Switch between light and dark theme', onclick: toggleTheme }, icon(isDark() ? 'sun' : 'moon', 18));
  const logo = (cls) => h('img', { class: cls, alt: 'odrive', src: '/assets/odrive-lockup-' + (isDark() ? 'light' : 'dark') + '-text.svg' });
  sysDark.addEventListener('change', () => { applyTheme(); render(); });
  applyTheme();

  // ---------- views ----------
  function loginView() {
    const err = h('div', { class: 'err', role: 'alert' });
    const demo = state.demo ? h('button', { class: 'btn', onclick: async () => {
      err.textContent = '';
      try { await api('/auth/demo', { method: 'POST' }); await boot(); }
      catch (x) { err.textContent = x.message; }
    } }, 'Try the demo') : '';
    if (demo && !state.oidc) demo.classList.add('primary');
    return h('div', { class: 'login' }, h('div', { class: 'corner' }, themeBtn()), h('div', { class: 'card' },
      logo('logo'), h('h1', {}, 'Sign in'),
      h('p', {}, 'A demo of the Odraz Go SDK: this page browses files stored in OpenCloud through a small gateway.'),
      state.oidc ? h('a', { class: 'btn primary', href: '/auth/login' }, 'Sign in with Pocket ID') : '',
      state.oidc && demo ? h('div', { class: 'or' }, 'or') : '', demo, err,
      state.oidc ? h('p', { class: 'fine' }, 'Pocket ID uses passkeys. First time? Run ', h('code', {}, 'make login-link'), ' and open the link once to register one.') : ''));
  }

  function crumbs() {
    const parts = state.path.split('/').filter(Boolean);
    const el = h('nav', { class: 'crumbs', 'aria-label': 'Folder path' });
    const add = (label, p, last) => el.append(h('button', { onclick: () => go(p), 'aria-current': last ? 'page' : null }, label));
    add(drive() ? drive().name : 'Drive', '/', parts.length === 0);
    let acc = '';
    parts.forEach((seg, i) => { acc += '/' + seg; el.append(h('span', { class: 'sep' }, icon('chevron', 14))); add(seg, acc, i === parts.length - 1); });
    return el;
  }

  function iconFor(e) {
    if (e.isDir) return icon('folder', 18);
    const m = e.mime || '';
    if (m.startsWith('image/')) return icon('image', 18);
    if (m === 'text/csv') return icon('table', 18);
    if (m === 'application/json') return icon('braces', 18);
    if (m.startsWith('text/')) return icon('fileText', 18);
    if (/\.(bin|zip|gz|tar|7z)$/i.test(e.name)) return icon('archive', 18);
    return icon('file', 18);
  }

  function row(e) {
    const ro = readOnly();
    const btn = (label, icon, fn, dis) => h('button', { class: 'iconbtn', title: label, 'aria-label': label + ' ' + e.name, onclick: fn, disabled: dis }, icon);
    return h('tr', {},
      h('td', {}, h('button', { class: 'name', onclick: () => (e.isDir ? go(e.path) : preview(e)) }, h('span', { class: 'ic', 'aria-hidden': 'true' }, iconFor(e)), h('span', { class: 't' }, e.name))),
      h('td', { class: 'muted num' }, e.isDir ? '' : fmtSize(e.size)),
      h('td', { class: 'muted mod' }, fmtDate(e.modTime)),
      h('td', { class: 'actions' },
        e.isDir ? '' : btn('Download', icon('download'), () => { location.href = fileUrl(e.path); }),
        btn('Rename', icon('pencil'), () => rename(e), ro), btn('Move', icon('move'), () => moveOrCopy(e, 'move'), ro),
        btn('Copy', icon('copy'), () => moveOrCopy(e, 'copy'), ro), btn('Delete', icon('trash'), () => remove(e), ro)));
  }

  function appView() {
    const d = drive();
    const side = h('aside', {}, h('h2', {}, 'Spaces'), ...state.drives.map((dr) => {
      const pct = dr.total ? Math.min(100, (dr.used / dr.total) * 100) : 0;
      return h('button', { class: 'drive', 'aria-current': dr.id === state.driveId ? 'true' : 'false', onclick: () => { state.driveId = dr.id; go('/'); } },
        h('span', { class: 'dn' }, icon(dr.kind === 'personal' ? 'user' : dr.kind === 'project' ? 'users' : 'share'), dr.name), h('small', {}, (KIND[dr.kind] || dr.kind) + (dr.readOnly ? ' · read-only' : '') + (dr.used != null ? ' · ' + fmtSize(dr.used) + (dr.total ? ' of ' + fmtSize(dr.total) : ' used') : '')),
        dr.total ? h('div', { class: 'bar' }, h('i', { style: 'width:' + pct + '%' })) : '');
    }));

    const fileInput = h('input', { type: 'file', multiple: true, hidden: true, onchange: () => { uploadFiles([...fileInput.files]); fileInput.value = ''; } });
    const toolbar = h('div', { class: 'toolbar' },
      h('button', { class: 'btn primary', disabled: readOnly(), onclick: () => fileInput.click() }, icon('upload'), 'Upload'), fileInput,
      h('button', { class: 'btn', disabled: readOnly(), onclick: newFolder }, icon('folderPlus'), 'New folder'),
      h('button', { class: 'btn', onclick: () => load() }, icon('refresh'), 'Refresh'),
      readOnly() ? h('span', { class: 'ro' }, 'This drive is read-only') : '');

    const body = state.loading ? h('div', { class: 'empty' }, 'Loading…')
      : state.entries.length === 0 ? h('div', { class: 'empty' }, 'This folder is empty.' + (readOnly() ? '' : ' Drop files here to upload.'))
      : h('table', {}, h('thead', {}, h('tr', {}, h('th', {}, 'Name'), h('th', { class: 'num' }, 'Size'), h('th', { class: 'mod' }, 'Modified'), h('th', {}, ''))),
          h('tbody', {}, ...state.entries.map(row)));

    const main = h('main', {}, crumbs(), toolbar, body);
    wireDrop(main);

    return h('div', { class: 'shell' },
      h('header', { class: 'topbar' }, logo('logo'), h('div', { class: 'spacer' }),
        h('span', { class: 'chip' }, 'Signed in as ', h('b', {}, state.me.user || 'you'), ' · ', state.me.method === 'oidc' ? 'Pocket ID' : 'app token'),
        themeBtn(), h('button', { class: 'btn small', onclick: signOut }, 'Sign out')),
      h('div', { class: 'layout' }, side, main));
  }

  function render() {
    const app = $('#app');
    app.replaceChildren(state.me ? appView() : loginView());
    document.title = (state.me && drive() ? drive().name + ' · ' : '') + 'odrive demo · Odraz SDK';
  }

  // ---------- navigation ----------
  const hashOf = () => '#' + qs({ drive: state.driveId, path: state.path });
  function go(path) { state.path = path; history.pushState(null, '', hashOf()); load(); }
  window.addEventListener('popstate', () => { readHash(); load(); });
  function readHash() {
    const p = new URLSearchParams(location.hash.slice(1));
    if (p.get('drive') && state.drives.some((d) => d.id === p.get('drive'))) state.driveId = p.get('drive');
    state.path = p.get('path') || '/';
  }

  async function load() {
    state.loading = true; render();
    try {
      const res = await api('/api/list?' + qs({ drive: state.driveId, path: state.path }));
      state.path = res.path; state.entries = res.entries;
    } catch (e) {
      state.entries = [];
      if (state.path !== '/') { state.path = '/'; history.replaceState(null, '', hashOf()); fail(e); state.loading = false; return load(); }
      if (e.status !== 401) fail(e);
    }
    state.loading = false; render();
  }

  // ---------- actions ----------
  async function preview(e) {
    const type = (e.mime || '').split(';')[0];
    const isImg = type.startsWith('image/');
    const isText = type.startsWith('text/') || type === 'application/json' || /\.(md|csv|json|txt|js|css|html)$/i.test(e.name);
    await openDialog((done) => {
      const box = h('div', { class: 'preview' }, h('div', { class: 'empty' }, isImg || isText ? 'Loading…' : 'No preview for this type of file.'));
      if (isImg) { const img = h('img', { alt: e.name, src: fileUrl(e.path, { inline: 1 }) }); img.onload = () => box.replaceChildren(img); img.onerror = () => box.replaceChildren(h('div', { class: 'empty' }, 'Could not load the image.')); }
      else if (isText) {
        api(fileUrl(e.path, { inline: 1 }), { raw: true, headers: { Range: 'bytes=0-131071' } }).then((r) => r.text()).then((t) => {
          box.replaceChildren(h('pre', {}, t + (e.size > 131072 ? '\n\n… (showing the first 128 KB)' : '')));
        }).catch((x) => box.replaceChildren(h('div', { class: 'empty' }, x.message)));
      }
      return [h('h2', {}, e.name), box, h('div', { class: 'meta' }, fmtSize(e.size) + ' · ' + (type || 'unknown type') + ' · ' + fmtDate(e.modTime)),
        h('div', { class: 'row' }, h('a', { class: 'btn', href: fileUrl(e.path) }, icon('download'), 'Download'), h('button', { class: 'btn primary', onclick: () => closeDialog() }, 'Close'))];
    }, true);
  }

  async function newFolder() {
    const name = await askText('New folder', 'Folder name', '', 'Create');
    if (!name) return;
    try { await api('/api/mkdir?' + qs({ drive: state.driveId, path: join(state.path, name) }), { method: 'POST' }); toast('Created ' + name); load(); } catch (e) { fail(e); }
  }

  async function rename(e) {
    const name = await askText('Rename', 'New name', e.name, 'Rename');
    if (!name || name === e.name) return;
    try { await api('/api/move?' + qs({ drive: state.driveId, from: e.path, to: join(parentOf(e.path), name) }), { method: 'POST' }); toast('Renamed to ' + name); load(); }
    catch (x) { fail(x.code === 'exists' ? new Error('“' + name + '” already exists here.') : x); }
  }

  async function moveOrCopy(e, kind) {
    const verb = kind === 'move' ? 'Move' : 'Copy';
    const dest = await askText(verb + ' “' + e.name + '”', 'Destination path (a new full path, e.g. /Documents/' + e.name + ')', e.path, verb);
    if (!dest || dest === e.path) return;
    try {
      await api('/api/' + kind + '?' + qs({ drive: state.driveId, from: e.path, to: dest.startsWith('/') ? dest : '/' + dest }), { method: 'POST' });
      toast(verb + (kind === 'move' ? 'd' : 'ied') + ' to ' + dest); load();
    } catch (x) { fail(x.code === 'exists' ? new Error('Something already exists at ' + dest + '.') : x); }
  }

  async function remove(e) {
    if (!(await confirmBox('Delete ' + (e.isDir ? 'folder' : 'file') + '?', '“' + e.name + '”' + (e.isDir ? ' and everything in it' : '') + ' will be deleted.', 'Delete', true))) return;
    try { await api('/api/entry?' + qs({ drive: state.driveId, path: e.path, recursive: 1 }), { method: 'DELETE' }); toast('Deleted ' + e.name); load(); } catch (x) { fail(x); }
  }

  // ---------- upload ----------
  const panel = $('#uploads');
  function putFile(file, path, overwrite, onProgress) {
    return new Promise((resolve, reject) => {
      const x = new XMLHttpRequest();
      x.open('PUT', '/api/file?' + qs({ drive: state.driveId, path, overwrite: overwrite ? 1 : 0 }));
      x.setRequestHeader('X-Requested-With', 'odraz-demo');
      x.upload.onprogress = (ev) => ev.lengthComputable && onProgress(ev.loaded / ev.total);
      x.onload = () => {
        if (x.status < 300) return resolve();
        let j = {}; try { j = JSON.parse(x.responseText); } catch { /* not JSON */ }
        reject(Object.assign(new Error(j.error || 'upload failed (HTTP ' + x.status + ')'), { code: j.code }));
      };
      x.onerror = () => reject(new Error('network error'));
      x.send(file);
    });
  }

  async function uploadFiles(list) {
    if (readOnly() || !list.length) return;
    const dir = state.path, drv = state.driveId;
    let ok = 0;
    for (const file of list) {
      const path = join(dir, file.name);
      const bar = h('i', { style: 'width:0%' });
      const pct = h('span', {}, '0%');
      panel.hidden = false;
      panel.append(h('div', { class: 'u' }, h('div', {}, h('span', {}, file.name), pct), h('div', { class: 'bar' }, bar)));
      const prog = (f) => { bar.style.width = f * 100 + '%'; pct.textContent = Math.round(f * 100) + '%'; };
      try {
        try { await putFile(file, path, false, prog); }
        catch (e) {
          if (e.code !== 'exists') throw e;
          if (!(await confirmBox('Replace file?', '“' + file.name + '” already exists in this folder.', 'Replace', true))) continue;
          await putFile(file, path, true, prog);
        }
        ok++;
      } catch (e) { fail(new Error(file.name + ': ' + e.message)); }
    }
    setTimeout(() => { panel.replaceChildren(); panel.hidden = true; }, 1200);
    if (ok) toast('Uploaded ' + ok + (ok === 1 ? ' file' : ' files'));
    if (state.driveId === drv && state.path === dir) load();
  }

  function wireDrop(main) {
    let depth = 0, veil = null;
    const hide = () => { depth = 0; veil?.remove(); veil = null; };
    main.addEventListener('dragenter', (e) => { if (readOnly() || !e.dataTransfer?.types.includes('Files')) return; e.preventDefault(); depth++; if (!veil) { veil = h('div', { class: 'drop' }, 'Drop files to upload to ' + state.path); main.append(veil); } });
    main.addEventListener('dragover', (e) => { if (e.dataTransfer?.types.includes('Files')) e.preventDefault(); });
    main.addEventListener('dragleave', () => { if (--depth <= 0) hide(); });
    main.addEventListener('drop', (e) => { if (!e.dataTransfer?.files.length) return; e.preventDefault(); hide(); uploadFiles([...e.dataTransfer.files]); });
  }

  async function signOut() {
    try { await api('/auth/logout', { method: 'POST' }); } catch { /* already gone */ }
    state.me = null; state.drives = []; history.replaceState(null, '', '/'); render();
  }

  // ---------- start ----------
  async function boot() {
    try {
      const me = await api('/api/me');
      state.demo = !!me.demo; state.oidc = me.oidc !== false;
      if (!me.authenticated) { state.me = null; return render(); }
      state.me = me;
      const order = { personal: 0, project: 1, shared: 2 };
      state.drives = (await api('/api/drives')).sort((a, b) => (order[a.kind] ?? 3) - (order[b.kind] ?? 3) || a.name.localeCompare(b.name));
      state.driveId = (state.drives.find((d) => d.kind === 'personal') || state.drives[0] || {}).id;
      readHash();
      history.replaceState(null, '', hashOf());
      await load();
    } catch (e) { state.me = null; render(); if (e.status !== 401) fail(e); }
  }
  boot();
})();
