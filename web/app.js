(() => {
  'use strict';
  const app = document.getElementById('app');
  const state = { user: null, language: localStorage.getItem('tb_language') || 'zh', event: null, stream: null, operations: false, editor: null, roster: {}, users: [], page: 1, filters: {}, connection: '' };
  const langs = { zh: '中文', en: 'English', ko: '한국어' };
  const t = key => window.TB_I18N[state.language]?.[key] || window.TB_I18N.zh[key] || key;
  const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  const $ = (s, root = document) => root.querySelector(s);
  const $$ = (s, root = document) => [...root.querySelectorAll(s)];
  const terminal = e => ['completed', 'cancelled'].includes(e.status);
  const hasRoster = e => e?.teams?.length === 2 && e.teams.every(team => team.players.some(player => player.active !== false));
  const preMatch = e => ['draft', 'captain_vote', 'lineup_confirm'].includes(e.status);
  const manages = e => state.user && (state.user.role === 'superadmin' || Number(state.user.id) === Number(e.manager_id));
  const editable = e => manages(e) && !terminal(e);
  const noScore = e => !(e.matches || []).some(m => m.status === 'finished');
  const canEditSettings = e => editable(e) && preMatch(e) && noScore(e);
  const teamName = (event, team) => t(event.teams[0].id === team.id ? 'teamA' : 'teamB');
  const person = u => `<span class="person">${avatar(u)}<span><strong>${esc(u.display_name)}</strong><small>${esc(u.username || '')}</small></span>${categoryIcon(u)}</span>`;
  const avatar = u => u.avatar_url ? `<img class="board-avatar" src="${esc(u.avatar_url)}" alt="">` : `<span class="board-avatar board-avatar-fallback">${esc((u.display_name || '?').slice(0, 1))}</span>`;
  const categoryIcon = u => u.category_icon ? `<img class="board-category-icon" src="${esc(u.category_icon)}" alt="${esc(u.category_name || '')}" title="${esc(u.category_name || '')}">` : '';
  const recordName = u => u.id ? `<button type="button" class="player-record-link" data-player-record="${u.id}" title="${esc(u.display_name)} · ${t('viewPlayerRecords')}">${esc(u.display_name)}</button>` : esc(u.display_name);
  let fieldID = 0;
  const field = (label, html, full = false) => {
    const id = 'field-' + (++fieldID);
    // 保留单选项自身的标签及日期、小时等独立名称，仅为未命名控件补充字段标签。
    const template = document.createElement('template'); template.innerHTML = html;
    template.content.querySelectorAll('input,select,textarea').forEach(control => {
      if (!control.closest('label') && !control.hasAttribute('aria-label') && !control.hasAttribute('aria-labelledby')) control.setAttribute('aria-labelledby', id);
    });
    return `<div class="field${full ? ' full' : ''}" role="group" aria-labelledby="${id}"><span class="field-label" id="${id}">${t(label)}</span>${template.innerHTML}</div>`;
  };
  const input = (name, value = '', type = 'text', attrs = '') => `<input name="${name}" type="${type}" value="${esc(value)}" ${attrs}>`;
  const options = (users, chosen = 0, empty = '') => (empty ? `<option value="0">${t(empty)}</option>` : '') + users.map(u => `<option value="${u.id}"${Number(chosen) === Number(u.id) ? ' selected' : ''}>${esc(u.display_name)}</option>`).join('');
  const localTime = () => { const date = new Date(); return new Date(date - date.getTimezoneOffset() * 60000).toISOString().slice(0, 13); };
  const displayTime = value => { const text = String(value || ''); return text ? `${text.slice(0, 10)} · ${text.slice(11, 13)} ${t('hour')}` : '—'; };
  function eventTimeInput(value = localTime()) { return `<div class="event-time-input"><input type="date" name="event_date" value="${esc(value.slice(0, 10))}" required aria-label="${t('date')}"><select name="event_hour" required aria-label="${t('hour')}">${Array.from({ length: 24 }, (_, hour) => `<option value="${String(hour).padStart(2, '0')}"${hour === Number(value.slice(11, 13)) ? ' selected' : ''}>${String(hour).padStart(2, '0')} ${t('hour')}</option>`).join('')}</select></div>`; }
  const eventTimeValue = form => `${form.get('event_date')}T${form.get('event_hour')}`;
  function toast(message) { const box = $('#toast'); box.textContent = message; box.classList.remove('hidden'); clearTimeout(state.toastTimer); state.toastTimer = setTimeout(() => box.classList.add('hidden'), 4000); }
  async function api(url, options = {}) { const headers = { ...options.headers }; if (options.body !== undefined && !(options.body instanceof FormData)) headers['Content-Type'] = 'application/json'; const res = await fetch(url, { ...options, headers }); const data = await res.json().catch(() => ({})); if (!res.ok) { if (res.status === 401 && state.user) { sessionEnded(); throw new Error(t('sessionReplaced')); } throw new Error(data.error || t('requestFailed')); } return data; }
  function sessionEnded() {
    state.user = null; state.editor = null; state.operations = false;
    if (location.pathname === '/workspace' || location.pathname.endsWith('/setting')) go('/login');
    else renderRoute().catch(error => toast(error.message));
    toast(t('sessionReplaced'));
  }
  const post = (url, body = {}, method = 'POST') => api(url, { method, body: JSON.stringify(body) });
  function action(fn) {
    return async event => {
      const source = event?.currentTarget;
      if (source?.dataset.busy) return;
      const form = source?.tagName === 'FORM' ? source : null;
      const controls = form ? $$('button[type="submit"],button:not([type])', form) : source?.tagName === 'BUTTON' ? [source] : [];
      const previous = controls.map(button => button.disabled);
      if (source) { source.dataset.busy = 'true'; source.setAttribute('aria-busy', 'true'); }
      controls.forEach(button => button.disabled = true);
      form?.querySelector('.form-error')?.remove();
      try { await fn(event); }
      catch (error) {
        if (error.name === 'AbortError') return;
        if (form?.isConnected) { const message = document.createElement('p'); message.className = 'form-error full'; message.setAttribute('role', 'alert'); message.textContent = error.message; form.prepend(message); message.scrollIntoView({ block: 'nearest', behavior: 'smooth' }); }
        toast(error.message);
      } finally {
        if (source) { delete source.dataset.busy; source.removeAttribute('aria-busy'); }
        controls.forEach((button, index) => button.disabled = previous[index]);
      }
    };
  }
  function picker() { return `<select class="language-picker" aria-label="Language">${Object.entries(langs).map(([key, name]) => `<option value="${key}"${state.language === key ? ' selected' : ''}>${name}</option>`).join('')}</select>`; }
  function closeStream() { state.stream?.close(); state.stream = null; }
  function pageURL(page) { return page === 'home' ? '/' : `/workspace?page=${page}`; }
  function go(url) { closeStream(); state.editor = null; state.ruleDraft = null; state.operations = false; history.pushState({}, '', url); renderRoute().catch(error => toast(error.message)); }
  // 语言只改变当前视图的文案，保留表单值和路由。
  const formKey = (form, index) => form.id || ['name', 'category', 'kofReserve'].map(key => form.dataset[key] ? `${key}:${form.dataset[key]}` : '').find(Boolean) || `form:${index}`;
  const fieldType = el => el.dataset.passwordField ? 'password' : el.type;
  function captureForms() {
    return $$('form').map((form, index) => ({
      key: formKey(form, index),
      matchRounds: form.id === 'archive-form' ? $$('[name="past_round"]', form).map(el => Number(el.value)) : null,
      fields: $$('input,select,textarea', form).map(el => ({ name: el.name, type: fieldType(el), value: el.value, checked: el.checked, selected: el.multiple ? [...el.selectedOptions].map(o => o.value) : null, files: el.type === 'file' ? el.files : null }))
    }));
  }
  function restoreForms(forms) {
    for (const data of forms) {
      const form = $$('form').find((form, index) => formKey(form, index) === data.key);
      if (!form) continue;
      if (data.matchRounds) { $('#past-matches').innerHTML = data.matchRounds.map(pastMatchRow).join(''); bindPastMatchRemoval(); }
      const used = new Set();
      for (const item of data.fields) {
        const el = $$('input,select,textarea', form).find(el => !used.has(el) && el.name === item.name && fieldType(el) === item.type);
        if (!el) continue;
        used.add(el);
        if (item.type === 'file') { try { el.files = item.files; if (el.files?.length) el.dispatchEvent(new Event('change')); } catch (_) {} }
        else if (item.selected) { for (const option of el.options) option.selected = item.selected.includes(option.value); }
        else { el.value = item.value; el.checked = item.checked; }
      }
    }
  }
  function bindCommon() {
    $$('[data-go]').forEach(el => el.addEventListener('click', event => { if (event.ctrlKey || event.metaKey || event.shiftKey) return; event.preventDefault(); go(el.dataset.go); }));
    $$('input[type="password"]').forEach(input => {
      input.dataset.passwordField = 'true';
      if (input.parentElement.classList.contains('password-input')) return;
      const wrapper = document.createElement('div'); wrapper.className = 'password-input'; input.before(wrapper); wrapper.append(input);
      const toggle = document.createElement('button'); toggle.type = 'button'; toggle.className = 'password-toggle'; toggle.textContent = t('showPassword'); toggle.setAttribute('aria-pressed', 'false'); wrapper.append(toggle);
      toggle.onclick = () => { const show = input.type === 'password'; input.type = show ? 'text' : 'password'; toggle.textContent = t(show ? 'hidePassword' : 'showPassword'); toggle.setAttribute('aria-pressed', String(show)); };
    });
    $$('.language-picker').forEach(el => el.addEventListener('change', action(async () => { const forms = captureForms(); if ($('#event-form') && $('[name=\"round1_mode\"]')) state.ruleDraft = readRulesDraft($('#event-form')); state.language = el.value; localStorage.setItem('tb_language', state.language); document.documentElement.lang = state.language; await renderRoute(true); restoreForms(forms); refreshSpecialCounts(); })));
    $('#logout')?.addEventListener('click', action(async () => { await post('/api/auth/logout'); state.user = null; go('/'); }));
  }
  function shell(content, page = 'home') {
    const admin = state.user?.role === 'superadmin';
    const nav = ['home', ...(admin ? ['createEvent', 'accounts', 'archive', 'siteSettings'] : []), ...(state.user ? ['settings'] : [])];
    const navIcons = { home: '◫', createEvent: '+', accounts: '♙', archive: '▤', siteSettings: '⚙', settings: '◎' };
    const brand = `<a class="brand" href="/" data-go="/"><span class="brand-mark">TB<span></span></span><span><strong>TEAM BATTLE</strong><small>${t('brandCaption')}</small></span></a>`;
    const sidebar = state.user ? `<aside class="sidebar">${brand}<span class="nav-caption">${t('workspace')}</span><nav class="nav" aria-label="${t('workspace')}">${nav.map(key => `<a href="${pageURL(key)}" class="${page === key ? 'active' : ''}" ${page === key ? 'aria-current="page"' : ''} data-go="${pageURL(key)}"><span class="nav-icon" aria-hidden="true">${navIcons[key]}</span><span>${t(key)}</span></a>`).join('')}</nav><div class="side-footer">${person(state.user)}<span class="account-role">${t(state.user.role)}</span></div></aside>` : '';
    app.innerHTML = `<div class="shell${state.user ? '' : ' public-shell'}">${sidebar}<main class="main"><header class="topbar">${state.user ? `<span class="breadcrumb">TEAM BATTLE <span>/</span> <b>${t(page)}</b></span>` : brand}<div class="top-actions">${picker()}${state.user ? `<button class="ghost" id="logout">${t('logout')}</button>` : `<a class="primary" href="/login" data-go="/login">${t('login')} <span aria-hidden="true">↗</span></a>`}</div></header><div class="page">${content}</div><footer class="site-footer">TEAM BATTLE <span>·</span> ${t('brandCaption')}</footer></main></div>`;
    bindCommon();
  }
  function eventTable(events) {
    if (!events.length) return '<div class="empty"><span class="empty-symbol" aria-hidden="true">◇</span><h3>' + t('noEvents') + '</h3><p>' + t('emptyHistoryHint') + '</p></div>';
    return '<div class="table-wrap"><table class="table event-table"><thead><tr><th>' + t('name') + '</th><th>' + t('time') + '</th><th>' + t('results') + '</th><th>' + t('status') + '</th><th><span class="sr-only">' + t('view') + '</span></th></tr></thead><tbody>' + events.map(e => '<tr><td><a class="event-name" href="/live/' + e.id + '" data-go="/live/' + e.id + '"><span class="event-emblem" aria-hidden="true">TB</span><span>' + esc(e.name) + '<small>' + t('players') + ' · ' + (e.player_count || 0) + '</small></span></a></td><td class="event-date">' + esc(displayTime(e.event_time)) + '</td><td><span class="event-mini-score"><small>A</small><b>' + (e.round_wins_a || 0) + '</b><i>:</i><b>' + (e.round_wins_b || 0) + '</b><small>B</small></span></td><td><span class="tag ' + (terminal(e) ? 'done' : 'live') + '">' + t(e.status) + '</span></td><td><a class="row-view" aria-label="' + esc(t('view') + ' · ' + e.name) + '" href="/live/' + e.id + '" data-go="/live/' + e.id + '">↗</a></td></tr>').join('') + '</tbody></table></div>';
  }
  function liveCards(events) {
    return '<div class="live-grid">' + events.map(e => '<a class="live-card" href="/live/' + e.id + '" data-go="/live/' + e.id + '"><div class="live-card-top"><span class="live-indicator">' + t('ongoing') + '</span><span class="tag">' + t(e.status) + '</span></div><h3>' + esc(e.name) + '</h3><p>' + esc(displayTime(e.event_time)) + '</p><div class="live-card-score"><span>' + t('teamA') + '</span><b>' + (e.round_wins_a || 0) + '<i>:</i>' + (e.round_wins_b || 0) + '</b><span>' + t('teamB') + '</span></div><div class="live-card-footer"><span>' + t('players') + ' · ' + (e.player_count || 0) + '</span><strong>' + t('enterLive') + ' <span aria-hidden="true">→</span></strong></div></a>').join('') + '</div>';
  }
  async function homePage() {
    const query = new URLSearchParams(location.search); state.page = Math.max(1, Number(query.get('p') || 1)); state.filters = { name: query.get('name') || '', from: query.get('from') || '', to: query.get('to') || '' };
    const search = new URLSearchParams({ ...state.filters, page: state.page, page_size: 10 });
    const [past, live, mine] = await Promise.all([api('/api/public/history?' + search), api('/api/public/events'), state.user ? api('/api/events') : Promise.resolve({ events: [] })]);
    const publicIDs = new Set(live.events.map(e => e.id)); const own = mine.events.filter(e => !terminal(e) && !publicIDs.has(e.id));
    shell('<div class="home-heading"><div><span class="eyebrow">TEAM BATTLE / ' + t('eventCenter') + '</span><h1>' + t('home') + '</h1><p class="subtitle">' + t('homeIntro') + '</p></div><div class="home-summary"><span><b>' + live.events.length + '</b>' + t('ongoing') + '</span><span><b>' + past.total + '</b>' + t('history') + '</span></div></div>' + (live.events.length ? '<section class="home-live section-gap"><div class="section-title"><h2><span class="status-dot"></span>' + t('ongoing') + '</h2><span>' + t('watchPublic') + '</span></div>' + liveCards(live.events) + '</section>' : '') + (own.length ? '<section class="panel section-gap"><div class="panel-head"><h2>' + t('myEvents') + '</h2><span class="count-badge">' + own.length + '</span></div>' + eventTable(own) + '</section>' : '') + '<section class="panel history-panel"><div class="panel-head"><div><span class="eyebrow">ARCHIVE</span><h2>' + t('history') + '</h2></div><span class="count-badge">' + t('total') + ' ' + past.total + '</span></div><form id="history-filter" class="filter-bar">' + field('name', input('name', state.filters.name, 'search', 'placeholder="' + t('searchEventHint') + '"')) + field('from', input('from', state.filters.from, 'date')) + field('to', input('to', state.filters.to, 'date')) + '<button class="primary">' + t('search') + '</button><button type="button" class="ghost" data-go="/">' + t('clear') + '</button></form>' + eventTable(past.events) + '<div class="pagination"><span>' + t('total') + ' ' + past.total + '</span><div><button class="outline" id="previous" ' + (state.page <= 1 ? 'disabled' : '') + '>' + t('previous') + '</button><span>' + state.page + ' <small>/ ' + Math.max(1, Math.ceil(past.total / 10)) + '</small></span><button class="outline" id="next" ' + (state.page * 10 >= past.total ? 'disabled' : '') + '>' + t('next') + '</button></div></div></section>');
    const filterURL = (page = 1, values = state.filters) => '/?' + new URLSearchParams({ ...values, p: page });
    $('#history-filter').addEventListener('submit', action(async event => { event.preventDefault(); const values = Object.fromEntries(new FormData(event.target)); if (values.from && values.to && values.from > values.to) throw Error(t('dateRangeError')); go(filterURL(1, values)); }));
    $('#previous').onclick = () => go(filterURL(state.page - 1)); $('#next').onclick = () => go(filterURL(state.page + 1));
  }
  function loginPage() {
    app.innerHTML = '<main class="login-shell"><section class="login-story"><a class="brand" href="/" data-go="/"><span class="brand-mark">TB<span></span></span><span><strong>TEAM BATTLE</strong><small>' + t('brandCaption') + '</small></span></a><div><span class="eyebrow">1V1 / KOF / ACE</span><h2>' + t('loginHeadline') + '</h2><p>' + t('homeIntro') + '</p><div class="login-art" aria-hidden="true"><span>A</span><b>VS</b><span>B</span></div></div><small>TEAM BATTLE</small></section><section class="login-main"><div class="login-language">' + picker() + '</div><div class="login-card"><span class="eyebrow">WELCOME BACK</span><h1>' + t('login') + '</h1><p class="subtitle">' + t('loginIntro') + '</p><form id="login-form" class="login-form">' + field('username', input('username', '', 'text', 'required autocomplete="username" placeholder="' + t('username') + '"')) + field('password', input('password', '', 'password', 'required autocomplete="current-password"')) + '<button class="primary accent">' + t('login') + ' <span aria-hidden="true">→</span></button></form><p class="help">' + t('persistentLoginHint') + '</p><a class="back-link" href="/" data-go="/">← ' + t('homeLink') + '</a></div></section></main>'; bindCommon();
    $('#login-form').onsubmit = action(async event => { event.preventDefault(); const form = new FormData(event.target); state.user = (await post('/api/auth/login', Object.fromEntries(form))).user; const target = new URLSearchParams(location.search).get('return'); go(target?.startsWith('/live/') ? target : '/'); });
  }
  function profileLimit(changedAt, limited) {
    const next = changedAt ? new Date(Date.parse(changedAt) + 7 * 24 * 60 * 60 * 1000) : null;
    return limited && next && next > new Date() ? next : null;
  }
  function profileFields(user, categories, managed = false, creating = false) {
    const limited = !managed && user.role !== 'superadmin';
    const nameNext = profileLimit(user.display_name_changed_at, limited), avatarNext = profileLimit(user.avatar_changed_at, limited);
    const editableLogin = creating || user.id === state.user.id;
    const dateText = date => new Intl.DateTimeFormat({ zh: 'zh-CN', en: 'en-GB', ko: 'ko-KR' }[state.language], { dateStyle: 'medium', timeStyle: 'short' }).format(date);
    const limitHint = (key, next) => limited ? '<p class="help">' + t(key) + (next ? ' ' + t('availableOn') + ' ' + esc(dateText(next)) : '') + '</p>' : '';
    const section = (title, contents) => `<fieldset class="profile-section full"><legend>${t(title)}</legend><div class="form-grid">${contents}</div></fieldset>`;
    const identity = field('avatar', `<div class="avatar-upload"><div id="avatar-preview">${user.avatar_url ? `<img class="account-avatar-preview" src="${esc(user.avatar_url)}" alt="">` : `<span class="account-avatar-empty">${t('noAvatar')}</span>`}</div><div><input type="file" name="avatar" accept="image/jpeg,image/png,image/gif,image/webp" aria-label="${t('avatar')}"${avatarNext ? ' disabled' : ''}><p class="help">${t('imageHint')}</p>${limitHint('avatarWeekly', avatarNext)}</div></div>`, true)
      + field('displayName', input('display_name', user.display_name || '', 'text', (creating ? '' : 'required') + (nameNext ? ' disabled' : '')) + limitHint('nameWeekly', nameNext), true);
    const credentials = field('username', input('username', user.username || '', 'text', 'required autocomplete="username"' + (editableLogin ? '' : ' readonly')) + (editableLogin ? '' : '<p class="help">' + t('usernameSelfOnly') + '</p>'), true)
      + field('newPassword', input('new_password', '', 'password', 'minlength="6" autocomplete="new-password"' + (creating ? ' required' : '')))
      + field('confirmPassword', input('new_password_confirm', '', 'password', 'autocomplete="new-password"' + (creating ? ' required' : '')))
      + `<p class="help full">${t('passwordHint')}</p>`;
    const category = field('category', `<div class="category-choices"><label><input type="radio" name="category_id" value="0"${!user.category_id ? ' checked' : ''}>${t('noCategory')}</label>${categories.map(c => `<label><input type="radio" name="category_id" value="${c.id}"${Number(c.id) === Number(user.category_id) ? ' checked' : ''}>${c.icon_url ? `<img src="${esc(c.icon_url)}" alt="">` : ''}${esc(c.name)}</label>`).join('')}</div>`, true)
      + (managed ? field('role', '<select name="role">' + ['user', 'superadmin'].map(role => '<option value="' + role + '"' + (role === (user.role || 'user') ? ' selected' : '') + '>' + t(role) + '</option>').join('') + '</select>') : '');
    return section('profileIdentity', identity) + section('profileCredentials', credentials) + section('profileCategory', category);
  }
  function bindProfileForm(form, endpoint, method, saved) {
    let previewURL;
    $('[name="avatar"]', form).onchange = event => {
      if (previewURL) URL.revokeObjectURL(previewURL);
      const file = event.target.files[0];
      if (file) {
        previewURL = URL.createObjectURL(file);
        $('#avatar-preview').innerHTML = '<img class="account-avatar-preview" src="' + previewURL + '" alt="">';
      }
    };
    form.onsubmit = action(async event => {
      event.preventDefault();
      const values = new FormData(form);
      if (values.get('new_password') !== values.get('new_password_confirm')) throw Error(t('passwordMismatch'));
      if (!values.get('avatar')?.size) values.delete('avatar');
      const button = $('button[type="submit"]', form);
      button.disabled = true;
      try { await saved(await api(endpoint, { method, body: values })); if (previewURL) URL.revokeObjectURL(previewURL); }
      finally { button.disabled = false; }
    });
  }
  async function accountPage() {
    const [catalog, current] = await Promise.all([api('/api/settings/catalog'), api('/api/auth/me')]);
    state.user = current.user;
    shell(`<div class="page-head"><h1>${t('settings')}</h1><button type="button" class="outline" data-player-record="${state.user.id}">${t('myRecords')}</button></div><section class="panel"><form id="account-form" class="panel-body form-grid">${profileFields(state.user, catalog.categories)}<button type="submit" class="primary accent">${t('save')}</button></form></section>`, 'settings');
    bindProfileForm($('#account-form'), '/api/auth/profile', 'PATCH', async result => { state.user = result.user; await accountPage(); toast(t('saved')); });
  }
  async function usersPage() {
    const [directory, catalog] = await Promise.all([api('/api/users'), api('/api/settings/catalog')]);
    const users = directory.users, edit = new URLSearchParams(location.search).get('edit');
    const creating = edit === 'new', user = creating ? {} : users.find(u => String(u.id) === edit);
    if (creating || user) {
      shell(`<div class="page-head"><div><h1>${t('accounts')}</h1><p class="subtitle">${t(creating ? 'createUser' : 'editAccount')}</p></div><button class="outline" data-go="${pageURL('accounts')}">${t('backAccounts')}</button></div><section class="panel"><form id="user-form" class="form-grid panel-body">${profileFields(user, catalog.categories, true, creating)}<button type="submit" class="primary accent">${t(creating ? 'createUser' : 'save')}</button></form></section>`, 'accounts');
      bindProfileForm($('#user-form'), '/api/users' + (creating ? '' : '/' + user.id), creating ? 'POST' : 'PATCH', async result => {
        if (result.user?.id === state.user.id) state.user = result.user;
        toast(t(creating ? 'accountCreated' : 'saved'));
        go(state.user.role === 'superadmin' ? pageURL('accounts') : '/');
      });
      return;
    }
    shell(`<div class="page-head"><h1>${t('accounts')}</h1><button class="primary accent" data-go="${pageURL('accounts')}&edit=new">${t('createUser')}</button></div><section class="panel"><div class="panel-body"><input id="account-search" aria-label="${t('searchPlayers')}" placeholder="${t('searchPlayers')}"><p class="help">${t('accountDeleteHint')}</p></div><div class="table-wrap"><table class="table"><thead><tr><th>${t('account')}</th><th>${t('role')}</th><th>${t('category')}</th><th>${t('actions')}</th></tr></thead><tbody>${users.map(u => `<tr data-account-row="${esc((u.username + ' ' + u.display_name).toLowerCase())}"><td>${person(u)}</td><td>${t(u.role)}</td><td>${esc(u.category_name || '—')}</td><td><div class="account-actions"><button class="link" data-go="${pageURL('accounts')}&edit=${u.id}">${t('editAccount')}</button><button class="link danger" data-delete-user="${u.id}"${u.can_delete ? '' : ' disabled'} title="${u.can_delete ? '' : t(u.id === state.user.id ? 'cannotDeleteSelf' : 'cannotDeletePlayed')}">${t('delete')}</button></div></td></tr>`).join('')}</tbody></table></div></section>`, 'accounts');
    $('#account-search').oninput = event => { const text = event.target.value.trim().toLowerCase(); $$('[data-account-row]').forEach(row => { row.hidden = !row.dataset.accountRow.includes(text); }); };
    $$('[data-delete-user]').forEach(button => button.onclick = action(async () => { if (!confirm(t('deleteAccountConfirm'))) return; await api('/api/users/' + button.dataset.deleteUser, { method: 'DELETE' }); await usersPage(); toast(t('saved')); }));
  }

  async function siteSettingsPage() {
    const data = await api('/api/site-settings');
    shell(`<div class="page-head"><h1>${t('siteSettings')}</h1></div><div class="settings-columns"><section class="panel"><div class="panel-head"><h2>${t('names')}</h2></div><div class="panel-body"><form id="name-form" class="inline-form">${input('name', '', 'text', 'required')}<button class="primary">${t('add')}</button></form>${data.event_names.map(n => `<form class="inline-form item-form" data-name="${n.id}">${input('name', n.name, 'text', 'required')}<button class="outline">${t('save')}</button><button type="button" class="link danger" data-delete-name="${n.id}">${t('delete')}</button></form>`).join('')}</div></section><section class="panel"><div class="panel-head"><h2>${t('categories')}</h2></div><div class="panel-body"><form id="category-form" class="field">${field('categoryName', input('name', '', 'text', 'required'))}${field('icon', '<input type="file" name="icon" accept="image/jpeg,image/png,image/gif,image/webp" required>')}<p class="help">${t('imageHint')}</p><button class="primary">${t('add')}</button></form>${data.categories.map(c => `<form class="category-edit item-form" data-category="${c.id}">${c.icon_url ? `<img class="category-icon-preview" src="${esc(c.icon_url)}" alt="">` : `<span>${t('noIcon')}</span>`}${input('name', c.name, 'text', 'required')}<input type="file" name="icon" accept="image/jpeg,image/png,image/gif,image/webp"><button class="outline">${t('save')}</button><button type="button" class="link danger" data-delete-category="${c.id}">${t('delete')}</button></form>`).join('')}</div></section></div>`, 'siteSettings');
    $('#name-form').onsubmit = action(async event => { event.preventDefault(); await post('/api/site-settings/event-names', Object.fromEntries(new FormData(event.target))); await siteSettingsPage(); });
    $('#category-form').onsubmit = action(async event => { event.preventDefault(); await api('/api/site-settings/categories', { method: 'POST', body: new FormData(event.target) }); await siteSettingsPage(); });
    $$('[data-name]').forEach(form => form.onsubmit = action(async event => { event.preventDefault(); await post('/api/site-settings/event-names/' + form.dataset.name, Object.fromEntries(new FormData(form)), 'PATCH'); await siteSettingsPage(); }));
    $$('[data-category]').forEach(form => form.onsubmit = action(async event => { event.preventDefault(); await api('/api/site-settings/categories/' + form.dataset.category, { method: 'PATCH', body: new FormData(form) }); await siteSettingsPage(); }));
    for (const kind of ['name', 'category']) $$(`[data-delete-${kind}]`).forEach(button => button.onclick = action(async () => { if (!confirm(t('deleteConfirm'))) return; const id = button.getAttribute('data-delete-' + kind); await api('/api/site-settings/' + (kind === 'name' ? 'event-names' : 'categories') + '/' + id, { method: 'DELETE' }); await siteSettingsPage(); }));
  }

  function rosterMarkup(users, assignments) {
    return `<section class="panel section-gap editor-section" id="event-roster"><div class="panel-head"><h2>${t('players')}</h2><span id="roster-count"></span></div><div class="panel-body"><p class="help">${t('rosterHint')}</p><div class="roster-tools"><input id="player-search" aria-label="${t('searchPlayers')}" placeholder="${t('searchPlayers')}"><button type="button" class="outline" id="all-random">${t('allRandom')}</button></div><div class="roster-summary" id="roster-summary"></div><div class="roster-preview" id="roster-preview"></div><div class="roster-cards">${users.map(u => `<article class="roster-card" data-player-card="${u.id}" data-search="${esc((u.username + ' ' + u.display_name + ' ' + (u.category_name || '')).toLowerCase())}">${person(u)}<div class="team-switch">${['', 'R', 'A', 'B'].map(value => `<label><input type="radio" name="pick_${u.id}" value="${value}"${(assignments[u.id] || '') === value ? ' checked' : ''}><span>${value === '' ? t('notSelected') : value === 'R' ? t('randomTeam') : t(value === 'A' ? 'teamA' : 'teamB')}</span></label>`).join('')}</div></article>`).join('') || `<div class="empty">${t('emptyPlayers')}</div>`}</div></div></section>`;
  }
  function rosterValue(form) { const formData = new FormData(form); const ids = [], fixed = {}; for (const user of state.users) { const pick = formData.get('pick_' + user.id); if (pick) { ids.push(user.id); if (pick !== 'R') fixed[user.id] = pick; } } return { player_ids: ids, fixed_assignments: fixed }; }
  function bindRoster(form) {
    const update = () => { const roster = rosterValue(form); state.roster = Object.fromEntries(roster.player_ids.map(id => [id, roster.fixed_assignments[id] || 'R'])); const count = team => Object.values(state.roster).filter(v => v === team).length; $('#roster-count').textContent = `${t('selected')} ${roster.player_ids.length} / 12`; $('#roster-summary').innerHTML = ['A', 'B', 'R'].map(team => `<span class="roster-total ${team.toLowerCase()}">${t(team === 'A' ? 'teamA' : team === 'B' ? 'teamB' : 'unassigned')} <b>${count(team)}</b></span>`).join(''); $$('[data-player-card]').forEach(card => { card.dataset.team = state.roster[card.dataset.playerCard] || ''; });
      $$('[data-special-candidates]', form).forEach(container => {
        const selected = $$('input:checked', container).map(el => Number(el.value));
        container.innerHTML = specialCandidatesMarkup(selected);
      });
      $('#roster-count').classList.toggle('danger', roster.player_ids.length > 12);
      $('#roster-preview').innerHTML = ['A', 'B', 'R'].map(team => {
        const players = state.users.filter(user => state.roster[user.id] === team);
        return `<section class="roster-preview-team ${team.toLowerCase()}"><h3>${t(team === 'A' ? 'teamA' : team === 'B' ? 'teamB' : 'unassigned')}</h3><div>${players.map(user => `<span>${categoryIcon(user)}${esc(user.display_name)}</span>`).join('') || `<small>${t('noneSelected')}</small>`}</div></section>`;
      }).join('');
      refreshSpecialCounts();
    };
    $$('[name^="pick_"]', form).forEach(el => el.addEventListener('change', update));
    $('#player-search').oninput = event => { const text = event.target.value.trim().toLowerCase(); $$('[data-player-card]').forEach(card => card.hidden = !card.dataset.search.includes(text)); };
    $('#all-random').onclick = () => { for (const user of state.users) if (state.roster[user.id]) $(`[name="pick_${user.id}"][value="R"]`, form).checked = true; update(); };
    update();
  }
  const defaultRules = () => ({ round1_mode: 'manual', kof_starter: 'random', no_repeat: true, all_first: true, random_draw_seconds: 3, ace_draw_seconds: 5, ace_most_wins: 20, ace_most_losses: 20, ace_specials: [] });
  function specialCandidatesMarkup(selected = []) {
    const available = state.users.filter(u => state.roster[u.id]);
    if (!available.length) return `<p class="help">${t('selectPlayersFirst')}</p>`;
    return ['A', 'B', 'R'].map(team => {
      const users = available.filter(u => state.roster[u.id] === team);
      return users.length ? `<div class="candidate-team"><h4>${t(team === 'A' ? 'teamA' : team === 'B' ? 'teamB' : 'unassigned')}</h4><div class="candidate-grid">${users.map(u => `<label class="candidate-card"><input type="checkbox" name="special_ids" value="${u.id}"${selected.includes(u.id) ? ' checked' : ''}><span class="candidate-person">${person(u)}</span></label>`).join('')}</div></div>` : '';
    }).join('');
  }
  function specialMarkup(group = { player_ids: [], probability: 0 }) {
    return `<section class="special-rule"><div class="special-rule-head"><h3 data-special-title></h3><span data-special-count></span><button type="button" class="link danger" data-remove-special>${t('delete')}</button></div><div class="special-probability">${field('probability', input('special_probability', group.probability, 'number', 'min="0" max="100" required'))}<p class="help">${t('candidateHint')}</p></div><div class="special-candidates" data-special-candidates>${specialCandidatesMarkup(group.player_ids)}</div></section>`;
  }
  function refreshSpecialCounts() {
    $$('.special-rule').forEach((row, index) => {
      $('[data-special-title]', row).textContent = `${t('special')} ${index + 1}`;
      $('[data-special-count]', row).textContent = `${t('selected')} ${$$('input[type=checkbox]:checked', row).length}`;
    });
  }
  function rulesMarkup(e = {}) {
    const rules = { ...defaultRules(), ...e.rules };
    return `<section class="panel section-gap editor-section" id="event-round1"><div class="panel-head"><h2>${t('round1')}</h2></div><div class="panel-body form-grid">${field('round1Target', input('round1_target', e.round1_target || 4, 'number', 'min="1" max="99" required'))}${field('round1Mode', `<select name="round1_mode"><option value="manual"${rules.round1_mode === 'manual' ? ' selected' : ''}>${t('manual')}</option><option value="random"${rules.round1_mode === 'random' ? ' selected' : ''}>${t('random')}</option></select>`)}<label class="check"><input name="no_repeat" type="checkbox"${rules.no_repeat ? ' checked' : ''}>${t('noRepeat')}</label><label class="check"><input name="all_first" type="checkbox"${rules.all_first ? ' checked' : ''}>${t('allFirst')}</label></div></section><section class="panel section-gap editor-section" id="event-round2"><div class="panel-head"><h2>${t('round2')}</h2></div><div class="panel-body form-grid">${field('round2Target', input('round2_target', e.round2_target || 5, 'number', 'min="1" max="99" required'))}${field('kofStarter', `<div class="mode-choices">${['random', 'captain'].map(mode => `<label><input type="radio" name="kof_starter" value="${mode}"${rules.kof_starter === mode ? ' checked' : ''}><span>${t(mode === 'random' ? 'randomStarter' : 'captainStarter')}</span></label>`).join('')}</div>`)}<p class="help full">${t('kofHint')}</p></div></section><section class="panel section-gap editor-section" id="event-ace"><div class="panel-head"><h2>${t('aceRules')}</h2><span id="fallback-probability"></span></div><div class="panel-body"><div class="form-grid">${field('mostWins', input('ace_most_wins', rules.ace_most_wins, 'number', 'min="0" max="100" required'))}${field('mostLosses', input('ace_most_losses', rules.ace_most_losses, 'number', 'min="0" max="100" required'))}</div><p class="help">${t('aceHint')}</p><div id="special-rules">${rules.ace_specials.map(specialMarkup).join('')}</div><button type="button" class="outline" id="add-special">${t('addSpecial')}</button></div></section>`;
  }
  function readRulesDraft(form) {
    const f = new FormData(form);
    return { round1_mode: f.get('round1_mode'), kof_starter: f.get('kof_starter'), no_repeat: f.has('no_repeat'), all_first: f.has('all_first'), random_draw_seconds: Number(f.get('random_draw_seconds')), ace_draw_seconds: Number(f.get('ace_draw_seconds')), ace_most_wins: Number(f.get('ace_most_wins')), ace_most_losses: Number(f.get('ace_most_losses')), ace_specials: $$('.special-rule', form).map(row => ({ probability: Number($('[name="special_probability"]', row).value), player_ids: $$('input[type=checkbox]:checked', row).map(el => Number(el.value)) })) };
  }
  function rulesValue(form) {
    const rules = readRulesDraft(form);
    const probabilities = [rules.ace_most_wins, rules.ace_most_losses, ...rules.ace_specials.map(group => group.probability)];
    if (probabilities.some(value => !Number.isInteger(value) || value < 0) || probabilities.reduce((sum, value) => sum + value, 0) > 100) throw Error(t('probabilityError'));
    if (rules.ace_specials.some(group => !group.player_ids.length)) throw Error(t('specialEmpty'));
    return rules;
  }
  function bindRules(form) {
    const refresh = () => {
      const rules = readRulesDraft(form);
      const total = rules.ace_most_wins + rules.ace_most_losses + rules.ace_specials.reduce((sum, group) => sum + group.probability, 0);
      $('#fallback-probability').textContent = `${t('fallback')} ${100 - total}%`;
      $('#fallback-probability').classList.toggle('danger', total > 100);
      refreshSpecialCounts();
    };
    $('#add-special').onclick = () => { $('#special-rules').insertAdjacentHTML('beforeend', specialMarkup()); refresh(); };
    $('#special-rules').addEventListener('click', event => { const remove = event.target.closest('[data-remove-special]'); if (remove) { remove.closest('.special-rule').remove(); refresh(); } });
    form.addEventListener('input', refresh);
    form.addEventListener('change', refresh);
    refresh();
  }
  function drawTimingMarkup(e, editableTiming, separateSave) {
    const rules = { ...defaultRules(), ...e.rules };
    return `<section class="panel section-gap draw-timing-panel editor-section" id="event-timing"><div class="panel-head"><h2>${t('drawTiming')}</h2><span>${t('drawTimingMutable')}</span></div><div class="panel-body form-grid">${field('randomDrawSeconds', input('random_draw_seconds', rules.random_draw_seconds, 'number', 'min="0" step="1" required' + (editableTiming ? '' : ' disabled')))}${field('aceDrawSeconds', input('ace_draw_seconds', rules.ace_draw_seconds, 'number', 'min="0" step="1" required' + (editableTiming ? '' : ' disabled')))}<p class="help full">${t('drawTimingHint')}</p>${separateSave ? '<button type="button" class="primary accent" id="save-draw-settings">' + t('save') + '</button>' : ''}</div></section>`;
  }
  function editorJumps() {
    const sections = [['basics', 'basicInfo'], ['roster', 'players'], ['round1', 'round1'], ['round2', 'round2'], ['ace', 'ace'], ['timing', 'drawTiming']];
    return `<nav class="editor-jumps" aria-label="${t('eventSections')}">${sections.map(([id, title], index) => `<a href="#event-${id}"><span>${index + 1}</span>${t(title)}</a>`).join('')}</nav>`;
  }
  async function eventEditor(mode = 'create', preserve = false) {
    const source = mode === 'create' ? {} : state.event;
    if (mode !== 'create' && !manages(source)) { state.editor = null; return boardPage(); }
    const readonly = mode !== 'create' && !canEditSettings(source);
    const timingEditable = mode === 'create' || (mode !== 'roster' && manages(source) && !terminal(source));
    const e = preserve && state.ruleDraft && !readonly ? { ...source, rules: state.ruleDraft } : source;
    const [users, catalog] = await Promise.all([
      mode === 'create' ? api('/api/users') : terminal(e) ? Promise.resolve({ users: e.teams.flatMap(team => team.players) }) : api('/api/events/' + e.id + '/eligible-users'),
      state.user.role === 'superadmin' ? api('/api/site-settings') : Promise.resolve({ event_names: [] })
    ]);
    state.users = users.users.filter(u => u.role !== 'superadmin');
    if (!preserve || readonly) state.roster = Object.fromEntries((e.teams || []).flatMap((team, i) => team.players.filter(p => p.active !== false).map(p => [p.id, i ? 'B' : 'A'])));
    const managerDefault = mode === 'create' || state.users.some(u => u.id === e.manager_id) ? 0 : e.manager_id;
    const managerField = state.user.role === 'superadmin' ? field('manager', '<select name="manager_id"><option value="' + managerDefault + '">' + t('managerDefault') + '</option>' + options(state.users, e.manager_id) + '</select>') : '';
    const basic = mode !== 'roster' ? '<section class="panel editor-section" id="event-basics"><div class="panel-head"><h2>' + t('basicInfo') + '</h2><span>' + t('requiredBasics') + '</span></div><div class="panel-body form-grid">' + field('name', '<input name="name" list="event-names" value="' + esc(e.name || '') + '" required><datalist id="event-names">' + catalog.event_names.map(n => '<option value="' + esc(n.name) + '">').join('') + '</datalist>', true) + field('time', eventTimeInput(e.event_time || localTime())) + managerField + field('description', '<textarea name="description" rows="2">' + esc(e.description || '') + '</textarea>', true) + '</div></section>' : '';
    const content = '<div class="page-head"><div><h1>' + t(mode === 'create' ? 'createEvent' : mode === 'roster' ? 'editRoster' : 'eventSettings') + '</h1><p class="subtitle">' + t(readonly ? 'settingsLocked' : mode === 'create' ? 'requiredHint' : 'settingsResetHint') + '</p></div>' + (mode === 'create' ? '' : '<button class="outline" id="back-board">' + t('close') + '</button>') + '</div>' + (mode === 'roster' ? '' : editorJumps()) + '<form id="event-form"><fieldset class="event-settings-fields">' + basic + rosterMarkup(state.users, state.roster) + (mode === 'roster' ? '' : rulesMarkup(e)) + '</fieldset>' + (mode === 'roster' ? '' : drawTimingMarkup(e, timingEditable, readonly && timingEditable)) + (readonly ? '' : '<div class="form-footer"><button class="primary accent">' + t(mode === 'create' ? 'createEvent' : 'save') + '</button></div>') + '</form>';
    if (mode === 'create') shell(content, 'createEvent'); else {
      app.innerHTML = '<main class="event-editor-page"><div class="editor-language">' + picker() + '</div>' + content + '</main>';
      bindCommon(); $('#back-board').onclick = () => go(hasRoster(state.event) ? '/live/' + state.event.id : '/');
    }
    const form = $('#event-form');
    bindRoster(form);
    if (mode !== 'roster') bindRules(form);
    $('.event-settings-fields', form).disabled = readonly;
    form.onsubmit = action(async event => {
      event.preventDefault();
      if (mode !== 'create' && !canEditSettings(state.event)) throw Error(t('settingsLocked'));
      const f = new FormData(form);
      const body = mode === 'roster' ? rosterValue(form) : {
        name: f.get('name'), event_time: eventTimeValue(f), description: f.get('description'),
        ...(state.user.role === 'superadmin' ? { manager_id: Number(f.get('manager_id') || 0) } : {}),
        round1_target: Number(f.get('round1_target')), round2_target: Number(f.get('round2_target')),
        rules: rulesValue(form), ...rosterValue(form)
      };
      if (mode === 'create') { const created = await post('/api/events', body); toast(t('eventCreated')); go('/live/' + created.id); }
      else {
        await post('/api/events/' + e.id + (mode === 'roster' ? '/players' : '/settings'), body, mode === 'roster' ? 'POST' : 'PATCH');
        state.editor = null;
        if (mode === 'settings') history.replaceState({}, '', '/live/' + e.id);
        if (!hasRoster(state.event)) { await openEvent(e.id); } else { await refreshEvent(); }
        toast(t('saved'));
      }
    });
    $('#save-draw-settings')?.addEventListener('click', action(async () => {
      const body = { random_draw_seconds: Number($('[name="random_draw_seconds"]', form).value), ace_draw_seconds: Number($('[name="ace_draw_seconds"]', form).value) };
      await post('/api/events/' + e.id + '/draw-settings', body, 'PATCH');
      state.event.rules = { ...state.event.rules, ...body };
      toast(t('saved'));
    }));
  }

  function playerHasWin(e, playerID, stage) { return e.matches.some(m => m.stage === stage && m.status === 'finished' && ((m.player_a_id === playerID && m.who_win === 'a') || (m.player_b_id === playerID && m.who_win === 'b'))); }
  function playerHasLoss(e, playerID, stage) { return e.matches.some(m => m.stage === stage && m.status === 'finished' && ((m.player_a_id === playerID && m.who_win === 'b') || (m.player_b_id === playerID && m.who_win === 'a'))); }
  function waitingKOFMatch(e) {
    if (e.status !== 'round2' || e.matches.some(m => m.stage === 'round2' && m.status === 'pending')) return null;
    const last = e.matches.filter(m => m.stage === 'round2').at(-1);
    return { stage: 'round2', status: 'waiting', seq: (last?.seq || 0) + 1, who_win: '0', player_a_id: last?.who_win === 'a' ? last.player_a_id : 0, player_b_id: last?.who_win === 'b' ? last.player_b_id : 0, player_a_name: last?.who_win === 'a' ? last.player_a_name : t('toConfirm'), player_b_name: last?.who_win === 'b' ? last.player_b_name : t('toConfirm') };
  }
  function matchMarkup(m, people) { const a = people.find(p => p.id === m.player_a_id) || { id: m.player_a_id, display_name: m.player_a_name }, b = people.find(p => p.id === m.player_b_id) || { id: m.player_b_id, display_name: m.player_b_name }; const decided = m.who_win === 'a' || m.who_win === 'b'; const resultLabel = (side) => !decided ? '—' : m.who_win === side ? t('win') : t('lose'); const resultClass = (side) => !decided ? 'board-result-muted' : m.who_win === side ? 'board-result-win' : 'board-result-lose'; return `<div class="board-match ${m.status === 'pending' ? 'is-pending' : ''}"><span class="board-match-no">#${m.seq}</span><div class="board-player ${m.who_win === 'a' ? 'is-winner' : m.who_win === 'b' ? 'is-loser' : ''}"><span>${recordName(a)}</span>${categoryIcon(a)}</div><span class="board-match-result"><b class="${resultClass('a')}">${resultLabel('a')}</b><span class="board-vs">VS</span><b class="${resultClass('b')}">${resultLabel('b')}</b></span><div class="board-player board-player-right ${m.who_win === 'b' ? 'is-winner' : m.who_win === 'a' ? 'is-loser' : ''}">${categoryIcon(b)}<span>${recordName(b)}</span></div></div>`; }
  function currentPlayerMarkup(e, player, stage, side) { const hasWin = playerHasWin(e, player.id, stage); const hasLoss = stage === 'round2' && playerHasLoss(e, player.id, stage); const stateClass = hasWin ? 'is-record-winner' : 'is-record-new'; return `<div class="current-player ${side === 'b' ? 'current-player-right' : ''} ${stateClass} ${hasLoss ? 'is-lost' : ''}">${side === 'a' ? avatar(player) : ''}<div><strong>${recordName(player)}</strong>${categoryIcon(player)}</div>${side === 'b' ? avatar(player) : ''}</div>`; }
  function currentMatchMarkup(e, match, people) { const a = people.find(p => p.id === match.player_a_id) || { display_name: match.player_a_name }, b = people.find(p => p.id === match.player_b_id) || { display_name: match.player_b_name }; return `<section class="board-current-match"><div class="board-current-head"><span class="board-round-no">${t(match.stage)}</span><span>${t('currentMatch')}</span></div><div class="board-current-fighters">${currentPlayerMarkup(e, a, match.stage, 'a')}<div class="board-current-vs">VS</div>${currentPlayerMarkup(e, b, match.stage, 'b')}</div></section>`; }
  function roundMarkup(round, matches, people) { return `<section class="board-round"><div class="board-round-head"><div><span class="board-round-no">${t(round.stage)}</span><h2>${round.score_a} : ${round.score_b}</h2></div><span class="board-target">${t('target')} ${round.target_score}</span></div><div class="board-matches">${matches.map(m => matchMarkup(m, people)).join('') || `<div class="board-empty">${t('noMatches')}</div>`}</div></section>`; }
  function rulesSummary(e) {
    const rules = { ...defaultRules(), ...e.rules };
    const fallback = 100 - rules.ace_most_wins - rules.ace_most_losses - rules.ace_specials.reduce((sum, group) => sum + group.probability, 0);
    const specialGroups = rules.ace_specials.map((group, index) => `<div class="summary-special"><div class="summary-special-head"><strong>${t('special')} ${index + 1}</strong><b>${group.probability}%</b></div>${e.teams.map((team, side) => {
      const candidates = team.players.filter(p => group.player_ids.includes(p.id));
      return `<div class="summary-team"><span>${t(side ? 'teamB' : 'teamA')}</span><div>${candidates.length ? candidates.map(p => `<span class="summary-candidate">${avatar(p)}<span>${recordName(p)}</span>${categoryIcon(p)}</span>`).join('') : `<small>${t('noCandidateFallback')}</small>`}</div></div>`;
    }).join('')}</div>`).join('');
    return `<section class="board-rules"><div class="board-roster-head"><span>${t('eventSettings')}</span></div><div class="board-rule-block"><h3>${t('round1')}</h3><dl><div><dt>${t('target')}</dt><dd>${e.round1_target}</dd></div><div><dt>${t('round1Mode')}</dt><dd>${t(rules.round1_mode === 'random' ? 'random' : 'manual')}</dd></div><div><dt>${t('noRepeat')}</dt><dd>${t(rules.no_repeat ? 'enabled' : 'disabled')}</dd></div><div><dt>${t('allFirst')}</dt><dd>${t(rules.all_first ? 'enabled' : 'disabled')}</dd></div><div><dt>${t('randomDrawSeconds')}</dt><dd>${rules.random_draw_seconds}${t('seconds')}</dd></div></dl></div><div class="board-rule-block"><h3>${t('round2')}</h3><dl><div><dt>${t('target')}</dt><dd>${e.round2_target}</dd></div><div><dt>${t('kofStarter')}</dt><dd>${t(rules.kof_starter === 'captain' ? 'captainStarter' : 'randomStarter')}</dd></div><div><dt>${t('randomDrawSeconds')}</dt><dd>${rules.random_draw_seconds}${t('seconds')}</dd></div></dl></div><div class="board-rule-block"><h3>${t('aceRules')}</h3><dl><div><dt>${t('aceDrawSeconds')}</dt><dd>${rules.ace_draw_seconds}${t('seconds')}</dd></div><div><dt>${t('mostWinsRule')}</dt><dd>${rules.ace_most_wins}%</dd></div><div><dt>${t('mostLossesRule')}</dt><dd>${rules.ace_most_losses}%</dd></div><div><dt>${t('random')}</dt><dd>${fallback}%</dd></div></dl>${specialGroups}</div>${e.description ? `<div class="board-rule-block"><h3>${t('description')}</h3><p class="summary-description">${esc(e.description)}</p></div>` : ''}</section>`;
  }
  function boardContent(e) {
    const teams = e.teams, people = teams.flatMap(team => team.players), rounds = e.rounds;
    const panel = stage => {
      const matches = e.matches.filter(match => match.stage === stage);
      const waiting = stage === 'round2' ? waitingKOFMatch(e) : null;
      if (waiting) matches.push(waiting);
      return roundMarkup(rounds.find(round => round.stage === stage), matches, people);
    };
    const pending = !terminal(e) ? (e.matches.find(match => match.status === 'pending' && match.stage === e.status) || waitingKOFMatch(e)) : null;
    // 历史赛事继续保留KOF胜负颜色，避免赛事结束后阵容退回无标记状态。
    const recordStage = e.matches.some(match => match.stage === 'round2') ? 'round2' : 'round1';
    const roster = teams.map(team => `<section class="board-roster-team"><h3>${esc(teamName(e, team))} · ${team.players.length}</h3>${team.players.map(player => {
      const lost = playerHasLoss(e, player.id, 'round2');
      const recordClass = playerHasWin(e, player.id, recordStage) ? 'is-record-winner' : 'is-record-new';
      return `<div class="board-roster-player ${lost ? 'is-lost' : ''} ${recordClass}">${avatar(player)}<span class="board-roster-name">${recordName(player)}</span>${categoryIcon(player)}${player.is_captain ? `<b class="board-captain">${t('captain')}</b>` : ''}</div>`;
    }).join('')}</section>`).join('');
    return `<div class="board-top"><div><span class="board-live-mark">TEAM BATTLE</span><h1>${esc(e.name)}</h1><p>${esc(displayTime(e.event_time))}</p></div><div class="board-heading-actions"><span class="board-status">${t(e.status)}</span>${manages(e) && !terminal(e) ? `<button class="board-settings-button" id="board-settings">${t('eventSettings')}</button>` : ''}</div></div><div class="board-score"><div><span>${t('teamA')}</span><strong>${rounds.filter(round => round.winner_team_id === teams[0].id).length}</strong></div><b>VS</b><div><span>${t('teamB')}</span><strong>${rounds.filter(round => round.winner_team_id === teams[1].id).length}</strong></div></div>${pending ? currentMatchMarkup(e, pending, people) : ''}<div class="board-records"><div class="board-columns"><div class="board-left">${panel('round1')}${panel('round2')}</div><div class="board-right">${panel('ace')}<section class="board-roster"><div class="board-roster-head"><span>${t('players')}</span><b>${people.length}</b></div>${roster}</section></div></div>${rulesSummary(e)}</div>`;
  }
  function boardVariant(e) {
    if (!state.user || terminal(e)) return 'spectator';
    if (manages(e)) return 'admin';
    const team = e.teams.find(team => team.players.some(p => p.id === state.user.id && p.active !== false));
    if (!team) return 'spectator';
    const captain = team.players.find(p => p.is_captain && p.active !== false);
    return captain ? (captain.id === state.user.id ? 'captain' : 'spectator') : 'member';
  }
  function canOperate(e) { return boardVariant(e) !== 'spectator'; }
  function bindBoardSettings() {
    $('#board-settings')?.addEventListener('click', () => go('/live/' + state.event.id + '/setting'));
  }
  function boardPage() {
    const e = state.event;
    if (!canOperate(e)) state.operations = false;
    app.innerHTML = `<main class="broadcast-board" data-variant="${boardVariant(e)}"><div id="board-content">${boardContent(e)}</div><div id="event-operations" class="event-operations${state.operations ? '' : ' hidden'}"></div><details class="board-menu"><summary aria-label="${t('eventControls')}">☰</summary><div>${picker()}<button data-go="/">${t('homeLink')}</button>${canOperate(e) ? `<button id="toggle-operations">${t('eventControls')}</button>` : ''}${!state.user ? `<button data-go="/login?return=/live/${e.id}">${t('openLogin')}</button>` : ''}<span id="connection-state" class="help">${terminal(e) ? t('readOnly') : t(state.connection || 'reconnecting')}</span></div></details></main>`;
    bindCommon(); bindBoardSettings(); $('#toggle-operations')?.addEventListener('click', () => { state.operations = !state.operations; $('#event-operations').classList.toggle('hidden', !state.operations); if (state.operations) { renderOperations(); $('#event-operations').scrollIntoView({ behavior: 'smooth' }); } });
    if (state.operations) renderOperations();
  }
  async function refreshEvent() { state.event = await api('/api/public/events/' + state.event.id); if (!state.editor) boardPage(); }
  async function eventAction(path, body = {}, method = 'POST') { await post('/api/events/' + state.event.id + path, body, method); await refreshEvent(); }
  async function renderKOFSelection(e, manager) {
    const target = $('#kof-selection');
    if (!target) return;
    const choices = await api('/api/events/' + e.id + '/kof/options');
    if (!target.isConnected || state.event.id !== e.id) return;
    if (!choices.match_id && e.rules.kof_starter !== 'captain') return;
    const snapshot = JSON.stringify(choices);
    if (target.dataset.options === snapshot) return;
    target.dataset.options = snapshot;
    target.innerHTML = `<section class="panel section-gap"><div class="panel-head"><h2>${t(choices.match_id ? 'nextSlot' : 'kofStarter')}</h2></div><div class="panel-body"><p class="help">${t('reservationHint')}</p><div class="team-lineups">${choices.teams.map(choice => {
      const team = e.teams.find(team => team.id === choice.team_id);
      const selected = team.players.find(p => p.id === choice.player_id);
      const candidates = team.players.filter(p => choice.candidate_ids.includes(p.id));
      const captain = choice.captain_id === state.user.id, selfService = !choice.captain_id;
      const canSend = manager || captain || selfService;
      const ownEligible = choice.candidate_ids.includes(state.user.id);
      const status = selected ? `<div class="starter-selection">${person(selected)}<span class="tag done">${t('slotOccupied')}</span></div>` : `<p class="help">${t(candidates.length ? 'slotEmpty' : 'noEligible')}</p>`;
      const form = canSend && candidates.length && (manager || !selected) ? `<form data-kof-reserve="${team.id}" class="inline-form">${manager || captain ? `<select name="player_id">${options(candidates)}</select><button class="primary">${t('sendNext')}</button>` : ownEligible ? `<input type="hidden" name="player_id" value="${state.user.id}"><button class="primary">${t('signUpNext')}</button>` : `<p class="help">${t('cannotSignUp')}</p>`}</form>` : '';
      return `<section><h3>${esc(teamName(e, team))}</h3>${status}${!canSend ? `<p class="help">${t('captainWillSend')}</p>` : ''}${form}</section>`;
    }).join('')}</div></div></section>`;
    $$('[data-kof-reserve]', target).forEach(form => form.onsubmit = action(async event => {
      event.preventDefault();
      const button = $('button', form); button.disabled = true;
      try {
        await eventAction(choices.match_id ? '/kof/next-player' : '/kof/starter', { match_id: choices.match_id || 0, team_id: Number(form.dataset.kofReserve), player_id: Number(new FormData(form).get('player_id')) });
        toast(t('reservationSaved'));
      } catch (error) { await renderKOFSelection(state.event, manages(state.event)); throw error; }
      finally { button.disabled = false; }
    }));
  }
  function renderOperations() {
    const e = state.event, target = $('#event-operations'); if (!target) return;
    if (!canOperate(e)) { target.innerHTML = ''; return; }
    const manager = manages(e), people = e.teams.flatMap(team => team.players), myTeam = e.teams.find(team => team.players.some(p => p.id === state.user.id));
    let content = `<div class="operations-heading"><div><span class="eyebrow">${t('eventControls')}</span><h2>${t(e.status)}</h2></div><button class="outline" id="close-operations">${t('close')}</button></div>`;
    if (myTeam && !myTeam.players.some(p => p.is_captain && p.active !== false)) {
      const candidates = myTeam.players.filter(player => player.active !== false);
      content += `<section class="panel"><div class="panel-body"><p class="help">${t('voteThreshold').replace('{count}', Math.ceil(candidates.length / 2))}</p><form id="vote-form" class="inline-form">${field('vote', `<select name="target_id">${candidates.map(player => `<option value="${player.id}">${esc(player.display_name)} · ${e.votes.find(vote => vote.player_id === player.id)?.count || 0} ${t('votes')}</option>`).join('')}</select>`)}<button class="primary">${t('vote')}</button></form></div></section>`;
    }
    if (manager && e.teams.every(team => team.players.length)) content += `<section class="panel section-gap"><form id="captains-form" class="panel-body form-grid">${e.teams.map((team, i) => { return field(i ? 'teamB' : 'teamA', `<select name="team_${i ? 'b' : 'a'}_player_id"><option value="0">${t('noCaptain')}</option>${team.players.map(p => `<option value="${p.id}"${p.is_captain ? ' selected' : ''}>${esc(p.display_name)} · ${e.votes.find(v => v.player_id === p.id)?.count || 0} ${t('votes')}</option>`).join('')}</select>`); }).join('')}<button class="primary">${t('confirmCaptains')}</button></form></section>`;
    if (preMatch(e) && (manager || boardVariant(e) === 'captain')) {
      content += `<section class="panel section-gap"><div class="panel-head"><h2>${t('lineup')}</h2><span>${t(e.rules.round1_mode === 'random' ? 'random' : 'manual')}</span></div><div class="panel-body"><p class="help">${t('lineupHint')}</p><div class="team-lineups">${e.teams.filter(team => manager || team.players.some(p => p.id === state.user.id && p.is_captain)).map(team => `<form id="lineup-${team.id}" data-lineup="${team.id}" class="field"><h3>${esc(teamName(e, team))}</h3>${Array.from({ length: Math.min(team.players.length, Math.floor(people.length / 2)) }, (_, i) => field('matchNo', `<span>${i + 1}</span><select name="player_${i}"${e.rules.round1_mode === 'random' ? ' disabled' : ''}>${options(team.players, (e.lineups.find(l => l.team_id === team.id && l.position === i + 1)?.player_id || 0), 'randomSlot')}</select>`)).join('')}<button class="outline">${t('saveLineup')}</button></form>`).join('')}</div>${manager ? `<div class="form-footer"><button class="outline" id="random-lineup">${t('randomLineup')}</button><button class="primary accent" id="start-event">${t('start')}</button></div>` : ''}</div></section>`;
    }
    const pending = e.matches.find(m => m.status === 'pending' && m.stage === e.status);
    if (manager && pending) content += `<section class="panel section-gap"><div class="panel-head"><h2>${t(e.status)} · #${pending.seq}</h2></div><div class="panel-body"><div class="fighters"><div class="fighter">${person(people.find(p => p.id === pending.player_a_id))}</div><span class="vs">VS</span><div class="fighter">${person(people.find(p => p.id === pending.player_b_id))}</div></div><div class="result-actions"><button class="win-a" data-winner="${pending.team_a_id}" data-match="${pending.id}">${esc(pending.player_a_name)} · ${t('win')}</button><button class="win-b" data-winner="${pending.team_b_id}" data-match="${pending.id}">${esc(pending.player_b_name)} · ${t('win')}</button></div></div></section>`;
    if (e.status === 'round2') content += '<div id="kof-selection"></div>';
    if (manager && e.status === 'ace' && pending) content += `<section class="panel section-gap"><form id="ace-form" class="panel-body form-grid">${field('teamA', `<select name="player_a_id">${options(e.teams[0].players, pending.player_a_id)}</select>`)}${field('teamB', `<select name="player_b_id">${options(e.teams[1].players, pending.player_b_id)}</select>`)}<button class="outline">${t('overrideAce')}</button></form></section>`;
    if (manager && e.status === 'final_confirm') content += `<section class="panel section-gap"><div class="panel-body"><p>${t('confirmEndHint')}</p><button class="primary accent" id="confirm-end">${t('confirmEnd')}</button></div></section>`;
    if (manager && e.matches.some(m => m.status === 'finished')) content += `<section class="panel section-gap"><div class="panel-head"><h2>${t('correct')}</h2></div><form id="correct-form" class="panel-body form-grid"><p class="notice full">${t('correctHint')}</p>${field('matchNo', `<select name="match_id">${e.matches.filter(m => m.status === 'finished').map(m => `<option value="${m.id}">${t(m.stage)} #${m.seq} · ${esc(m.player_a_name)} / ${esc(m.player_b_name)}</option>`).join('')}</select>`)}${field('winner', `<select name="winner_team_id">${e.teams.map(team => `<option value="${team.id}">${esc(teamName(e, team))}</option>`).join('')}</select>`)}<button class="outline">${t('correct')}</button></form></section>`;
    target.innerHTML = content;
    $('#close-operations')?.addEventListener('click', () => { state.operations = false; $('#event-operations').classList.add('hidden'); });
    if (e.status === 'round2') renderKOFSelection(e, manager).catch(error => toast(error.message));
    $('#vote-form')?.addEventListener('submit', action(async event => { event.preventDefault(); await eventAction('/captain-votes', { target_id: Number(new FormData(event.target).get('target_id')) }); toast(t('voteSaved')); }));
    $('#captains-form')?.addEventListener('submit', action(async event => { event.preventDefault(); const f = new FormData(event.target); await eventAction('/captains/confirm', { team_a_player_id: Number(f.get('team_a_player_id')), team_b_player_id: Number(f.get('team_b_player_id')) }); }));
    $$('[data-lineup]').forEach(form => form.onsubmit = action(async event => { event.preventDefault(); const ids = [...new FormData(form).values()].map(Number); const manual = []; let randomSeen = false; for (const id of ids) { if (!id) randomSeen = true; else { if (randomSeen) throw Error(t('lineupHint')); manual.push(id); } } await eventAction('/lineup/submit', { team_id: Number(form.dataset.lineup), player_ids: manual }); toast(t('saved')); }));
    $('#random-lineup')?.addEventListener('click', action(() => eventAction('/lineup/generate'))); $('#start-event')?.addEventListener('click', action(() => eventAction('/start')));
    $$('[data-winner]').forEach(button => button.onclick = action(() => eventAction('/matches/' + button.dataset.match + '/result', { winner_team_id: Number(button.dataset.winner) })));
    $('#ace-form')?.addEventListener('submit', action(async event => { event.preventDefault(); const f = new FormData(event.target); await eventAction('/ace/confirm', { player_a_id: Number(f.get('player_a_id')), player_b_id: Number(f.get('player_b_id')) }); }));
    $('#correct-form')?.addEventListener('submit', action(async event => { event.preventDefault(); if (!confirm(t('correctHint'))) return; const f = new FormData(event.target); await eventAction('/matches/' + f.get('match_id') + '/result', { winner_team_id: Number(f.get('winner_team_id')) }, 'PATCH'); }));
    $('#confirm-end')?.addEventListener('click', action(async () => { if (confirm(t('confirmEndHint'))) await eventAction('/confirm'); }));
  }
  async function openEvent(id, preserve = false, setting = false) {
    if (state.event?.id !== Number(id)) state.event = null;
    state.event = await api('/api/public/events/' + id);
    if (!hasRoster(state.event) && !setting) {
      if (state.user && manages(state.event)) {
        history.replaceState({}, '', '/live/' + id + '/setting');
        state.editor = 'settings';
        return eventEditor('settings', preserve);
      }
      throw new Error(t('eventRosterRequired'));
    }
    if (setting) state.editor = 'settings';
    if (state.editor) await eventEditor(state.editor, preserve); else boardPage();
    if (terminal(state.event)) { closeStream(); return; }
    if (state.stream) return;
    const stream = new EventSource('/api/public/events/' + id + '/stream'); state.stream = stream;
    stream.onopen = () => { state.connection = 'connected'; if ($('#connection-state')) $('#connection-state').textContent = t('connected'); };
    stream.onerror = () => { state.connection = 'reconnecting'; if ($('#connection-state')) $('#connection-state').textContent = t('reconnecting'); };
    stream.onmessage = message => {
      const data = JSON.parse(message.data); const changed = JSON.stringify(state.event) !== JSON.stringify(data); const variantChanged = boardVariant(state.event) !== boardVariant(data); const settingsLocked = canEditSettings(state.event) && !canEditSettings(data); state.event = data;
      if (state.editor && settingsLocked) { eventEditor(state.editor).catch(error => toast(error.message)); }
      if (variantChanged && !state.editor) { boardPage(); }
      if (changed && !state.editor && $('#board-content')) { $('#board-content').innerHTML = boardContent(data); bindBoardSettings(); if (state.operations) { const forms = captureForms(); renderOperations(); restoreForms(forms); } }
      if (terminal(data)) { closeStream(); if (!state.editor) boardPage(); }
    };
  }

  function pastMatchRow(round = 1) { return `<div class="past-match"><select name="past_round" aria-label="${t('stage')}">${[1, 2, 3].map(r => `<option value="${r}"${r === round ? ' selected' : ''}>${t(r === 1 ? 'round1' : r === 2 ? 'round2' : 'ace')}</option>`).join('')}</select><select name="past_a" aria-label="${t('teamA')}" required>${options(state.users.filter(u => state.roster[u.id] === 'A'))}</select><span>VS</span><select name="past_b" aria-label="${t('teamB')}" required>${options(state.users.filter(u => state.roster[u.id] === 'B'))}</select><select name="past_winner" aria-label="${t('winner')}"><option value="a">${t('teamA')} · ${t('win')}</option><option value="b">${t('teamB')} · ${t('win')}</option></select><button type="button" class="link danger" data-remove-match>${t('delete')}</button></div>`; }
  function bindPastMatchRemoval() { $$('[data-remove-match]').forEach(button => button.onclick = () => button.closest('.past-match').remove()); }
  async function archivePage(preserve = false) {
    state.users = (await api('/api/users')).users.filter(u => u.role !== 'superadmin'); if (!preserve) state.roster = {};
    shell(`<div class="page-head"><div><h1>${t('archive')}</h1><p class="subtitle">${t('pastHint')}</p></div></div><form id="archive-form"><section class="panel"><div class="panel-body form-grid">${field('name', input('name', '', 'text', 'required'), true)}${field('time', eventTimeInput())}</div></section>${rosterMarkup(state.users, state.roster)}<section class="panel section-gap"><div class="panel-head"><h2>${t('results')}</h2></div><div class="panel-body"><div id="past-matches">${pastMatchRow()}</div><button type="button" class="outline" id="add-match">${t('addMatch')}</button></div></section><div class="form-footer"><button class="primary accent">${t('pastSave')}</button></div></form>`, 'archive');
    bindRoster($('#archive-form'));
    // 过去赛事直接指定两队，避免历史对局被重新随机分配。
    $$('[name^="pick_"][value="R"]').forEach(el => el.closest('label').remove()); $('#all-random').remove();
    bindPastMatchRemoval();
    $('#add-match').onclick = () => { $('#past-matches').insertAdjacentHTML('beforeend', pastMatchRow()); bindPastMatchRemoval(); };
    $$('[name^="pick_"]').forEach(el => el.addEventListener('change', () => { for (const side of ['a', 'b']) $$(`[name="past_${side}"]`).forEach(select => { const chosen = select.value; select.innerHTML = options(state.users.filter(u => state.roster[u.id] === side.toUpperCase()), chosen); }); }));
    $('#archive-form').onsubmit = action(async event => { event.preventDefault(); const f = new FormData(event.target); const matches = $$('.past-match').map(row => ({ round: Number($('[name="past_round"]', row).value), a_id: Number($('[name="past_a"]', row).value), b_id: Number($('[name="past_b"]', row).value), who_win: $('[name="past_winner"]', row).value })); if (!confirm(t('confirmEndHint'))) return; const result = await post('/api/events/archive', { name: f.get('name'), event_time: eventTimeValue(f), ...rosterValue(event.target), matches }); go('/live/' + result.id); });
  }
  async function renderRoute(preserve = false) {
    document.documentElement.lang = state.language;
    const path = location.pathname;
    if (path.startsWith('/live/')) {
      const parts = path.split('/').filter(Boolean);
      return openEvent(parts[1], preserve, parts[2] === 'setting');
    }
    closeStream(); state.event = null;
    if (path === '/login') return loginPage();
    const page = new URLSearchParams(location.search).get('page') || 'home';
    if (page === 'home') return homePage();
    if (!state.user) return go('/login');
    if (page === 'settings') return accountPage();
    if (state.user.role !== 'superadmin') return go('/');
    if (page === 'createEvent' || page === 'events') return eventEditor('create', preserve);
    if (['accounts', 'createUser', 'users', 'passwords'].includes(page)) return usersPage();
    if (page === 'siteSettings' || page === 'site-settings') return siteSettingsPage();
    if (page === 'archive') return archivePage(preserve);
    return homePage();
  }
  window.addEventListener('popstate', () => { closeStream(); state.editor = null; state.operations = false; renderRoute().catch(error => toast(error.message)); });
  // 公开SSE保持一致，登录是否已在其他设备被替换通过独立鉴权检查确认。
  let checkingSession = false;
  async function checkSession() {
    if (!state.user || document.hidden || checkingSession) return;
    checkingSession = true;
    try { await api('/api/auth/me'); } catch (_) {} finally { checkingSession = false; }
  }
  setInterval(checkSession, 10000);
  window.addEventListener('focus', checkSession);
  document.addEventListener('visibilitychange', checkSession);
  // 预报名不进入公开推送，操作面板单独刷新本队报名状态。
  let pollingKOF = false;
  setInterval(async () => {
    if (pollingKOF || document.hidden || state.editor || !state.operations || state.event?.status !== 'round2' || !$('#kof-selection')) return;
    pollingKOF = true;
    try { await renderKOFSelection(state.event, manages(state.event)); } catch (_) {} finally { pollingKOF = false; }
  }, 3000);
  window.addEventListener('beforeunload', closeStream);
  (async () => { try { state.user = (await api('/api/auth/me')).user; } catch (_) {} await renderRoute(); })().catch(error => { app.innerHTML = `<main class="page"><p>${esc(error.message)}</p><button data-go="/">${t('homeLink')}</button></main>`; bindCommon(); });
})();
