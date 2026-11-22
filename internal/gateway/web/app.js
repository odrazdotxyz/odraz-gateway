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
    film: '<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M7 3v18"/><path d="M3 7.5h4"/><path d="M3 12h18"/><path d="M3 16.5h4"/><path d="M17 3v18"/><path d="M17 7.5h4"/><path d="M17 16.5h4"/>',
    play: '<polygon points="6 3 20 12 6 21 6 3"/>',
    eyeOff: '<path d="M9.88 9.88a3 3 0 1 0 4.24 4.24"/><path d="M10.73 5.08A10.43 10.43 0 0 1 12 5c7 0 10 7 10 7a13.16 13.16 0 0 1-1.67 2.68"/><path d="M6.61 6.61A13.526 13.526 0 0 0 2 12s3 7 10 7a9.74 9.74 0 0 0 5.39-1.61"/><line x1="2" x2="22" y1="2" y2="22"/>',
    settings: '<path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z"/><circle cx="12" cy="12" r="3"/>',
    plus: '<path d="M5 12h14"/><path d="M12 5v14"/>',
    chevronDown: '<path d="m6 9 6 6 6-6"/>',
    plug: '<path d="M12 22v-5"/><path d="M9 8V2"/><path d="M15 8V2"/><path d="M18 8v5a4 4 0 0 1-4 4h-4a4 4 0 0 1-4-4V8Z"/>',
    unplug: '<path d="m19 5 3-3"/><path d="m2 22 3-3"/><path d="M6.3 20.3a2.4 2.4 0 0 0 3.4 0L12 18l-6-6-2.3 2.3a2.4 2.4 0 0 0 0 3.4Z"/><path d="M7.5 13.5 10 11"/><path d="M10.5 16.5 13 14"/><path d="m12 6 6 6 2.3-2.3a2.4 2.4 0 0 0 0-3.4l-2.6-2.6a2.4 2.4 0 0 0-3.4 0Z"/>',
  };
  const icon = (name, size = 16) => {
    const s = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    s.setAttribute('viewBox', '0 0 24 24'); s.setAttribute('width', size); s.setAttribute('height', size);
    s.setAttribute('fill', 'none'); s.setAttribute('stroke', 'currentColor'); s.setAttribute('stroke-width', '1.75');
    s.setAttribute('stroke-linecap', 'round'); s.setAttribute('stroke-linejoin', 'round'); s.setAttribute('aria-hidden', 'true');
    s.setAttribute('class', 'i'); s.innerHTML = ICONS[name]; return s;
  };

  // ---------- vendor badges (small marks drawn here, not the vendors' official logos) ----------
  const VENDORS = {
    opencloud: { name: 'OpenCloud', bg: '#1d3b53', mark: '<circle cx="12" cy="12" r="5.2" fill="none" stroke="#fff" stroke-width="2.6"/><path d="M12 3.5V7" stroke="#1d3b53" stroke-width="3"/><path d="M12 3.5V7" stroke="#6fd0ff" stroke-width="1.6" stroke-linecap="round"/>' },
    nextcloud: { name: 'Nextcloud', bg: '#0082c9', mark: '<circle cx="5.2" cy="12" r="2.7" fill="none" stroke="#fff" stroke-width="1.7"/><circle cx="12" cy="12" r="4.1" fill="none" stroke="#fff" stroke-width="1.9"/><circle cx="18.8" cy="12" r="2.7" fill="none" stroke="#fff" stroke-width="1.7"/>' },
  };
  const vendorName = (v) => (VENDORS[v] ? VENDORS[v].name : v);
  function vbadge(vendor, size = 14) {
    const v = VENDORS[vendor] || { name: vendor, bg: '#666', mark: '' };
    const s = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    s.setAttribute('viewBox', '0 0 24 24'); s.setAttribute('width', size); s.setAttribute('height', size);
    s.setAttribute('class', 'vb'); s.setAttribute('role', 'img'); s.setAttribute('aria-label', v.name);
    s.innerHTML = '<title>' + v.name + '</title><circle cx="12" cy="12" r="12" fill="' + v.bg + '"/>' + v.mark;
    return s;
  }

  // ---------- state ----------
  // Drives from every connected backend are grouped into three categories. The page shows each category
  // merged: Personal (the files of all personal drives together), Teams (every team space or folder) and
  // Shared. `loc` is null at a category's top level, or {acct, drive, path} once inside a folder.
  const CATS = [
    { id: 'personal', label: 'Personal', icon: 'user', kinds: ['personal'] },
    { id: 'photos', label: 'Photos', icon: 'image', kinds: [] }, // built from the folders chosen per drive (manifest)
    { id: 'teams', label: 'Teams', icon: 'users', kinds: ['project'] },
    { id: 'shared', label: 'Shared', icon: 'share', kinds: ['shared', 'other'] },
  ];
  const state = { me: null, demo: false, oidc: true, backends: [], drives: [], driveErrors: {}, cat: 'personal', loc: null, entries: [], loading: false, lastTarget: null,
    mappings: [], problems: [], photos: { items: [], truncated: false }, showHidden: false, hiddenCount: 0,
    menuOpen: false, // the "New" menu
    acctMenu: null, // id of the server whose top-bar menu is open
    avatars: {}, // per server: 'ok' | 'none' (so a missing avatar is asked for once, not on every redraw)
    settings: false, // the Settings page is showing
    sel: new Map(), anchor: null, // the selected files and folders, by key; the last one clicked (for shift-click ranges)
    photoView: localStorage.getItem('photoView') === 'folders' ? 'folders' : 'gallery' };
  const catById = (id) => CATS.find((c) => c.id === id) || CATS[0];
  const catOfKind = (k) => (CATS.find((c) => c.kinds.includes(k)) || CATS[2]).id;
  const drivesOf = (cat) => state.drives.filter((d) => catById(cat).kinds.includes(d.kind));
  const driveBy = (acct, id) => state.drives.find((d) => d.acct === acct && d.id === id);
  const curDrive = () => (state.loc ? driveBy(state.loc.acct, state.loc.drive) : null);
  const curPath = () => (state.loc ? state.loc.path : '/');
  const isRO = (e) => { const d = driveBy(e.acct, e.drive); return !!(d && d.readOnly); };
  // Photos folders the user mapped (from each drive's manifest) and what the plain file views leave out:
  // dot-folders always (/.odraz holds the manifest and thumbnails), and a mapped Photos folder when the
  // mapping says hideInFiles. "Show hidden" in the toolbar reveals them, so nothing is ever unreachable.
  const mappingOf = (acct, drive) => state.mappings.find((m) => m.acct === acct && m.drive === drive);
  const isHidden = (e) => {
    if (e.name.startsWith('.')) return true;
    if (state.cat === 'photos') return false; // the Photos views show the chosen folders themselves
    const m = mappingOf(e.acct, e.drive);
    return !!(m && m.hideInFiles && e.isDir && e.path === m.folder);
  };
  function applyHidden(entries) {
    state.hiddenCount = entries.filter(isHidden).length;
    return state.showHidden ? entries : entries.filter((e) => !isHidden(e));
  }
  const IMG_RE = /\.(png|jpe?g|gif|webp|avif|heic|heif|bmp|tiff?|svg)$/i, VID_RE = /\.(mp4|m4v|mov|webm|mkv|avi|3gp|ogv)$/i;
  const kindOf = (e) => {
    if (e.isDir) return 'dir';
    const m = (e.mime || '').split(';')[0];
    if (m.startsWith('image/') || IMG_RE.test(e.name)) return 'image';
    if (m.startsWith('video/') || VID_RE.test(e.name)) return 'video';
    return 'other';
  };
  const baseName = (p) => p.slice(p.lastIndexOf('/') + 1) || p;
  const writableDrives = () => state.drives.filter((d) => !d.readOnly && d.kind !== 'shared');

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
  // An entry knows its backend: e.acct / e.drive. Everything the page sends back names them.
  const at = (e, extra = {}) => ({ acct: e.acct, drive: e.drive, ...extra });
  const fileUrl = (e, extra = {}) => '/api/file?' + qs({ ...at(e), path: e.path, ...extra });

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
      // A dialog closed a moment ago ("Add a folder" closes one and opens the next) delivers its close event
      // late, while this one is already open: ignore it.
      dlg.onclose = () => { if (!dlg.open) resolve(null); };
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

  // ---------- selection ----------
  const selKey = (e) => e.acct + '|' + e.drive + '|' + e.path;
  const isSel = (e) => state.sel.has(selKey(e));
  const clearSel = () => { state.sel.clear(); state.anchor = null; };
  // What is on screen and can be selected, in order (shift-click selects the range between two of them).
  const photoItems = () => (!state.loc && state.photoView === 'gallery' ? (state.photos.items || [])
    : state.entries.filter((e) => e.kind === 'dir' || kindOf(e) !== 'other')); // Photos is only pictures and videos (and folders to browse)
  const shownItems = () => (state.cat === 'photos' ? photoItems() : state.entries.filter((e) => !e.isSpace));
  function toggleSel(e, ev) {
    const list = shownItems(), k = selKey(e);
    if (ev && ev.shiftKey && state.anchor) {
      const a = list.findIndex((x) => selKey(x) === state.anchor), b = list.findIndex((x) => selKey(x) === k);
      if (a >= 0 && b >= 0) { for (const x of list.slice(Math.min(a, b), Math.max(a, b) + 1)) state.sel.set(selKey(x), x); render(); return; }
    }
    if (state.sel.has(k)) state.sel.delete(k); else state.sel.set(k, e);
    state.anchor = k; render();
  }
  function selectAll() { for (const x of shownItems()) state.sel.set(selKey(x), x); render(); }
  const selectedList = () => [...state.sel.values()];
  document.addEventListener('keydown', (ev) => {
    if (ev.key !== 'Escape' || dlg.open) return;
    if (state.acctMenu) { state.acctMenu = null; render(); } else if (state.menuOpen) { state.menuOpen = false; render(); } else if (state.sel.size) { clearSel(); render(); }
  });

  // ---------- views ----------
  function loginView() {
    const err = h('div', { class: 'err', role: 'alert' });
    const hasOidc = state.backends.some((b) => b.oidc);
    const demo = state.demo ? h('button', { class: 'btn' + (hasOidc ? '' : ' primary'), onclick: async () => {
      err.textContent = '';
      try { await api('/auth/demo', { method: 'POST' }); await boot(); }
      catch (x) { err.textContent = x.message; }
    } }, 'Try the demo') : '';
    const names = state.backends.map((b) => vendorName(b.vendor)).join(' and ');
    return h('div', { class: 'login' }, h('div', { class: 'corner' }, themeBtn()), h('div', { class: 'card' },
      logo('logo'), h('h1', {}, 'Sign in'),
      h('p', {}, 'A demo of the Odraz Go SDK: one page for the files you keep on ' + (names || 'your servers') + ', through a small gateway.'),
      hasOidc ? h('a', { class: 'btn primary', href: '/auth/login' }, 'Sign in with Pocket ID') : '',
      hasOidc && demo ? h('div', { class: 'or' }, 'or') : '', demo, err,
      hasOidc ? h('p', { class: 'fine' }, 'Pocket ID uses passkeys. First time? Run ', h('code', {}, 'make login-link'), ' and open the link once to register one.') : ''));
  }

  const catSummary = (c) => {
    if (c.id === 'photos') return state.mappings.length ? state.mappings.length + (state.mappings.length === 1 ? ' folder' : ' folders') : 'Choose a folder';
    const ds = drivesOf(c.id);
    if (!ds.length) return 'Nothing connected';
    if (c.id === 'personal') { const u = ds.reduce((n, d) => n + (d.used || 0), 0); return u ? fmtSize(u) + ' used' : 'Your files'; }
    if (c.id === 'teams') return ds.length + (ds.length === 1 ? ' space' : ' spaces');
    return 'Shared with you';
  };

  function crumbs() {
    const el = h('nav', { class: 'crumbs', 'aria-label': 'Folder path' });
    const cat = catById(state.cat);
    const items = [{ label: cat.label, fn: () => openCat(cat.id) }];
    if (state.loc) {
      const d = curDrive();
      const base = { acct: state.loc.acct, drive: state.loc.drive };
      if (cat.id === 'teams' && d) items.push({ label: d.name, fn: () => go({ ...base, path: '/' }) });
      // Inside Photos the chosen folder is the starting point: its path is not shown, only what is below it.
      const m = cat.id === 'photos' ? mappingOf(state.loc.acct, state.loc.drive) : null;
      const below = m && state.loc.path.startsWith(m.folder + '/');
      let acc = below ? m.folder : '';
      for (const seg of (below ? state.loc.path.slice(m.folder.length) : state.loc.path).split('/').filter(Boolean)) { acc += '/' + seg; const p = acc; items.push({ label: seg, fn: () => go({ ...base, path: p }) }); }
    }
    items.forEach((it, i) => {
      if (i) el.append(h('span', { class: 'sep' }, icon('chevron', 14)));
      el.append(h('button', { onclick: it.fn, 'aria-current': i === items.length - 1 ? 'page' : null }, it.label));
    });
    const d = curDrive();
    if (d) el.append(h('span', { class: 'where', title: vendorName(d.vendor) + ' · ' + d.name }, vbadge(d.vendor, 14), vendorName(d.vendor) + ' · ' + d.name));
    return el;
  }

  function iconFor(e) {
    if (e.isSpace) return icon('users', 18);
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
    const ro = isRO(e);
    const btn = (label, ic, fn, dis) => h('button', { class: 'iconbtn', title: label, 'aria-label': label + ' ' + e.name, onclick: fn, disabled: dis }, ic);
    const d = driveBy(e.acct, e.drive);
    const vendor = e.vendor || (d && d.vendor);
    return h('tr', { class: isSel(e) ? 'selected' : null },
      h('td', { class: 'sel' }, e.isSpace ? '' : h('input', { type: 'checkbox', checked: isSel(e), 'aria-label': 'Select ' + e.name, onclick: (ev) => { ev.stopPropagation(); toggleSel(e, ev); } })),
      h('td', {}, h('button', { class: 'name', onclick: () => (e.isDir ? go({ acct: e.acct, drive: e.drive, path: e.path }) : preview(e)) },
        h('span', { class: 'ic', 'aria-hidden': 'true' }, iconFor(e), vendor ? h('span', { class: 'vb-corner' }, vbadge(vendor, 12)) : ''),
        h('span', { class: 't' }, e.name))),
      h('td', { class: 'muted num' }, e.isSpace ? (e.used != null ? fmtSize(e.used) : '') : e.isDir ? '' : fmtSize(e.size)),
      h('td', { class: 'muted mod' }, fmtDate(e.modTime)),
      h('td', { class: 'actions' }, e.isSpace ? '' : [
        e.isDir ? '' : btn('Download', icon('download'), () => { location.href = fileUrl(e); }),
        btn('Rename', icon('pencil'), () => rename(e), ro), btn('Move', icon('move'), () => moveOrCopy(e, 'move'), ro),
        btn('Copy', icon('copy'), () => moveOrCopy(e, 'copy'), ro), btn('Delete', icon('trash'), () => remove(e), ro)]));
  }

  // Can files be added here? Inside a folder: unless the space is read-only. At the top of Personal: yes if
  // some personal drive is writable (the page asks which one). Teams and Shared tops only list spaces.
  const canAdd = () => (state.loc ? !(curDrive() && curDrive().readOnly)
    : state.cat === 'photos' ? state.mappings.length > 0
    : state.cat === 'personal' && writableDrives().some((d) => d.kind === 'personal'));
  const addHint = () => {
    if (state.loc) return curDrive() && curDrive().readOnly ? 'This space is read-only' : '';
    if (state.cat === 'teams') return 'Open a team space to add files';
    if (state.cat === 'shared') return 'Shared items are read-only here';
    return '';
  };

  // "+ New": one button for what you can add here. Photos only talks about pictures and videos.
  function newMenu(fileInput, inPhotos) {
    const close = () => { state.menuOpen = false; render(); };
    const item = (label, ic, fn) => h('button', { class: 'mi', role: 'menuitem', onclick: (ev) => { ev.stopPropagation(); fn(); } }, icon(ic, 16), label);
    return h('span', { class: 'newmenu' },
      h('button', { class: 'btn primary', disabled: !canAdd(), 'aria-haspopup': 'menu', 'aria-expanded': String(state.menuOpen && canAdd()),
        onclick: (ev) => { ev.stopPropagation(); state.menuOpen = !state.menuOpen; render(); } }, icon('plus'), 'New', icon('chevronDown', 14)),
      state.menuOpen && canAdd() ? h('div', { class: 'menu', role: 'menu' },
        item('New folder', 'folderPlus', () => { close(); newFolder(); }),
        // click first: render() replaces the page, and the picker must open from this very click
        item(inPhotos ? 'Upload pictures and videos' : 'Upload files', 'upload', () => { fileInput.click(); close(); })) : '');
  }
  document.addEventListener('click', (ev) => {
    if (state.menuOpen && !ev.target.closest('.newmenu')) { state.menuOpen = false; render(); }
    if (state.acctMenu && !ev.target.closest('.acctmenu')) { state.acctMenu = null; render(); }
  });

  // ---------- the accounts in the top bar ----------
  // One round button per server: the user's picture from that server when it has one, else the vendor's icon
  // (dimmed when not connected). It opens a small menu to disconnect (or connect).
  function avatarFace(b) {
    const fallback = () => vbadge(b.vendor, 28);
    if (!b.connected) return h('span', { class: 'face off' }, fallback());
    if (state.avatars[b.id] === 'none') return h('span', { class: 'face' }, fallback());
    const img = h('img', { class: 'avatar', alt: '', src: '/api/avatar?' + qs({ acct: b.id, size: 64 }) });
    img.onload = () => { state.avatars[b.id] = 'ok'; };
    img.onerror = () => { state.avatars[b.id] = 'none'; const f = img.closest('.face'); if (f) f.replaceChildren(fallback()); };
    // a picture gets the vendor's small icon in its corner, so you still see which server it is
    return h('span', { class: 'face' }, img, h('span', { class: 'vb-corner' }, vbadge(b.vendor, 12)));
  }

  function accountButton(b) {
    const open = state.acctMenu === b.id;
    const run = async (fn) => { state.acctMenu = null; try { await fn(); await boot(); } catch (e) { fail(e); render(); } };
    const action = b.connected
      ? h('button', { class: 'mi', role: 'menuitem', onclick: () => run(() => api('/auth/disconnect?' + qs({ backend: b.id }), { method: 'POST' })) }, icon('unplug', 16), 'Disconnect')
      : b.oidc ? h('a', { class: 'mi', role: 'menuitem', href: '/auth/login?' + qs({ backend: b.id }) }, icon('plug', 16), 'Connect')
      : b.demo ? h('button', { class: 'mi', role: 'menuitem', onclick: () => run(() => api('/auth/demo?' + qs({ backend: b.id }), { method: 'POST' })) }, icon('plug', 16), 'Connect')
      : h('span', { class: 'mi off' }, 'Not available to connect here');
    return h('span', { class: 'acctmenu' },
      h('button', { class: 'acctbtn', title: vendorName(b.vendor) + (b.connected ? ' · ' + (b.user || 'connected') : ' · not connected'), 'aria-haspopup': 'menu', 'aria-expanded': String(open),
        'aria-label': vendorName(b.vendor) + (b.connected ? ', signed in as ' + (b.user || '') : ', not connected'),
        onclick: (ev) => { ev.stopPropagation(); state.menuOpen = false; state.acctMenu = open ? null : b.id; render(); } }, avatarFace(b)),
      open ? h('div', { class: 'menu right', role: 'menu' },
        h('div', { class: 'mhead' }, vbadge(b.vendor, 18), h('div', {}, h('b', {}, vendorName(b.vendor)), h('small', {}, b.connected ? (b.user || 'connected') : 'not connected'))),
        action) : '');
  }

  function appView() {
    const side = h('aside', {}, h('h2', {}, 'Library'), ...CATS.map((c) => {
      const vendors = c.id === 'photos'
        ? [...new Set(state.mappings.map((m) => (driveBy(m.acct, m.drive) || {}).vendor).filter(Boolean))]
        : [...new Set(drivesOf(c.id).map((d) => d.vendor))];
      return h('button', { class: 'drive', 'aria-current': c.id === state.cat && !state.settings ? 'true' : 'false', onclick: () => openCat(c.id) },
        h('span', { class: 'dn' }, icon(c.icon), c.label),
        h('small', {}, catSummary(c), h('span', { class: 'vbs' }, vendors.map((v) => vbadge(v, 12)))));
    }));

    const inPhotos = state.cat === 'photos';
    const fileInput = h('input', { type: 'file', multiple: true, hidden: true, accept: inPhotos ? 'image/*,video/*' : null, onchange: () => { uploadFiles([...fileInput.files]); fileInput.value = ''; } });
    const hiddenHint = state.hiddenCount ? h('span', { class: 'ro' }, state.hiddenCount + ' hidden ', h('button', { class: 'linkbtn', onclick: () => { state.showHidden = !state.showHidden; load(); } }, state.showHidden ? 'Hide again' : 'Show')) : '';
    // Photos: just New and the Gallery | Folders switch, centred. Where the Photos folders are chosen lives in Settings.
    const normalBar = inPhotos
      ? h('div', { class: 'toolbar photobar' }, fileInput,
          state.mappings.length ? newMenu(fileInput, true) : h('span', {}),
          state.mappings.length ? h('span', { class: 'seg', role: 'group', 'aria-label': 'Photos view' },
            ['gallery', 'folders'].map((v) => h('button', { class: 'btn', 'aria-pressed': String(!state.loc ? state.photoView === v : v === 'folders'), onclick: () => {
              clearSel(); setPhotoView(v); state.loc = null; history.pushState(null, '', hashOf()); load(); } }, v === 'gallery' ? 'Gallery' : 'Folders'))) : h('span', {}),
          h('span', {}))
      : h('div', { class: 'toolbar' }, fileInput, newMenu(fileInput, false),
          addHint() ? h('span', { class: 'ro' }, addHint()) : '', hiddenHint,
          h('span', { class: 'spacer' }),
          h('button', { class: 'iconbtn', title: 'Refresh', 'aria-label': 'Refresh', onclick: () => load() }, icon('refresh')));
    const toolbar = state.sel.size ? selBar() : normalBar;

    const body = state.loading ? h('div', { class: 'empty' }, 'Loading…')
      : inPhotos ? photosView()
      : state.entries.length === 0 ? h('div', { class: 'empty' }, drivesOf(state.cat).length === 0 && !state.loc ? 'Nothing here yet: none of your connected accounts has this kind of space.' : 'This folder is empty.' + (canAdd() ? ' Drop files here to upload.' : ''))
      : h('table', {}, h('thead', {}, h('tr', {}, h('th', { class: 'sel' }, selectAllBox()), h('th', {}, 'Name'), h('th', { class: 'num' }, 'Size'), h('th', { class: 'mod' }, 'Modified'), h('th', {}, ''))),
          h('tbody', {}, ...state.entries.map(row)));

    const main = state.settings ? h('main', {}, settingsView()) : h('main', {}, crumbs(), toolbar, body);
    if (!state.settings) wireDrop(main);

    return h('div', { class: 'shell' },
      h('header', { class: 'topbar' }, logo('logo'), h('div', { class: 'spacer' }),
        ...state.backends.map(accountButton),
        themeBtn(), h('button', { class: 'iconbtn theme' + (state.settings ? ' active' : ''), title: 'Settings', 'aria-label': 'Settings', 'aria-pressed': String(state.settings),
          onclick: () => (state.settings ? openCat(state.cat) : openSettingsPage()) }, icon('settings', 18)),
        h('button', { class: 'btn small', onclick: signOut }, 'Sign out')),
      h('div', { class: 'layout' }, side, main));
  }

  function render() {
    const app = $('#app');
    const scroll = (app.querySelector('main') || {}).scrollTop || 0;
    app.replaceChildren(state.me ? appView() : loginView());
    const main = app.querySelector('main');
    if (main && scroll) main.scrollTop = scroll;
    document.title = (state.me ? catById(state.cat).label + ' · ' : '') + 'odrive demo · Odraz SDK';
  }

  // ---------- navigation ----------
  // Inside Photos the folder you are in is a drive path too, so the hash says cat=photos to keep you there.
  const hashOf = () => state.settings ? '#settings' : '#' + (state.loc ? qs({ ...(state.cat === 'photos' ? { cat: 'photos' } : {}), acct: state.loc.acct, drive: state.loc.drive, path: state.loc.path }) : qs({ cat: state.cat }));
  const setPhotoView = (v) => { state.photoView = v; localStorage.setItem('photoView', v); };
  function openCat(id) {
    clearSel(); state.settings = false;
    if (id === 'photos' && state.loc) setPhotoView('folders'); // coming up from a folder: show the folders, not the gallery
    state.cat = id; state.loc = null; history.pushState(null, '', hashOf()); load();
  }
  const isPhotoRoot = (loc) => state.cat === 'photos' && state.mappings.some((m) => m.acct === loc.acct && m.drive === loc.drive && m.folder === (loc.path || '/'));
  function go(loc) {
    clearSel(); state.settings = false;
    const d = driveBy(loc.acct, loc.drive);
    if (state.cat === 'photos') setPhotoView('folders');
    else if (d) state.cat = catOfKind(d.kind);
    state.loc = isPhotoRoot(loc) ? null : { acct: loc.acct, drive: loc.drive, path: loc.path || '/' };
    history.pushState(null, '', hashOf()); load();
  }
  window.addEventListener('popstate', () => { clearSel(); readHash(); load(); });
  function readHash() {
    state.settings = location.hash === '#settings';
    const p = new URLSearchParams(location.hash.slice(1));
    const d = driveBy(p.get('acct'), p.get('drive'));
    if (d) {
      state.loc = { acct: d.acct, drive: d.id, path: p.get('path') || '/' };
      state.cat = p.get('cat') === 'photos' ? 'photos' : catOfKind(d.kind);
      if (state.cat === 'photos') { state.photoView = 'folders'; if (isPhotoRoot(state.loc)) state.loc = null; }
      return;
    }
    state.loc = null;
    state.cat = CATS.some((c) => c.id === p.get('cat')) ? p.get('cat') : 'personal';
  }

  const byName = (a, b) => (a.isDir !== b.isDir ? (a.isDir ? -1 : 1) : a.name.toLowerCase().localeCompare(b.name.toLowerCase()) || (a.vendor || '').localeCompare(b.vendor || ''));

  // The top of a category: Teams lists the spaces themselves; Personal and Shared merge the root folders of
  // every drive in the category. A backend that fails is reported and does not hide the others.
  async function loadCategory(cat) {
    const ds = drivesOf(cat);
    if (cat === 'teams') return ds.map((d) => ({ name: d.name, path: '/', isDir: true, isSpace: true, acct: d.acct, drive: d.id, vendor: d.vendor, used: d.used })).sort(byName);
    const res = await Promise.allSettled(ds.map((d) => api('/api/list?' + qs({ acct: d.acct, drive: d.id, path: '/' }))
      .then((r) => r.entries.map((e) => ({ ...e, acct: d.acct, drive: d.id, vendor: d.vendor })))));
    res.forEach((r, i) => { if (r.status === 'rejected' && r.reason.status !== 401) fail(new Error(vendorName(ds[i].vendor) + ': ' + r.reason.message)); });
    let all = res.flatMap((r) => (r.status === 'fulfilled' ? r.value : []));
    // Nextcloud also shows a team folder as a folder inside the user's own files; it already has its own
    // entry under Teams, so do not list it twice.
    if (cat === 'personal') all = all.filter((e) => !(e.isDir && e.vendor === 'nextcloud' && state.drives.some((d) => d.acct === e.acct && d.kind === 'project' && d.name === e.name)));
    return all.sort(byName);
  }

  async function loadPhotos() {
    const res = await api('/api/photos');
    Object.entries(res.errors || {}).forEach(([k, v]) => fail(new Error(k + ': ' + v)));
    return res;
  }
  async function refreshMappings() {
    try {
      const res = await api('/api/photos/config');
      state.mappings = res.mappings || []; state.problems = res.problems || [];
    } catch (e) { state.mappings = []; if (e.status !== 401) fail(e); }
  }

  // Only the latest load() may update the page: a slower, older one (for example the reload after a delete,
  // still in flight when you click another tab) must not overwrite it.
  let loadSeq = 0;
  async function load() {
    const my = ++loadSeq;
    const stale = () => my !== loadSeq;
    state.loading = true; render();
    try {
      let entries;
      if (state.loc) {
        const { acct, drive: dv } = state.loc;
        const res = await api('/api/list?' + qs({ acct, drive: dv, path: state.loc.path }));
        if (stale()) return;
        const vendor = (driveBy(acct, dv) || {}).vendor;
        state.loc.path = res.path; entries = res.entries.map((e) => ({ ...e, acct, drive: dv, vendor }));
        if (state.cat === 'photos') entries = entries.map((e) => ({ ...e, kind: kindOf(e) }));
      } else if (state.cat === 'photos' && state.photoView === 'gallery') {
        const res = state.mappings.length ? await loadPhotos() : { items: [], truncated: false };
        if (stale()) return;
        state.photos = res; entries = [];
      } else if (state.cat === 'photos') {
        // Folders view, top level: what is INSIDE the folders you chose (the chosen folders themselves are skipped),
        // merged across drives. A server that fails is reported and does not hide the others.
        const res = await Promise.allSettled(state.mappings.map((m) => api('/api/list?' + qs({ acct: m.acct, drive: m.drive, path: m.folder }))
          .then((r) => r.entries.map((e) => ({ ...e, acct: m.acct, drive: m.drive, vendor: (driveBy(m.acct, m.drive) || {}).vendor, kind: kindOf(e) })))));
        res.forEach((r, i) => { if (r.status === 'rejected' && r.reason.status !== 401) fail(new Error(state.mappings[i].folder + ': ' + r.reason.message)); });
        if (stale()) return;
        entries = res.flatMap((r) => (r.status === 'fulfilled' ? r.value : [])).sort(byName);
      } else {
        entries = await loadCategory(state.cat);
        if (stale()) return;
      }
      state.entries = applyHidden(entries);
    } catch (e) {
      if (stale()) return;
      state.entries = [];
      if (state.loc && state.loc.path !== '/') { state.loc.path = '/'; history.replaceState(null, '', hashOf()); fail(e); return load(); }
      if (state.loc) { state.loc = null; history.replaceState(null, '', hashOf()); fail(e); return load(); }
      if (e.status !== 401) fail(e);
    }
    state.loading = false; render();
  }

  // ---------- selection bar and batch actions ----------
  // The header checkbox of the table: all (or none) of what is shown; "mixed" when only some are selected.
  function selectAllBox() {
    const list = shownItems(), n = list.filter(isSel).length;
    const box = h('input', { type: 'checkbox', checked: list.length > 0 && n === list.length, 'aria-label': 'Select all', onclick: (ev) => {
      if (n === list.length) clearSel(); else for (const x of list) state.sel.set(selKey(x), x);
      ev.stopPropagation(); render(); } });
    box.indeterminate = n > 0 && n < list.length;
    return box;
  }

  function selBar() {
    const items = selectedList(), n = items.length, anyRO = items.some(isRO);
    const one = n === 1 ? items[0] : null;
    return h('div', { class: 'toolbar selbar' },
      h('span', { class: 'selcount' }, n + ' selected'),
      h('button', { class: 'btn', onclick: batchDownload }, icon('download'), one && !one.isDir ? 'Download' : 'Download .zip'),
      one ? h('button', { class: 'btn', disabled: anyRO, onclick: () => rename(one) }, icon('pencil'), 'Rename') : '',
      h('button', { class: 'btn', disabled: anyRO, onclick: () => batchMoveCopy('move') }, icon('move'), 'Move…'),
      h('button', { class: 'btn', onclick: () => batchMoveCopy('copy') }, icon('copy'), 'Copy…'),
      h('button', { class: 'btn danger', disabled: anyRO, onclick: batchDelete }, icon('trash'), 'Delete'),
      shownItems().length > n ? h('button', { class: 'btn small', onclick: selectAll }, 'Select all') : '',
      h('button', { class: 'btn small', onclick: () => { clearSel(); render(); } }, 'Clear'),
      anyRO ? h('span', { class: 'ro' }, 'Some items are read-only: they can be copied or downloaded only') : '');
  }

  // One file downloads as itself; several files, or any folder, as one zip built by the gateway.
  function batchDownload() {
    const items = selectedList();
    if (items.length === 1 && !items[0].isDir) { location.href = fileUrl(items[0]); return; }
    const url = '/api/zip?' + items.map((e) => 'item=' + encodeURIComponent(selKey(e))).join('&');
    if (url.length > 7500) { toast('That is too many items for one download: select fewer.', true); return; }
    location.href = url;
  }

  // Runs one request per item and reports once: how many worked, how many were skipped (the name already
  // exists there), and what failed. The selection is cleared and the view reloaded.
  async function runBatch(items, verb, one) {
    const r = { ok: 0, skipped: 0, failed: [] };
    for (const e of items) {
      try { await one(e); r.ok++; }
      catch (x) { if (x.code === 'exists') r.skipped++; else r.failed.push('“' + e.name + '”: ' + x.message); }
    }
    clearSel();
    const noun = (n) => n + (n === 1 ? ' item' : ' items');
    if (r.ok || r.skipped) toast(verb + ' ' + noun(r.ok) + (r.skipped ? ' (' + r.skipped + ' skipped: the name already exists there)' : ''));
    r.failed.slice(0, 3).forEach((m) => fail(new Error(m)));
    if (r.failed.length > 3) fail(new Error('…and ' + (r.failed.length - 3) + ' more failed'));
    load();
  }

  async function batchDelete() {
    const items = selectedList(), dirs = items.some((e) => e.isDir);
    if (!(await confirmBox('Delete ' + items.length + (items.length === 1 ? ' item?' : ' items?'),
      items.length === 1 ? '“' + items[0].name + '”' + (dirs ? ' and everything in it' : '') + ' will be deleted.'
        : 'These ' + items.length + ' items' + (dirs ? ' (folders with everything in them)' : '') + ' will be deleted.', 'Delete', true))) return;
    await runBatch(items, 'Deleted', (e) => api('/api/entry?' + qs(at(e, { path: e.path, recursive: 1 })), { method: 'DELETE' }));
  }

  // Move or copy everything selected into one folder (of any server you can write to).
  async function batchMoveCopy(kind) {
    const items = selectedList(), verb = kind === 'move' ? 'Move' : 'Copy';
    const drives = writableDrives();
    if (!drives.length) { fail(new Error('There is nowhere to ' + verb.toLowerCase() + ' to.')); return; }
    const dest = await folderDialog({
      title: verb + ' ' + (items.length === 1 ? '“' + items[0].name + '”' : items.length + ' items') + ' to…', drives, start: { acct: items[0].acct, drive: items[0].drive },
      allowRoot: true, cancel: 'Cancel', confirm: (dir, d) => verb + ' to ' + (dir === '/' ? 'the top of ' + d.name : dir) });
    if (!dest) return;
    const into = dest.folder;
    const bad = items.find((e) => e.isDir && dest.drive.acct === e.acct && dest.drive.id === e.drive && (into === e.path || into.startsWith(e.path + '/')));
    if (bad) { fail(new Error('You cannot ' + verb.toLowerCase() + ' “' + bad.name + '” into itself.')); return; }
    const cross = items.some((e) => e.acct !== dest.drive.acct || e.drive !== dest.drive.id);
    if (cross || items.length > 5) toast((kind === 'move' ? 'Moving ' : 'Copying ') + items.length + ' items…');
    await runBatch(items, kind === 'move' ? 'Moved' : 'Copied', async (e) => {
      const to = join(into, e.name), same = e.acct === dest.drive.acct && e.drive === dest.drive.id;
      if (same && to === e.path) throw Object.assign(new Error('already there'), { code: 'exists' });
      await api('/api/' + kind + '?' + qs(at(e, { from: e.path, to, ...(same ? {} : { toAcct: dest.drive.acct, toDrive: dest.drive.id }) })), { method: 'POST' });
    });
  }

  // ---------- Photos ----------
  const thumbUrl = (it, size = 256) => '/api/thumb?' + qs(at(it, { path: it.path, size }));

  // Servers rarely make thumbnails for videos (OpenCloud has none; Nextcloud needs ffmpeg). When there is none,
  // the tile shows the video's own first frame, fetched with a Range request only once the tile scrolls into view.
  const lazyLoad = new IntersectionObserver((entries) => entries.forEach((en) => {
    if (en.isIntersecting) { lazyLoad.unobserve(en.target); en.target._load && en.target._load(); }
  }), { rootMargin: '300px' });

  // One tile: a picture or video (thumbnail), a folder (Folders view), or another file ("Show all files").
  // Hover or keyboard focus shows its actions: download, rename, move, delete.
  function tile(it) {
    const kind = it.kind || kindOf(it);
    const frame = h('span', { class: 'tmedia' });
    const generic = (ic) => frame.replaceChildren(h('span', { class: 'tgen' }, icon(ic, 28)));
    if (kind === 'dir') generic('folder');
    else {
      const img = h('img', { alt: '', loading: 'lazy', src: thumbUrl(it) });
      // The server renders thumbnails. If it has none for this file (HTTP 501), a picture falls back to the
      // original (shown scaled down by the browser); anything else gets a plain tile.
      img.onerror = () => {
        if (kind === 'image' && !img.dataset.fallback) { img.dataset.fallback = '1'; img.src = fileUrl(it, { inline: 1 }); }
        else if (kind === 'video') {
          const v = h('video', { muted: true, playsinline: true, preload: 'none', tabindex: '-1', 'aria-hidden': 'true' });
          v.muted = true;
          v.onerror = () => generic('film');
          v._load = () => { v.preload = 'metadata'; v.src = fileUrl(it, { inline: 1 }) + '#t=0.1'; };
          frame.replaceChildren(v); lazyLoad.observe(v);
        } else generic('image');
      };
      frame.append(img);
    }
    const ro = isRO(it);
    const act = (label, ic, fn) => h('button', { class: 'iconbtn', title: label, 'aria-label': label + ' ' + it.name, onclick: (ev) => { ev.stopPropagation(); fn(); }, disabled: ro }, ic);
    const actions = h('span', { class: 'tact' },
      kind === 'dir' ? '' : h('a', { class: 'iconbtn', title: 'Download', 'aria-label': 'Download ' + it.name, href: fileUrl(it), onclick: (ev) => ev.stopPropagation() }, icon('download')),
      act('Rename', icon('pencil'), () => rename(it)), act('Move', icon('move'), () => moveOrCopy(it, 'move')), act('Delete', icon('trash'), () => remove(it)));
    return h('div', { class: 'tile' + (kind === 'dir' ? ' dir' : '') + (isSel(it) ? ' selected' : '') },
      h('button', { class: 'topen', title: it.name + (it.vendor ? ' · ' + vendorName(it.vendor) : ''), onclick: (ev) => { if (state.sel.size) toggleSel(it, ev); else if (kind === 'dir') go({ acct: it.acct, drive: it.drive, path: it.path }); else preview(it); } },
        frame, kind === 'video' ? h('span', { class: 'play' }, icon('play', 14)) : '', h('span', { class: 'tl' }, it.name)),
      h('input', { type: 'checkbox', class: 'tsel', checked: isSel(it), 'aria-label': 'Select ' + it.name, onclick: (ev) => { ev.stopPropagation(); toggleSel(it, ev); } }),
      actions, it.vendor ? h('span', { class: 'vb-corner' }, vbadge(it.vendor, 14)) : '');
  }

  function photosView() {
    if (!state.mappings.length) {
      const note = state.problems.length ? h('p', { class: 'meta' }, 'A drive has a ' + '.odraz/manifest.json this page cannot read (invalid, or written by a newer client). It is left untouched.') : '';
      return h('div', { class: 'prompt' }, h('div', { class: 'pic' }, icon('image', 28)),
        h('h2', {}, 'Choose your photos folder'),
        h('p', {}, 'Pick the folder where your pictures and videos live. Photos then shows just the pictures and videos from it, from every server you have connected. Nothing is moved or renamed.'),
        h('button', { class: 'btn primary', onclick: () => chooseFolder() }, icon('folder'), 'Choose folder…'), note);
    }
    const gallery = !state.loc && state.photoView === 'gallery';
    const items = photoItems();
    if (!items.length) return h('div', { class: 'empty' }, gallery ? 'No pictures or videos in your photo folders yet.'
      : 'No pictures or videos here yet.' + (canAdd() ? ' Drop some here to upload.' : ''));
    return h('div', {}, h('div', { class: 'grid' + (state.sel.size ? ' selecting' : '') }, items.map(tile)),
      gallery && state.photos.truncated ? h('p', { class: 'meta' }, 'Showing the first ' + items.length + ' items.') : '');
  }

  // Browse a drive's folders and pick one (or create one on the way). Used to choose the Photos folder and as the
  // destination chooser for Move and Copy. Options: title, drives (the drives that may be chosen), start ({acct,
  // drive}), allowRoot (the top of a drive is a valid choice), confirm(dir, drive) -> the button label, cancel
  // (its label) and hideOption (offer "hide from Personal"). Resolves {drive, folder, hide} or null.
  function folderDialog(o) {
    const ds = o.drives;
    return openDialog((done) => {
      let drive = ds.find((d) => o.start && d.acct === o.start.acct && d.id === o.start.drive) || ds.find((d) => o.hideOption && !mappingOf(d.acct, d.id)) || ds[0];
      let dir = '/';
      const hide = h('input', { type: 'checkbox', checked: true });
      const list = h('div', { class: 'folders' });
      const bar = h('div', { class: 'crumbs pickcrumbs' });
      const useBtn = h('button', { class: 'btn primary' }, '');
      const sel = ds.length > 1 ? h('select', { 'aria-label': 'Server', onchange: () => { drive = ds[+sel.value]; dir = '/'; show(); } },
        ds.map((d, i) => h('option', { value: i, selected: d === drive }, driveLabel(d)))) : null;
      async function show() {
        list.replaceChildren(h('div', { class: 'empty' }, 'Loading…'));
        bar.replaceChildren(...[['/', drive.name], ...dir.split('/').filter(Boolean).map((seg, i, a) => ['/' + a.slice(0, i + 1).join('/'), seg])].flatMap(([p, label], i) => [
          i ? h('span', { class: 'sep' }, icon('chevron', 14)) : '', h('button', { type: 'button', onclick: () => { dir = p; show(); } }, label)]));
        useBtn.disabled = dir === '/' && !o.allowRoot; useBtn.textContent = o.confirm(dir, drive);
        try {
          const res = await api('/api/list?' + qs({ acct: drive.acct, drive: drive.id, path: dir }));
          const dirs = res.entries.filter((e) => e.isDir && !e.name.startsWith('.'));
          if (!nameIn.value) nameIn.placeholder = o.hideOption && dir === '/' && !dirs.some((e) => e.name === 'Photos') ? 'New folder name, e.g. Photos' : 'New folder name';
          list.replaceChildren(...(dirs.length ? dirs.map((e) => h('button', { type: 'button', class: 'frow', onclick: () => { dir = e.path; show(); } }, icon('folder', 16), e.name, icon('chevron', 14)))
            : [h('div', { class: 'empty' }, 'No folders here.')]));
        } catch (e) { list.replaceChildren(h('div', { class: 'empty' }, e.message)); }
      }
      // Create a folder right here and go into it, ready to be used.
      const nameIn = h('input', { type: 'text', placeholder: 'New folder name', 'aria-label': 'New folder name', onkeydown: (ev) => { if (ev.key === 'Enter') { ev.preventDefault(); create(); } } });
      const createBtn = h('button', { type: 'button', class: 'btn', onclick: () => create() }, icon('folderPlus'), 'Create');
      async function create() {
        const name = nameIn.value.trim();
        if (!name) { nameIn.focus(); return; }
        if (/[\/\\]/.test(name)) { toast('A folder name cannot contain / or \\', true); return; }
        createBtn.disabled = true;
        try {
          await api('/api/mkdir?' + qs({ acct: drive.acct, drive: drive.id, path: join(dir, name) }), { method: 'POST' });
          dir = join(dir, name); nameIn.value = '';
          await show();
        } catch (e) { fail(e); } finally { createBtn.disabled = false; }
      }
      const form = h('form', { method: 'dialog', onsubmit: (ev) => { ev.preventDefault(); if (dir !== '/' || o.allowRoot) { done({ drive, folder: dir, hide: hide.checked }); closeDialog(); } } },
        h('h2', {}, o.title), sel ? h('label', {}, 'Server', sel) : '', bar, list,
        h('div', { class: 'newfolder' }, nameIn, createBtn),
        o.hideOption ? h('label', { class: 'check' }, hide, 'Hide this folder from Personal (it stays reachable with "Show hidden")') : '',
        h('div', { class: 'row' }, h('button', { type: 'button', class: 'btn', onclick: () => closeDialog() }, o.cancel || 'Cancel'), useBtn));
      show();
      return [form];
    }, true);
  }

  const pickFolder = (startDrive) => folderDialog({
    title: 'Choose your photos folder', drives: state.drives.filter((d) => d.kind === 'personal' && !d.readOnly), start: startDrive,
    hideOption: true, cancel: 'Not now', confirm: (dir) => (dir === '/' ? 'Open or create a folder' : 'Use ' + dir) });

  async function chooseFolder(startDrive) {
    const ds = state.drives.filter((d) => d.kind === 'personal' && !d.readOnly);
    if (!ds.length) { toast('No personal space to choose a folder in', true); return; }
    const pick = await pickFolder(startDrive);
    if (!pick) return;
    try {
      await api('/api/photos/folder?' + qs({ acct: pick.drive.acct, drive: pick.drive.id, folder: pick.folder, hide: pick.hide ? 1 : 0 }), { method: 'PUT' });
      toast('Photos folder set to ' + pick.folder + ' on ' + driveLabel(pick.drive));
    } catch (e) { fail(e); return; }
    await refreshMappings(); if (state.settings) render(); else load();
  }

  // ---------- the Settings page ----------
  // Where you set things up rather than use them. For now: which folder feeds the Photos section, per server.
  // (Accounts are in the top bar, where you reach them quickly.)
  function openSettingsPage() {
    clearSel(); state.menuOpen = false; state.acctMenu = null; state.settings = true;
    history.pushState(null, '', hashOf()); render();
    refreshMappings().then(() => { if (state.settings) render(); });
  }

  function settingsView() {
    const redraw = async () => { await refreshMappings(); if (state.settings) render(); };
    const drives = state.drives.filter((d) => d.kind === 'personal' && !d.readOnly);
    const unmapped = drives.filter((d) => !mappingOf(d.acct, d.id));
    const rows = state.mappings.map((m) => {
      const d = driveBy(m.acct, m.drive);
      const hide = h('input', { type: 'checkbox', checked: m.hideInFiles, onchange: async () => {
        try { await api('/api/photos/folder?' + qs({ acct: m.acct, drive: m.drive, folder: m.folder, hide: hide.checked ? 1 : 0 }), { method: 'PUT' }); await redraw(); } catch (e) { fail(e); } } });
      return h('div', { class: 'mrow' }, d ? vbadge(d.vendor, 20) : '', h('div', { class: 'mt' }, h('b', {}, m.folder), h('small', {}, d ? driveLabel(d) : m.acct)),
        h('label', { class: 'check' }, hide, 'Hide from Personal'),
        h('button', { type: 'button', class: 'btn small', onclick: () => chooseFolder(m) }, 'Change'),
        h('button', { type: 'button', class: 'btn small', onclick: async () => {
          try { await api('/api/photos/folder?' + qs({ acct: m.acct, drive: m.drive }), { method: 'DELETE' }); await redraw(); } catch (e) { fail(e); } } }, 'Remove'));
    });
    return h('div', { class: 'settings' }, h('h1', {}, 'Settings'),
      h('section', { class: 'scard' }, h('h2', {}, 'Photos'),
        h('p', { class: 'meta' }, 'The Photos section shows the pictures and videos in the folders you choose here, from every server. Nothing is moved or renamed: the choice is stored in each server\'s own ', h('code', {}, '.odraz/manifest.json'), '.'),
        ...(rows.length ? rows : [h('p', { class: 'meta' }, 'No folder chosen yet.')]),
        state.problems.length ? h('p', { class: 'meta' }, 'A server has a .odraz/manifest.json this page cannot read (invalid, or written by a newer client). It is left untouched.') : '',
        drives.length === 0 ? h('p', { class: 'meta' }, 'Connect a server first (the round buttons in the top bar).')
          : unmapped.length ? h('div', { class: 'row left' }, h('button', { type: 'button', class: 'btn', onclick: () => chooseFolder() },
            icon('folderPlus'), rows.length ? 'Add a folder on ' + (unmapped.length === 1 ? driveLabel(unmapped[0]) : 'another server') : 'Choose folder…')) : ''));
  }

  // ---------- choosing where something goes ----------
  const driveLabel = (d) => vendorName(d.vendor) + ' · ' + d.name;
  function chooseDrive(title, drives, preferred) {
    return openDialog((done) => {
      let sel = drives.find((d) => preferred && d.acct === preferred.acct && d.id === preferred.drive) || drives[0];
      const form = h('form', { method: 'dialog', onsubmit: (ev) => { ev.preventDefault(); done(sel); closeDialog(); } }, h('h2', {}, title),
        ...drives.map((d) => { const r = h('input', { type: 'radio', name: 'd', checked: d === sel, onchange: () => { sel = d; } });
          return h('label', { class: 'opt' }, r, vbadge(d.vendor, 18), h('span', {}, h('b', {}, vendorName(d.vendor)), ' · ' + d.name)); }),
        h('div', { class: 'row' }, h('button', { type: 'button', class: 'btn', onclick: () => closeDialog() }, 'Cancel'), h('button', { class: 'btn primary' }, 'Continue')));
      return [form];
    });
  }

  // Where an upload / new folder goes: inside the open folder, or (at the top of Personal) the drive you pick.
  async function pickTarget(title) {
    if (state.loc) return { acct: state.loc.acct, drive: state.loc.drive, dir: state.loc.path };
    if (state.cat === 'photos') { // at the top of Photos: one of the folders you chose
      const ms = state.mappings.filter((m) => driveBy(m.acct, m.drive));
      if (!ms.length) return null;
      const d = ms.length === 1 ? driveBy(ms[0].acct, ms[0].drive) : await chooseDrive(title, ms.map((m) => driveBy(m.acct, m.drive)));
      const m = d && ms.find((x) => x.acct === d.acct && x.drive === d.id);
      return m ? { acct: m.acct, drive: m.drive, dir: m.folder } : null;
    }
    const ds = writableDrives().filter((d) => d.kind === 'personal');
    if (!ds.length) return null;
    const d = ds.length === 1 ? ds[0] : await chooseDrive(title, ds, state.lastTarget);
    if (!d) return null;
    state.lastTarget = { acct: d.acct, drive: d.id };
    return { acct: d.acct, drive: d.id, dir: '/' };
  }

  function askDest(verb, e) {
    return openDialog((done) => {
      const ds = writableDrives();
      const sel = h('select', { 'aria-label': 'Destination' }, ds.map((d, i) => h('option', { value: i, selected: d.acct === e.acct && d.id === e.drive }, driveLabel(d))));
      const input = h('input', { type: 'text', value: e.path, 'aria-label': 'Destination path' });
      const form = h('form', { method: 'dialog', onsubmit: (ev) => { ev.preventDefault(); const d = ds[+sel.value]; const p = input.value.trim(); done(p ? { acct: d.acct, drive: d.id, path: p.startsWith('/') ? p : '/' + p } : null); closeDialog(); } },
        h('h2', {}, verb + ' “' + e.name + '”'),
        h('label', {}, 'To', sel),
        h('label', {}, 'Destination path (a new full path, e.g. /Documents/' + e.name + ')', input),
        h('p', { class: 'meta' }, 'Between two different servers the file goes through the gateway: slower than inside one server.'),
        h('div', { class: 'row' }, h('button', { type: 'button', class: 'btn', onclick: () => closeDialog() }, 'Cancel'), h('button', { class: 'btn primary' }, verb)));
      setTimeout(() => { input.focus(); input.select(); }, 0);
      return [form];
    });
  }

  // ---------- actions ----------
  async function preview(e) {
    const type = (e.mime || '').split(';')[0];
    const isImg = type.startsWith('image/');
    const isVid = type.startsWith('video/') || /\.(mp4|m4v|webm|ogv)$/i.test(e.name);
    const isText = type.startsWith('text/') || type === 'application/json' || /\.(md|csv|json|txt|js|css|html)$/i.test(e.name);
    await openDialog((done) => {
      const box = h('div', { class: 'preview' }, h('div', { class: 'empty' }, isImg || isText ? 'Loading…' : isVid ? '' : 'No preview for this type of file.'));
      if (isVid) box.replaceChildren(h('video', { controls: true, preload: 'metadata', src: fileUrl(e, { inline: 1 }), 'aria-label': e.name }));
      if (isImg) { const img = h('img', { alt: e.name, src: fileUrl(e, { inline: 1 }) }); img.onload = () => box.replaceChildren(img); img.onerror = () => box.replaceChildren(h('div', { class: 'empty' }, 'Could not load the image.')); }
      else if (isText) {
        api(fileUrl(e, { inline: 1 }), { raw: true, headers: { Range: 'bytes=0-131071' } }).then((r) => r.text()).then((t) => {
          box.replaceChildren(h('pre', {}, t + (e.size > 131072 ? '\n\n… (showing the first 128 KB)' : '')));
        }).catch((x) => box.replaceChildren(h('div', { class: 'empty' }, x.message)));
      }
      return [h('h2', {}, e.name), box, h('div', { class: 'meta' }, fmtSize(e.size) + ' · ' + (type || 'unknown type') + ' · ' + fmtDate(e.modTime) + (e.vendor ? ' · ' + vendorName(e.vendor) : '')),
        h('div', { class: 'row' }, h('a', { class: 'btn', href: fileUrl(e) }, icon('download'), 'Download'), h('button', { class: 'btn primary', onclick: () => closeDialog() }, 'Close'))];
    }, true);
  }

  async function newFolder() {
    const t = await pickTarget('Create the folder on');
    if (!t) return;
    const name = await askText('New folder', 'Folder name', '', 'Create');
    if (!name) return;
    try { await api('/api/mkdir?' + qs({ acct: t.acct, drive: t.drive, path: join(t.dir, name) }), { method: 'POST' }); toast('Created ' + name); load(); } catch (e) { fail(e); }
  }

  async function rename(e) {
    const name = await askText('Rename', 'New name', e.name, 'Rename');
    if (!name || name === e.name) return;
    try { await api('/api/move?' + qs(at(e, { from: e.path, to: join(parentOf(e.path), name) })), { method: 'POST' }); toast('Renamed to ' + name); load(); }
    catch (x) { fail(x.code === 'exists' ? new Error('“' + name + '” already exists here.') : x); }
  }

  async function moveOrCopy(e, kind) {
    const verb = kind === 'move' ? 'Move' : 'Copy';
    const dest = await askDest(verb, e);
    if (!dest || (dest.acct === e.acct && dest.drive === e.drive && dest.path === e.path)) return;
    const cross = dest.acct !== e.acct || dest.drive !== e.drive;
    if (cross) toast((kind === 'move' ? 'Moving' : 'Copying') + ' “' + e.name + '”…');
    try {
      await api('/api/' + kind + '?' + qs(at(e, { from: e.path, to: dest.path, ...(cross ? { toAcct: dest.acct, toDrive: dest.drive } : {}) })), { method: 'POST' });
      const d = driveBy(dest.acct, dest.drive);
      toast((kind === 'move' ? 'Moved' : 'Copied') + ' to ' + (cross && d ? driveLabel(d) + ' ' : '') + dest.path); load();
    } catch (x) { fail(x.code === 'exists' ? new Error('Something already exists at ' + dest.path + '.') : x); }
  }

  async function remove(e) {
    if (!(await confirmBox('Delete ' + (e.isDir ? 'folder' : 'file') + '?', '“' + e.name + '”' + (e.isDir ? ' and everything in it' : '') + ' will be deleted.', 'Delete', true))) return;
    try { await api('/api/entry?' + qs(at(e, { path: e.path, recursive: 1 })), { method: 'DELETE' }); toast('Deleted ' + e.name); load(); } catch (x) { fail(x); }
  }

  // ---------- upload ----------
  const panel = $('#uploads');
  function putFile(file, t, path, overwrite, onProgress) {
    return new Promise((resolve, reject) => {
      const x = new XMLHttpRequest();
      x.open('PUT', '/api/file?' + qs({ acct: t.acct, drive: t.drive, path, overwrite: overwrite ? 1 : 0 }));
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
    if (!canAdd() || !list.length) return;
    const t = await pickTarget('Upload to');
    if (!t) return;
    let ok = 0;
    for (const file of list) {
      const path = join(t.dir, file.name);
      const bar = h('i', { style: 'width:0%' });
      const pct = h('span', {}, '0%');
      panel.hidden = false;
      panel.append(h('div', { class: 'u' }, h('div', {}, h('span', {}, file.name), pct), h('div', { class: 'bar' }, bar)));
      const prog = (f) => { bar.style.width = f * 100 + '%'; pct.textContent = Math.round(f * 100) + '%'; };
      try {
        try { await putFile(file, t, path, false, prog); }
        catch (e) {
          if (e.code !== 'exists') throw e;
          if (!(await confirmBox('Replace file?', '“' + file.name + '” already exists in this folder.', 'Replace', true))) continue;
          await putFile(file, t, path, true, prog);
        }
        ok++;
      } catch (e) { fail(new Error(file.name + ': ' + e.message)); }
    }
    setTimeout(() => { panel.replaceChildren(); panel.hidden = true; }, 1200);
    if (ok) toast('Uploaded ' + ok + (ok === 1 ? ' file' : ' files'));
    load();
  }

  function wireDrop(main) {
    let depth = 0, veil = null;
    const hide = () => { depth = 0; veil?.remove(); veil = null; };
    main.addEventListener('dragenter', (e) => { if (!canAdd() || !e.dataTransfer?.types.includes('Files')) return; e.preventDefault(); depth++; if (!veil) { veil = h('div', { class: 'drop' }, 'Drop files to upload'); main.append(veil); } });
    main.addEventListener('dragover', (e) => { if (e.dataTransfer?.types.includes('Files')) e.preventDefault(); });
    main.addEventListener('dragleave', () => { if (--depth <= 0) hide(); });
    main.addEventListener('drop', (e) => { if (!e.dataTransfer?.files.length) return; e.preventDefault(); hide(); uploadFiles([...e.dataTransfer.files]); });
  }

  async function signOut() {
    try { await api('/auth/logout', { method: 'POST' }); } catch { /* already gone */ }
    clearSel(); state.me = null; state.drives = []; state.loc = null; history.replaceState(null, '', '/'); render();
  }

  // ---------- start ----------
  async function boot() {
    try {
      const me = await api('/api/me');
      state.backends = me.backends || []; state.demo = !!me.demo;
      if (!me.authenticated) { state.me = null; return render(); }
      state.me = me;
      const res = await api('/api/drives');
      state.drives = res.drives.sort((a, b) => a.name.localeCompare(b.name) || a.vendor.localeCompare(b.vendor));
      Object.entries(res.errors || {}).forEach(([k, v]) => fail(new Error(k + ': ' + v)));
      await refreshMappings();
      readHash();
      history.replaceState(null, '', hashOf());
      await load();
    } catch (e) { state.me = null; render(); if (e.status !== 401) fail(e); }
  }
  boot();
})();
