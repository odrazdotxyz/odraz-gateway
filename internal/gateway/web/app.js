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
    { id: 'teams', label: 'Teams', icon: 'users', kinds: ['project'] },
    { id: 'shared', label: 'Shared', icon: 'share', kinds: ['shared', 'other'] },
  ];
  const state = { me: null, demo: false, oidc: true, backends: [], drives: [], driveErrors: {}, cat: 'personal', loc: null, entries: [], loading: false, lastTarget: null };
  const catById = (id) => CATS.find((c) => c.id === id) || CATS[0];
  const catOfKind = (k) => (CATS.find((c) => c.kinds.includes(k)) || CATS[2]).id;
  const drivesOf = (cat) => state.drives.filter((d) => catById(cat).kinds.includes(d.kind));
  const driveBy = (acct, id) => state.drives.find((d) => d.acct === acct && d.id === id);
  const curDrive = () => (state.loc ? driveBy(state.loc.acct, state.loc.drive) : null);
  const curPath = () => (state.loc ? state.loc.path : '/');
  const isRO = (e) => { const d = driveBy(e.acct, e.drive); return !!(d && d.readOnly); };
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
    const ds = drivesOf(c.id);
    if (!ds.length) return 'Nothing connected';
    if (c.id === 'personal') { const u = ds.reduce((n, d) => n + (d.used || 0), 0); return u ? fmtSize(u) + ' used' : 'Your files'; }
    if (c.id === 'teams') return ds.length + (ds.length === 1 ? ' space' : ' spaces');
    return 'Shared with you';
  };

  function accountRow(b) {
    const personal = state.drives.find((d) => d.acct === b.id && d.kind === 'personal');
    const act = b.connected
      ? h('button', { class: 'iconbtn', title: 'Disconnect ' + vendorName(b.vendor), 'aria-label': 'Disconnect ' + vendorName(b.vendor), onclick: async () => {
          try { await api('/auth/disconnect?' + qs({ backend: b.id }), { method: 'POST' }); await boot(); } catch (e) { fail(e); } } }, icon('unplug'))
      : b.oidc
        ? h('a', { class: 'btn small', href: '/auth/login?' + qs({ backend: b.id }) }, 'Connect')
        : b.demo ? h('button', { class: 'btn small', onclick: async () => { try { await api('/auth/demo?' + qs({ backend: b.id }), { method: 'POST' }); await boot(); } catch (e) { fail(e); } } }, 'Connect') : '';
    return h('div', { class: 'acct' + (b.connected ? '' : ' off') }, vbadge(b.vendor, 18),
      h('div', { class: 'at' }, h('b', {}, vendorName(b.vendor)), h('small', {}, b.connected ? (b.user || 'connected') + (personal && personal.used != null ? ' · ' + fmtSize(personal.used) : '') : 'not connected')), act);
  }

  function crumbs() {
    const el = h('nav', { class: 'crumbs', 'aria-label': 'Folder path' });
    const cat = catById(state.cat);
    const items = [{ label: cat.label, fn: () => openCat(cat.id) }];
    if (state.loc) {
      const d = curDrive();
      const base = { acct: state.loc.acct, drive: state.loc.drive };
      if (cat.id === 'teams' && d) items.push({ label: d.name, fn: () => go({ ...base, path: '/' }) });
      let acc = '';
      for (const seg of state.loc.path.split('/').filter(Boolean)) { acc += '/' + seg; const p = acc; items.push({ label: seg, fn: () => go({ ...base, path: p }) }); }
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
    return h('tr', {},
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
  const canAdd = () => (state.loc ? !(curDrive() && curDrive().readOnly) : state.cat === 'personal' && writableDrives().some((d) => d.kind === 'personal'));
  const addHint = () => {
    if (state.loc) return curDrive() && curDrive().readOnly ? 'This space is read-only' : '';
    if (state.cat === 'teams') return 'Open a team space to add files';
    if (state.cat === 'shared') return 'Shared items are read-only here';
    return '';
  };

  function appView() {
    const side = h('aside', {}, h('h2', {}, 'Library'), ...CATS.map((c) => {
      const vendors = [...new Set(drivesOf(c.id).map((d) => d.vendor))];
      return h('button', { class: 'drive', 'aria-current': c.id === state.cat ? 'true' : 'false', onclick: () => openCat(c.id) },
        h('span', { class: 'dn' }, icon(c.icon), c.label),
        h('small', {}, catSummary(c), h('span', { class: 'vbs' }, vendors.map((v) => vbadge(v, 12)))));
    }), h('h2', { class: 'acc' }, 'Accounts'), ...state.backends.map(accountRow));

    const fileInput = h('input', { type: 'file', multiple: true, hidden: true, onchange: () => { uploadFiles([...fileInput.files]); fileInput.value = ''; } });
    const toolbar = h('div', { class: 'toolbar' },
      h('button', { class: 'btn primary', disabled: !canAdd(), onclick: () => fileInput.click() }, icon('upload'), 'Upload'), fileInput,
      h('button', { class: 'btn', disabled: !canAdd(), onclick: newFolder }, icon('folderPlus'), 'New folder'),
      h('button', { class: 'btn', onclick: () => load() }, icon('refresh'), 'Refresh'),
      addHint() ? h('span', { class: 'ro' }, addHint()) : '');

    const body = state.loading ? h('div', { class: 'empty' }, 'Loading…')
      : state.entries.length === 0 ? h('div', { class: 'empty' }, drivesOf(state.cat).length === 0 && !state.loc ? 'Nothing here yet: none of your connected accounts has this kind of space.' : 'This folder is empty.' + (canAdd() ? ' Drop files here to upload.' : ''))
      : h('table', {}, h('thead', {}, h('tr', {}, h('th', {}, 'Name'), h('th', { class: 'num' }, 'Size'), h('th', { class: 'mod' }, 'Modified'), h('th', {}, ''))),
          h('tbody', {}, ...state.entries.map(row)));

    const main = h('main', {}, crumbs(), toolbar, body);
    wireDrop(main);

    const who = state.backends.filter((b) => b.connected);
    return h('div', { class: 'shell' },
      h('header', { class: 'topbar' }, logo('logo'), h('div', { class: 'spacer' }),
        h('span', { class: 'chip' }, 'Signed in ', ...who.flatMap((b) => [vbadge(b.vendor, 14), h('b', {}, b.user || vendorName(b.vendor)), ' '])),
        themeBtn(), h('button', { class: 'btn small', onclick: signOut }, 'Sign out')),
      h('div', { class: 'layout' }, side, main));
  }

  function render() {
    const app = $('#app');
    app.replaceChildren(state.me ? appView() : loginView());
    document.title = (state.me ? catById(state.cat).label + ' · ' : '') + 'odrive demo · Odraz SDK';
  }

  // ---------- navigation ----------
  const hashOf = () => '#' + (state.loc ? qs({ acct: state.loc.acct, drive: state.loc.drive, path: state.loc.path }) : qs({ cat: state.cat }));
  function openCat(id) { state.cat = id; state.loc = null; history.pushState(null, '', hashOf()); load(); }
  function go(loc) {
    const d = driveBy(loc.acct, loc.drive);
    if (d) state.cat = catOfKind(d.kind);
    state.loc = { acct: loc.acct, drive: loc.drive, path: loc.path || '/' };
    history.pushState(null, '', hashOf()); load();
  }
  window.addEventListener('popstate', () => { readHash(); load(); });
  function readHash() {
    const p = new URLSearchParams(location.hash.slice(1));
    const d = driveBy(p.get('acct'), p.get('drive'));
    if (d) { state.loc = { acct: d.acct, drive: d.id, path: p.get('path') || '/' }; state.cat = catOfKind(d.kind); return; }
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
      } else {
        entries = await loadCategory(state.cat);
        if (stale()) return;
      }
      state.entries = entries;
    } catch (e) {
      if (stale()) return;
      state.entries = [];
      if (state.loc && state.loc.path !== '/') { state.loc.path = '/'; history.replaceState(null, '', hashOf()); fail(e); return load(); }
      if (state.loc) { state.loc = null; history.replaceState(null, '', hashOf()); fail(e); return load(); }
      if (e.status !== 401) fail(e);
    }
    state.loading = false; render();
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
    const isText = type.startsWith('text/') || type === 'application/json' || /\.(md|csv|json|txt|js|css|html)$/i.test(e.name);
    await openDialog((done) => {
      const box = h('div', { class: 'preview' }, h('div', { class: 'empty' }, isImg || isText ? 'Loading…' : 'No preview for this type of file.'));
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
      toast(verb + (kind === 'move' ? 'd' : 'ied') + ' to ' + (cross && d ? driveLabel(d) + ' ' : '') + dest.path); load();
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
    state.me = null; state.drives = []; state.loc = null; history.replaceState(null, '', '/'); render();
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
      readHash();
      history.replaceState(null, '', hashOf());
      await load();
    } catch (e) { state.me = null; render(); if (e.status !== 401) fail(e); }
  }
  boot();
})();
