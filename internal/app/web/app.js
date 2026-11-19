(() => {
  'use strict';
  const app = document.getElementById('app');
  const state = { user: null, language: localStorage.getItem('tb_language') || 'zh', boardTransparency: readBoardTransparency(), event: null, stream: null, editor: null, roster: {}, rosterOrder: [], users: [], page: 1, filters: {}, connection: '', drawTimer: null, drawToken: 0, draw: null };
  const langs = { zh: '中文', en: 'English', ko: '한국어' };
  const pagePaths = { home: '/', createEvent: '/tournaments/new', accounts: '/accounts', tournamentManagement: '/tournaments', archive: '/tournaments/archive', siteSettings: '/site-settings', backgrounds: '/backgrounds', settings: '/account' };
  const pageForPath = (path = location.pathname) => Object.keys(pagePaths).find(page => pagePaths[page] === path) || (/^\/accounts\/(new|[1-9]\d*)$/.test(path) ? 'accounts' : null);
  const t = key => window.TB_I18N[state.language]?.[key] || window.TB_I18N.zh[key] || key;
  const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  const $ = (s, root = document) => root.querySelector(s);
  const $$ = (s, root = document) => [...root.querySelectorAll(s)];
  const terminal = e => ['completed', 'cancelled'].includes(e.status);
  const hasRoster = e => e?.teams?.length === 2 && e.teams.every(team => team.players.some(player => player.active !== false));
  const canWatch = e => e?.public_ready === true;
  const hasGrouping = e => (e?.teams || []).some(team => team.players?.some(player => player.active !== false));
  const preMatch = e => ['draft', 'captain_vote', 'lineup_confirm'].includes(e.status);
  const manages = e => state.user && (state.user.role === 'superadmin' || Number(state.user.id) === Number(e.manager_id));
  const editable = e => manages(e) && !terminal(e);
  const noScore = e => !(e.matches || []).some(m => m.status === 'finished');
  const canEditSettings = e => editable(e) && preMatch(e) && noScore(e);
  const lineupTeams = e => e.teams.filter(team => manages(e) || team.players.some(player => player.id === state.user?.id && player.active !== false && player.is_captain));
  const canArrangeLineup = e => hasRoster(e) && preMatch(e) && e.rules.round1_mode === 'manual' && !e.teams.every(team => team.lineup_submitted) && lineupTeams(e).length > 0;
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
  function startTimeInput(value = localTime()) { return `<div class="event-time-input"><input type="text" name="start_time" data-date-picker="hour" value="${esc(value.slice(0, 10) + ' ' + value.slice(11, 13) + ':00')}" required autocomplete="off" aria-label="${t('time')}"></div>`; }
  const startTimeValue = form => String(form.get('start_time') || '').replace(' ', 'T');
  function toast(message) { const box = $('#toast'); box.textContent = message; box.classList.remove('hidden'); clearTimeout(state.toastTimer); state.toastTimer = setTimeout(() => box.classList.add('hidden'), 4000); }
  async function api(url, options = {}) { const headers = { ...options.headers }; if (options.body !== undefined && !(options.body instanceof FormData)) headers['Content-Type'] = 'application/json'; const res = await fetch(url, { ...options, headers }); const data = await res.json().catch(() => ({})); if (!res.ok) { if (res.status === 401 && state.user) { sessionEnded(); throw new Error(t('sessionReplaced')); } const errorKey = Object.keys(window.TB_I18N.zh).find(key => window.TB_I18N.zh[key] === data.error); throw new Error(errorKey ? t(errorKey) : data.error || t('requestFailed')); } return data; }
  function sessionEnded() {
    state.user = null; state.editor = null;
    if ((pageForPath() && pageForPath() !== 'home') || location.pathname.endsWith('/setting') || location.pathname.endsWith('/lineup')) go('/login?return=' + encodeURIComponent(location.pathname + location.search));
    else renderRoute().catch(error => toast(error.message));
    toast(t('sessionReplaced'));
  }
  const post = (url, body = {}, method = 'POST') => api(url, { method, body: JSON.stringify(body) });
  const confirmAction = message => window.TBConfirm.ask(message);
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
  function closeStream() { state.stream?.close(); state.stream = null; if (state.drawTimer) clearInterval(state.drawTimer); state.drawTimer = null; state.drawToken += 1; state.draw = null; state.setupDraw = null; document.querySelectorAll('.setup-flying-player').forEach(node => node.remove()); }
  function pageURL(page) { return pagePaths[page] || '/'; }
  function loginTarget() {
    try {
      const target = new URL(new URLSearchParams(location.search).get('return') || '/', location.origin);
      // 登录只返回本站已知页面，防止外部地址或伪造路径成为跳转目标。
      return target.origin === location.origin && (pageForPath(target.pathname) || /^\/live\/[1-9]\d*(\/(setting|lineup))?$/.test(target.pathname)) ? target.pathname + target.search + target.hash : '/';
    } catch (_) { return '/'; }
  }
  function go(url) { window.TBConfirm.cancel(); closeStream(); state.editor = null; state.ruleDraft = null; history.pushState({}, '', url); renderRoute().catch(error => toast(error.message)); }
  // 语言只改变当前视图的文案，保留表单值和路由。
  const formKey = (form, index) => form.id || ['name', 'category', 'kofReserve'].map(key => form.dataset[key] ? `${key}:${form.dataset[key]}` : '').find(Boolean) || `form:${index}`;
  const fieldType = el => el.dataset.passwordField ? 'password' : el.type;
  function captureForms() {
    return $$('form').map((form, index) => ({
      key: formKey(form, index),
      fields: $$('input,select,textarea', form).map(el => ({ name: el.name || el.dataset.formKey || '', type: fieldType(el), value: el.value, checked: el.checked, selected: el.multiple ? [...el.selectedOptions].map(o => o.value) : null, files: el.type === 'file' ? el.files : null }))
    }));
  }
  function restoreForms(forms) {
    for (const data of forms) {
      const form = $$('form').find((form, index) => formKey(form, index) === data.key);
      if (!form) continue;
      const used = new Set();
      for (const item of data.fields) {
        const el = $$('input,select,textarea', form).find(el => !used.has(el) && (el.name || el.dataset.formKey || '') === item.name && fieldType(el) === item.type);
        if (!el) continue;
        used.add(el);
        if (item.type === 'file') { try { el.files = item.files; if (el.files?.length) el.dispatchEvent(new Event('change')); } catch (_) {} }
        else if (item.selected) { for (const option of el.options) option.selected = item.selected.includes(option.value); }
        else { el.value = item.value; el.checked = item.checked; }
      }
      form.dispatchEvent(new Event('formrestored'));
    }
  }
  function bindCommon() {
    window.TBSearchSelect.bindAll(app, t);
    window.TBDatePicker.bindAll(app, state.language, t);
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
    const nav = ['home', ...(admin ? ['createEvent', 'tournamentManagement', 'accounts', 'archive', 'siteSettings', 'backgrounds'] : []), ...(state.user ? ['settings'] : [])];
    const navPaths = {
      home: '<path d="m3 10 9-7 9 7v10a1 1 0 0 1-1 1h-5v-7H9v7H4a1 1 0 0 1-1-1Z"/>',
      createEvent: '<rect x="4" y="4" width="16" height="16" rx="3"/><path d="M12 8v8M8 12h8"/>',
      tournamentManagement: '<rect x="3" y="4" width="18" height="17" rx="2"/><path d="M8 2v4m8-4v4M3 10h18m-13 4h3m3 0h3m-9 3h3"/>',
      accounts: '<circle cx="9" cy="8" r="3"/><path d="M3 21v-3a6 6 0 0 1 12 0v3m2-16a3 3 0 0 1 0 6m2 10v-3a6 6 0 0 0-2-4"/>',
      archive: '<path d="M15 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8Z"/><path d="M14 3v6h6m-12 4h8m-8 4h5"/>',
      siteSettings: '<path d="M4 7h16M4 17h16"/><circle cx="9" cy="7" r="3" fill="#15263d"/><circle cx="15" cy="17" r="3" fill="#15263d"/>',
      backgrounds: '<rect x="3" y="3" width="18" height="18" rx="3"/><circle cx="8" cy="8" r="2"/><path d="m3 17 5-5 4 4 4-6 5 7"/>',
      settings: '<circle cx="12" cy="8" r="4"/><path d="M4 21v-2a8 8 0 0 1 16 0v2"/>'
    };
    const navIcons = Object.fromEntries(Object.entries(navPaths).map(([key, path]) => [key, `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">${path}</svg>`]));
    const brand = `<a class="brand" href="/" data-go="/"><span class="brand-mark">TB<span></span></span><span><strong>TEAM BATTLE</strong><small>${t('brandCaption')}</small></span></a>`;
    const sidebar = state.user ? `<aside class="sidebar">${brand}<span class="nav-caption">${t('workspace')}</span><nav class="nav" aria-label="${t('workspace')}">${nav.map(key => `<a href="${pageURL(key)}" class="${page === key ? 'active' : ''}" ${page === key ? 'aria-current="page"' : ''} data-go="${pageURL(key)}"><span class="nav-icon" aria-hidden="true">${navIcons[key]}</span><span>${t(key)}</span></a>`).join('')}</nav><div class="side-footer">${person(state.user)}<span class="account-role">${t(state.user.role)}</span></div></aside>` : '';
    app.innerHTML = `<div class="shell${state.user ? '' : ' public-shell'}">${sidebar}<main class="main"><header class="topbar">${state.user ? `<span class="breadcrumb">TEAM BATTLE <span>/</span> <b>${t(page)}</b></span>` : brand}<div class="top-actions">${picker()}${state.user ? `<button class="ghost" id="logout">${t('logout')}</button>` : `<a class="primary" href="/login" data-go="/login">${t('login')} <span aria-hidden="true">↗</span></a>`}</div></header><div class="page">${content}</div><footer class="site-footer">TEAM BATTLE <span>·</span> ${t('brandCaption')}</footer></main></div>`;
    bindCommon();
    // 手机导航保持横向滚动，新页面自动露出当前标签，不滚动正文。
    const navigation = $('.sidebar .nav'), activeTab = $('.sidebar .nav [aria-current="page"]');
    if (navigation && activeTab && navigation.scrollWidth > navigation.clientWidth) navigation.scrollLeft = activeTab.offsetLeft - navigation.offsetLeft - 12;
  }
  const deletedBadge = e => e.is_deleted ? `<span class="tournament-deleted-badge">${t('deletedTournament')}</span>` : '';
  function eventTable(events, management = false) {
    if (!events.length) return '<div class="empty"><span class="empty-symbol" aria-hidden="true">◇</span><h3>' + t('noEvents') + '</h3><p>' + t('emptyHistoryHint') + '</p></div>';
    return `<div class="table-wrap"><table class="table event-table"><thead><tr><th>${t('name')}</th><th>${t('time')}</th><th>${t('results')}</th>${management ? `<th>${t('status')}</th>` : ''}<th><span class="sr-only">${t(management ? 'actions' : 'view')}</span></th></tr></thead><tbody>${events.map(e => `<tr data-tournament-row="${e.id}" class="${e.is_deleted ? 'is-deleted' : ''}"><td><a class="event-name" href="/live/${e.id}" data-go="/live/${e.id}"><span class="event-emblem" aria-hidden="true">TB</span><span>${esc(e.name)}${deletedBadge(e)}<small>${t('players')} · ${e.player_count || 0}</small></span></a></td><td class="event-date">${esc(displayTime(e.start_time))}</td><td><span class="event-mini-score"><small>A</small><b>${e.round_wins_a || 0}</b><i>:</i><b>${e.round_wins_b || 0}</b><small>B</small></span></td>${management ? `<td>${t(e.status)}</td><td class="tournament-row-action">${terminal(e) ? `<button type="button" class="outline ${e.is_deleted ? '' : 'danger'}" data-tournament-action="${e.is_deleted ? 'restore' : 'delete'}" data-tournament-id="${e.id}">${t(e.is_deleted ? 'restore' : 'delete')}</button>` : '—'}</td>` : `<td><a class="row-view" aria-label="${esc(t('view') + ' · ' + e.name)}" href="/live/${e.id}" data-go="/live/${e.id}">↗</a></td>`}</tr>`).join('')}</tbody></table></div>`;
  }
  async function tournamentManagementPage() {
    const { tournaments } = await api('/api/tournaments');
    window.TBTournamentManagement.render({ tournaments, t, shell, eventTable, action, confirmAction, api, toast, reload: tournamentManagementPage });
  }
  function liveCards(events) {
    return '<div class="live-grid">' + events.map(e => {
      const available = e.public_ready || manages(e), tag = available ? 'a' : 'div';
      const path = e.public_ready ? `/live/${e.id}` : `/live/${e.id}/setting`;
      const entry = e.is_deleted ? 'view' : e.public_ready ? 'enterLive' : 'eventSettings';
      return `<${tag} class="live-card"${available ? ` href="${path}" data-go="${path}"` : ''}><h3>${esc(e.name)}${deletedBadge(e)}</h3><p>${esc(displayTime(e.start_time))}</p><div class="live-card-score"><span>${t('teamA')}</span><b>${e.round_wins_a || 0}<i>:</i>${e.round_wins_b || 0}</b><span>${t('teamB')}</span></div><div class="live-card-footer"><span>${t('players')} · ${e.player_count || 0}</span><strong>${t(entry)}${available ? ' <span aria-hidden="true">→</span>' : ''}</strong></div></${tag}>`;
    }).join('') + '</div>';
  }
  async function homePage() {
    const query = new URLSearchParams(location.search); state.page = Math.max(1, Number(query.get('p') || 1)); state.filters = { name: query.get('name') || '', player_id: query.get('player_id') || '', from: query.get('from') || '', to: query.get('to') || '', include_deleted: query.get('include_deleted') === '1' ? '1' : '' };
    const search = new URLSearchParams({ ...state.filters, page: state.page, page_size: 10 });
    const [past, live] = await Promise.all([api('/api/public/history?' + search), api('/api/public/tournaments?include_deleted=' + state.filters.include_deleted)]);
    const playerOptions = '<option value="">' + t('allPlayers') + '</option>' + (past.players || []).map(player => '<option value="' + player.id + '"' + (String(player.id) === String(state.filters.player_id || '') ? ' selected' : '') + '>' + esc(player.display_name) + (player.category_name ? ' · ' + esc(player.category_name) : '') + '</option>').join('');
    shell('<div class="home-heading"><div><span class="eyebrow">TEAM BATTLE / ' + t('eventCenter') + '</span><h1>' + t('home') + '</h1><p class="subtitle">' + t('homeIntro') + '</p></div><div class="home-summary"><span><b>' + live.tournaments.length + '</b>' + t('ongoing') + '</span><span><b>' + past.total + '</b>' + t('history') + '</span></div></div>' + (live.tournaments.length ? '<section class="home-live section-gap"><div class="section-title"><h2><span class="status-dot"></span>' + t('ongoing') + '</h2><span>' + t('watchPublic') + '</span></div>' + liveCards(live.tournaments) + '</section>' : '') + '<section class="panel history-panel"><div class="panel-head"><div><span class="eyebrow">ARCHIVE</span><h2>' + t('history') + '</h2></div><span class="count-badge">' + t('total') + ' ' + past.total + '</span></div><label class="show-deleted-control"><input type="checkbox" id="include-deleted"' + (state.filters.include_deleted ? ' checked' : '') + '>' + t('showDeletedTournaments') + '</label><form id="history-filter" class="filter-bar">' + field('name', input('name', state.filters.name, 'search', 'placeholder="' + t('searchEventHint') + '"')) + field('player', '<select name="player_id" data-searchable>' + playerOptions + '</select>') + field('from', input('from', state.filters.from, 'text', 'data-date-picker="date" autocomplete="off" placeholder="YYYY-MM-DD"')) + field('to', input('to', state.filters.to, 'text', 'data-date-picker="date" autocomplete="off" placeholder="YYYY-MM-DD"')) + '<button class="primary">' + t('search') + '</button><button type="button" class="ghost" data-go="/">' + t('clear') + '</button></form>' + eventTable(past.tournaments) + '<div class="pagination"><span>' + t('total') + ' ' + past.total + '</span><div><button class="outline" id="previous" ' + (state.page <= 1 ? 'disabled' : '') + '>' + t('previous') + '</button><span>' + state.page + ' <small>/ ' + Math.max(1, Math.ceil(past.total / 10)) + '</small></span><button class="outline" id="next" ' + (state.page * 10 >= past.total ? 'disabled' : '') + '>' + t('next') + '</button></div></div></section>');
    const filterURL = (page = 1, values = state.filters) => '/?' + new URLSearchParams({ ...values, p: page });
    $('#history-filter').addEventListener('submit', action(async event => { event.preventDefault(); const values = { ...Object.fromEntries(new FormData(event.target)), include_deleted: $('#include-deleted').checked ? '1' : '' }; if (values.from && values.to && values.from > values.to) throw Error(t('dateRangeError')); go(filterURL(1, values)); }));
    $('#include-deleted').onchange = () => go(filterURL(1, { ...state.filters, include_deleted: $('#include-deleted').checked ? '1' : '' }));
    $('#previous').onclick = () => go(filterURL(state.page - 1)); $('#next').onclick = () => go(filterURL(state.page + 1));
  }
  function loginPage() {
    app.innerHTML = '<main class="login-shell"><section class="login-story"><a class="brand" href="/" data-go="/"><span class="brand-mark">TB<span></span></span><span><strong>TEAM BATTLE</strong><small>' + t('brandCaption') + '</small></span></a><div><span class="eyebrow">1V1 / KOF / ACE</span><h2>' + t('loginHeadline') + '</h2><p>' + t('homeIntro') + '</p><div class="login-art" aria-hidden="true"><span>A</span><b>VS</b><span>B</span></div></div><small>TEAM BATTLE</small></section><section class="login-main"><div class="login-language">' + picker() + '</div><div class="login-card"><span class="eyebrow">WELCOME BACK</span><h1>' + t('login') + '</h1><p class="subtitle">' + t('loginIntro') + '</p><form id="login-form" class="login-form">' + field('username', input('username', '', 'text', 'required autocomplete="username" placeholder="' + t('username') + '"')) + field('password', input('password', '', 'password', 'required autocomplete="current-password"')) + '<button class="primary accent">' + t('login') + ' <span aria-hidden="true">→</span></button></form><p class="help">' + t('persistentLoginHint') + '</p><a class="back-link" href="/" data-go="/">← ' + t('homeLink') + '</a></div></section></main>'; bindCommon();
    $('#login-form').onsubmit = action(async event => { event.preventDefault(); const form = new FormData(event.target); state.user = (await post('/api/auth/login', Object.fromEntries(form))).user; go(loginTarget()); });
  }
  function profileLimit(changedAt, limited) {
    const next = changedAt ? new Date(Date.parse(changedAt) + 7 * 24 * 60 * 60 * 1000) : null;
    return limited && next && next > new Date() ? next : null;
  }
  function profileFields(user, categories, managed = false, creating = false) {
    const limited = !creating && user.id === state.user.id && state.user.role !== 'superadmin';
    const next = profileLimit(user.profile_changed_at, limited);
    const blocked = limited && state.profileEditBlocked;
    const disabled = next || blocked ? ' disabled' : '';
    const editableLogin = creating || user.id === state.user.id;
    const dateText = date => new Intl.DateTimeFormat({ zh: 'zh-CN', en: 'en-GB', ko: 'ko-KR' }[state.language], { dateStyle: 'medium', timeStyle: 'short' }).format(date);
    const limitHint = limited ? '<p class="notice full">' + t(blocked ? 'profileTournamentLocked' : 'profileWeekly') + (next ? ' ' + t('availableOn') + ' ' + esc(dateText(next)) : '') + '</p>' : '';
    const section = (title, contents) => `<fieldset class="profile-section full"><legend>${t(title)}</legend><div class="form-grid">${contents}</div></fieldset>`;
    const identity = field('avatar', `<div class="avatar-upload"><div id="avatar-preview">${user.avatar_url ? `<img class="account-avatar-preview" src="${esc(user.avatar_url)}" alt="">` : `<span class="account-avatar-empty">${t('noAvatar')}</span>`}</div><div><input type="file" name="avatar" accept="image/jpeg,image/png,image/gif,image/webp" aria-label="${t('avatar')}"${disabled}><p class="help">${t('imageHint')}</p></div></div>`, true)
      + field('displayName', input('display_name', user.display_name || '', 'text', (creating ? '' : 'required') + disabled), true);
    const credentials = field('username', input('username', user.username || '', 'text', 'required autocomplete="username"' + (editableLogin ? disabled : ' readonly')) + (editableLogin ? '' : '<p class="help">' + t('usernameSelfOnly') + '</p>'), true)
      + field('newPassword', input('new_password', '', 'password', 'minlength="6" autocomplete="new-password"' + (creating ? ' required' : '')))
      + field('confirmPassword', input('new_password_confirm', '', 'password', 'autocomplete="new-password"' + (creating ? ' required' : '')))
      + `<p class="help full">${t('passwordHint')}</p>`;
    const category = field('category', `<div class="category-choices"><label><input type="radio" name="category_id" value="0"${!user.category_id ? ' checked' : ''}${disabled}>${t('noCategory')}</label>${categories.map(c => `<label><input type="radio" name="category_id" value="${c.id}"${Number(c.id) === Number(user.category_id) ? ' checked' : ''}${disabled}>${c.icon_url ? `<img src="${esc(c.icon_url)}" alt="">` : ''}${esc(c.name)}</label>`).join('')}</div>`, true)
      + (managed ? field('role', '<select name="role"' + disabled + '>' + ['user', 'superadmin'].map(role => '<option value="' + role + '"' + (role === (user.role || 'user') ? ' selected' : '') + '>' + t(role) + '</option>').join('') + '</select>') : '');
    return limitHint + section('profileIdentity', identity) + section('profileCredentials', credentials) + section('profileCategory', category);
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
    state.profileEditBlocked = current.profile_edit_blocked;
    shell(`<div class="page-head"><h1>${t('settings')}</h1><button type="button" class="outline" data-player-record="${state.user.id}">${t('myRecords')}</button></div><section class="panel profile-panel"><form id="account-form" class="panel-body form-grid profile-form">${profileFields(state.user, catalog.categories)}<button type="submit" class="primary accent">${t('save')}</button></form></section>`, 'settings');
    bindProfileForm($('#account-form'), '/api/auth/profile', 'PATCH', async result => { state.user = result.user; await accountPage(); toast(t('saved')); });
  }
  async function usersPage(edit = '') {
    const [directory, catalog] = await Promise.all([api('/api/users'), api('/api/settings/catalog')]);
    const users = directory.users;
    const creating = edit === 'new', user = creating ? {} : users.find(u => String(u.id) === edit);
    if (edit && !creating && !user) return go(pageURL('accounts'));
    if (creating || user) {
      shell(`<div class="page-head"><div><h1>${t(creating ? 'createUser' : 'editAccount')}</h1>${creating ? '' : `<p class="subtitle">${esc(user.display_name)} · ${esc(user.username)}</p>`}</div><button class="outline" data-go="${pageURL('accounts')}">${t('backAccounts')}</button></div><section class="panel profile-panel"><form id="user-form" class="form-grid panel-body profile-form">${profileFields(user, catalog.categories, true, creating)}<button type="submit" class="primary accent">${t(creating ? 'createUser' : 'save')}</button></form></section>`, 'accounts');
      bindProfileForm($('#user-form'), '/api/users' + (creating ? '' : '/' + user.id), creating ? 'POST' : 'PATCH', async result => {
        if (result.user?.id === state.user.id) state.user = result.user;
        toast(t(creating ? 'accountCreated' : 'saved'));
        go(state.user.role === 'superadmin' ? pageURL('accounts') : '/');
      });
      return;
    }
    shell(`<div class="page-head"><h1>${t('accounts')}</h1><button class="primary accent" data-go="${pageURL('accounts')}/new">${t('createUser')}</button></div><section class="panel accounts-panel"><form id="account-filter" class="panel-body accounts-toolbar" role="search"><div class="field"><label for="account-search">${t('searchPlayers')}</label><input type="search" id="account-search" name="search" placeholder="${t('searchPlayers')}"></div><span id="account-count" class="count-badge" role="status" aria-live="polite"></span><p class="help">${t('accountDeleteHint')}</p></form><div class="table-wrap"><table class="table accounts-table"><thead><tr><th scope="col">${t('account')}</th><th scope="col">${t('role')}</th><th scope="col">${t('category')}</th><th scope="col">${t('actions')}</th></tr></thead><tbody>${users.map(u => `<tr data-account-row="${esc((u.username + ' ' + u.display_name).toLowerCase())}"><td class="account-identity">${person(u)}</td><td class="account-role-cell" data-label="${t('role')}">${t(u.role)}</td><td class="account-category-cell" data-label="${t('category')}">${esc(u.category_name || '—')}</td><td class="account-action-cell"><div class="account-actions"><button class="outline" data-go="${pageURL('accounts')}/${u.id}">${t('editAccount')}</button><button class="outline danger" data-delete-user="${u.id}"${u.can_delete ? '' : ' disabled'} title="${u.can_delete ? '' : t(u.id === state.user.id ? 'cannotDeleteSelf' : 'cannotDeletePlayed')}">${t('delete')}</button></div></td></tr>`).join('')}<tr id="account-empty" hidden><td colspan="4"><div class="empty">${t('noAccountMatches')}</div></td></tr></tbody></table></div></section>`, 'accounts');
    const filterAccounts = () => {
      const text = $('#account-search').value.trim().toLowerCase();
      const rows = $$('[data-account-row]');
      rows.forEach(row => { row.hidden = !row.dataset.accountRow.includes(text); });
      const count = rows.filter(row => !row.hidden).length;
      $('#account-count').textContent = `${t('total')} ${count} / ${users.length}`;
      $('#account-empty').hidden = count !== 0;
    };
    $('#account-filter').onsubmit = event => event.preventDefault();
    $('#account-filter').addEventListener('formrestored', filterAccounts);
    $('#account-search').oninput = filterAccounts;
    filterAccounts();
    $$('[data-delete-user]').forEach(button => button.onclick = action(async () => { if (!(await confirmAction(t('deleteAccountConfirm')))) return; await api('/api/users/' + button.dataset.deleteUser, { method: 'DELETE' }); await usersPage(); toast(t('saved')); }));
  }

  async function siteSettingsPage() {
    const data = await api('/api/site-settings');
    const defaults = data.tournament_defaults;
    const actions = contents => `<div class="settings-row-actions">${contents}</div>`;
    const nameInput = value => input('name', value, 'text', `required aria-label="${t('names')}" placeholder="${t('names')}"`);
    const categoryFields = (category = {}) => `<div class="settings-category-fields">${input('name', category.name || '', 'text', `required aria-label="${t('categoryName')}" placeholder="${t('categoryName')}"`)}<input type="file" name="icon" accept="image/jpeg,image/png,image/gif,image/webp" aria-label="${esc(t('icon') + (category.name ? ' · ' + category.name : ''))}"${category.id ? '' : ' required'}></div>`;
    const preview = category => `<span class="settings-category-preview">${category.icon_url ? `<img src="${esc(category.icon_url)}" alt="${esc(category.name)}">` : '<svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><rect x="3" y="3" width="18" height="18" rx="4"/><circle cx="9" cy="9" r="2"/><path d="m4 18 5-5 4 3 3-5 5 7"/></svg>'}</span>`;
    shell(`<div class="site-settings-page">
      <div class="page-head"><h1>${t('siteSettings')}</h1></div>
      <section class="panel settings-panel section-gap"><div class="panel-head"><h2>${t('tournamentDefaults')}</h2></div>
        <form id="tournament-defaults-form" class="panel-body settings-default-fields">
          ${field('randomDrawSeconds', input('random_draw_seconds', defaults.random_draw_seconds, 'number', 'min="0" step="1" required'))}
          ${field('aceDrawSeconds', input('ace_draw_seconds', defaults.ace_draw_seconds, 'number', 'min="0" step="1" required'))}
          <button class="primary">${t('save')}</button><p class="help full">${t('tournamentDefaultsHint')}</p>
        </form>
      </section>
      <div class="settings-columns">
        <section class="panel settings-panel"><div class="panel-head"><h2>${t('names')}</h2><span class="count-badge">${data.tournament_names.length}</span></div><div class="panel-body">
          <form id="name-form" class="settings-name-row settings-create-row">${nameInput('')}${actions(`<button class="primary">${t('add')}</button>`)}</form>
          ${data.tournament_names.map(n => `<form class="settings-name-row settings-list-row" data-name="${n.id}">${nameInput(n.name)}${actions(`<button class="outline">${t('save')}</button><button type="button" class="outline danger" data-delete-name="${n.id}">${t('delete')}</button>`)}</form>`).join('')}
        </div></section>
        <section class="panel settings-panel"><div class="panel-head"><h2>${t('categories')}</h2><span class="count-badge">${data.categories.length}</span></div><div class="panel-body">
          <form id="category-form" class="settings-category-row settings-create-row">${preview({})}${categoryFields()}${actions(`<button class="primary">${t('add')}</button>`)}</form>
          <p class="help settings-upload-hint">${t('imageHint')}</p>
          ${data.categories.map(c => `<form class="settings-category-row settings-list-row" data-category="${c.id}">${preview(c)}${categoryFields(c)}${actions(`<button class="outline">${t('save')}</button><button type="button" class="outline danger" data-delete-category="${c.id}">${t('delete')}</button>`)}</form>`).join('')}
        </div></section>
      </div>
    </div>`, 'siteSettings');
    $('#tournament-defaults-form').onsubmit = action(async event => {
      event.preventDefault();
      const f = new FormData(event.target);
      await post('/api/site-settings/tournament-defaults', { random_draw_seconds: Number(f.get('random_draw_seconds')), ace_draw_seconds: Number(f.get('ace_draw_seconds')) }, 'PATCH');
      await siteSettingsPage(); toast(t('saved'));
    });
    $('#name-form').onsubmit = action(async event => { event.preventDefault(); await post('/api/site-settings/tournament-names', Object.fromEntries(new FormData(event.target))); await siteSettingsPage(); toast(t('saved')); });
    $('#category-form').onsubmit = action(async event => { event.preventDefault(); await api('/api/site-settings/categories', { method: 'POST', body: new FormData(event.target) }); await siteSettingsPage(); toast(t('saved')); });
    $$('[data-name]').forEach(form => form.onsubmit = action(async event => { event.preventDefault(); await post('/api/site-settings/tournament-names/' + form.dataset.name, Object.fromEntries(new FormData(form)), 'PATCH'); await siteSettingsPage(); toast(t('saved')); }));
    $$('[data-category]').forEach(form => form.onsubmit = action(async event => { event.preventDefault(); await api('/api/site-settings/categories/' + form.dataset.category, { method: 'PATCH', body: new FormData(form) }); await siteSettingsPage(); toast(t('saved')); }));
    for (const kind of ['name', 'category']) $$(`[data-delete-${kind}]`).forEach(button => button.onclick = action(async () => { if (!(await confirmAction(t('deleteConfirm')))) return; const id = button.getAttribute('data-delete-' + kind); const path = kind === 'name' ? 'tournament-names' : 'categories'; await api('/api/site-settings/' + path + '/' + id, { method: 'DELETE' }); await siteSettingsPage(); toast(t('saved')); }));
  }

  async function backgroundsPage() {
    const data = await api('/api/backgrounds');
    shell(`<div class="page-head"><h1>${t('backgrounds')}</h1></div><section class="panel backgrounds-panel"><div class="panel-head"><h2>${t('backgroundImage')}</h2><span class="count-badge">${data.backgrounds.length}</span></div><div class="panel-body"><form id="background-form" class="field">${field('backgroundName', input('name', '', 'text', 'required'))}${field('backgroundImage', '<div class="background-file-actions"><input type="file" name="image" accept="image/jpeg,image/png,image/gif,image/webp" required><button class="primary">' + t('add') + '</button></div>')}<p class="help">${t('backgroundHint')}</p></form>${data.backgrounds.map(b => `<form class="background-edit item-form" data-background="${b.id}"><img class="background-preview" src="${esc(b.thumbnail_url)}" loading="lazy" decoding="async" alt="${esc(b.name)}">${input('name', b.name, 'text', `required aria-label="${t('backgroundName')}"`)}<div class="background-file-actions"><input type="file" name="image" accept="image/jpeg,image/png,image/gif,image/webp" aria-label="${t('backgroundImage')}"><button class="outline">${t('save')}</button><button type="button" class="outline danger" data-delete-background="${b.id}"${b.in_use ? ' disabled title="' + t('backgroundInUse') + '"' : ''}>${t('delete')}</button></div></form>`).join('')}</div></section>`, 'backgrounds');
    $('#background-form').onsubmit = action(async event => { event.preventDefault(); await api('/api/site-settings/backgrounds', { method: 'POST', body: new FormData(event.target) }); await backgroundsPage(); toast(t('saved')); });
    $$('[data-background]').forEach(form => form.onsubmit = action(async event => { event.preventDefault(); await api('/api/site-settings/backgrounds/' + form.dataset.background, { method: 'PATCH', body: new FormData(form) }); await backgroundsPage(); toast(t('saved')); }));
    $$('[data-delete-background]').forEach(button => button.onclick = action(async () => { if (!(await confirmAction(t('deleteConfirm')))) return; await api('/api/site-settings/backgrounds/' + button.dataset.deleteBackground, { method: 'DELETE' }); await backgroundsPage(); toast(t('saved')); }));
  }

  const rosterContext = () => ({ t, esc, person });
  function rosterMarkup(users, assignments, editable = true, allowRandom = true) {
    const entries = state.rosterOrder.filter(id => assignments[id]).map(id => [id, assignments[id]]);
    return window.TBRosterEditor.markup({ ...rosterContext(), users, entries, editable, allowRandom });
  }
  const rosterValue = form => window.TBRosterEditor.value(form);
  function bindRoster(form) {
    window.TBRosterEditor.bind($('#event-roster', form), { ...rosterContext(), users: state.users, onChange: () => {
      const entries = $$('[data-roster-player]', form).map(row => [Number(row.dataset.rosterPlayer), $('select', row).value]);
      state.roster = Object.fromEntries(entries);
      state.rosterOrder = entries.map(([id]) => id);
      $$('[data-special-candidates]', form).forEach(container => {
        const selected = specialCandidatesValue(container);
        container.innerHTML = specialCandidatesMarkup(selected);
      });
      refreshSpecialCounts();
      form.dispatchEvent(new Event('rosterchange'));
    } });
  }
  const defaultRules = () => ({ round1_mode: 'manual', kof_starter: 'random', kof_revival_no_repeat: false, no_repeat: true, random_draw_seconds: 5, ace_draw_seconds: 5, ace_most_wins: 20, ace_most_losses: 20, ace_specials: [] });
  const specialSideIDs = (row, side) => [...($(`[name="special_${side}_ids"]`, row)?.selectedOptions || [])].map(option => Number(option.value));
  const specialCandidatesValue = row => ({ team_a_ids: specialSideIDs(row, 'a'), team_b_ids: specialSideIDs(row, 'b') });
  const specialCandidateIDs = row => [...new Set([...specialSideIDs(row, 'a'), ...specialSideIDs(row, 'b')])];
  function specialCandidatesMarkup(group = {}) {
    const available = state.rosterOrder.map(id => state.users.find(user => user.id === id)).filter(Boolean);
    const split = Array.isArray(group.team_a_ids) || Array.isArray(group.team_b_ids);
    // 抽签前只列固定到本侧的选手，抽签后按正式分队列候选。
    const sideOf = id => state.specialTeams?.[id] || state.roster[id];
    const column = side => {
      const team = side.toUpperCase(), label = t(side === 'a' ? 'teamA' : 'teamB');
      const selected = split ? group[`team_${side}_ids`] || [] : group.player_ids || [];
      const players = available.filter(user => sideOf(user.id) === team);
      const option = user => `<option value="${user.id}"${selected.includes(user.id) ? ' selected' : ''}>${esc(user.display_name)}${user.username && user.username !== user.display_name ? ` (${esc(user.username)})` : ''}${user.category_name ? ` · ${esc(user.category_name)}` : ''}</option>`;
      return `<label class="special-team-candidates"><span class="field-label">${label} · ${t('specialCandidates')}</span><select class="special-player-select" name="special_${side}_ids" data-special-side="${side}" multiple size="6" aria-label="${label} · ${t('specialCandidates')}">${players.map(option).join('')}</select><small class="help">${t('specialSideFallback')}</small></label>`;
    };
    const hasCandidates = available.some(user => ['A', 'B'].includes(sideOf(user.id)));
    return `<div class="special-teams">${column('a')}<span class="special-teams-vs" aria-hidden="true">VS</span>${column('b')}</div>${hasCandidates ? '' : `<p class="help">${t('selectPlayersFirst')}</p>`}`;
  }
  function specialMarkup(group = { player_ids: [], probability: 20 }) {
    return `<section class="special-rule"><div class="special-rule-head"><h3 data-special-title></h3><span data-special-count></span><button type="button" class="link danger" data-remove-special>${t('delete')}</button></div><div class="special-probability">${field('probability', input('special_probability', group.probability, 'number', 'min="1" max="100" required'))}<p class="help">${t('candidateHint')}</p></div><div class="special-candidates" data-special-candidates>${specialCandidatesMarkup(group)}</div></section>`;
  }
  function refreshSpecialCounts() {
    $$('.special-rule').forEach((row, index) => {
      $('[data-special-title]', row).textContent = `${t('specialMatchGroup')}${index + 1}`;
      $('[data-special-count]', row).textContent = `${t('teamA')} ${specialSideIDs(row, 'a').length} · ${t('teamB')} ${specialSideIDs(row, 'b').length}`;
      $$('[data-special-side]', row).forEach(select => select.setAttribute('aria-label', `${t('specialMatchGroup')}${index + 1} · ${t(select.dataset.specialSide === 'a' ? 'teamA' : 'teamB')} · ${t('specialCandidates')}`));
    });
  }
  function rulesMarkup(e = {}, roundEditable = true, aceEditable = true) {
    const rules = { ...defaultRules(), ...e.rules };
    const roundDisabled = roundEditable ? '' : ' disabled';
    const aceDisabled = aceEditable ? '' : ' disabled';
    const hiddenRound = roundEditable ? '' : input('round1_target', e.round1_target || 4, 'hidden') + input('round2_target', e.round2_target || 5, 'hidden') + input('round1_mode', rules.round1_mode, 'hidden') + (rules.no_repeat ? input('no_repeat', 'on', 'hidden') : '') + input('kof_starter', rules.kof_starter, 'hidden') + (rules.kof_revival_no_repeat ? input('kof_revival_no_repeat', 'on', 'hidden') : '');
    return `<section class="panel section-gap editor-section" id="event-round1"><div class="panel-head"><h2>${t('round1')}</h2></div><div class="panel-body form-grid">${field('round1Target', input('round1_target', e.round1_target || 4, 'number', 'min="1" max="99" required' + roundDisabled))}${field('round1Mode', `<select name="round1_mode"${roundDisabled}><option value="manual"${rules.round1_mode === 'manual' ? ' selected' : ''}>${t('manual')}</option><option value="random"${rules.round1_mode === 'random' ? ' selected' : ''}>${t('random')}</option></select>`)}<label class="check"><input name="no_repeat" type="checkbox"${rules.no_repeat ? ' checked' : ''}${roundDisabled}>${t('noRepeat')}</label>${hiddenRound}</div></section><section class="panel section-gap editor-section" id="event-round2"><div class="panel-head"><h2>${t('round2')}</h2></div><div class="panel-body form-grid">${field('round2Target', input('round2_target', e.round2_target || 5, 'number', 'min="1" max="99" required' + roundDisabled))}${field('kofStarter', `<div class="mode-choices">${['random', 'captain'].map(mode => `<label><input type="radio" name="kof_starter" value="${mode}"${rules.kof_starter === mode ? ' checked' : ''}${roundDisabled}><span>${t(mode === 'random' ? 'randomStarter' : 'captainStarter')}</span></label>`).join('')}</div>`)}<div class="full" data-kof-revival-rule><label class="check"><input name="kof_revival_no_repeat" type="checkbox"${rules.kof_revival_no_repeat ? ' checked' : ''}${roundDisabled}>${t('kofRevivalNoRepeat')}</label><p class="help">${t('kofRevivalNoRepeatHint')}</p></div><p class="help full">${t('kofHint')}</p></div></section><section class="panel section-gap editor-section" id="event-ace"><div class="panel-head"><div class="ace-editor-heading"><h2>${t('aceRules')}</h2><span class="ace-edit-hint">${t('aceEditableUntilFirstResult')}</span></div><span id="fallback-probability"></span></div><div class="panel-body"><div class="form-grid">${field('mostWins', input('ace_most_wins', rules.ace_most_wins, 'number', 'min="0" max="100" required' + aceDisabled))}${field('mostLosses', input('ace_most_losses', rules.ace_most_losses, 'number', 'min="0" max="100" required' + aceDisabled))}</div><p class="help">${t('aceHint')}</p><div id="special-rules">${rules.ace_specials.map(specialMarkup).join('')}</div><button type="button" class="outline" id="add-special"${aceDisabled}>${t('addSpecial')}</button></div></section>`;
  }
  function readRulesDraft(form) {
    // FormData会跳过禁用控件；只读赛事和锁定规则也必须按实际值计算概率。
    const value = name => $(`[name="${name}"]:checked`, form)?.value ?? $(`[name="${name}"]`, form)?.value;
    const checked = name => !!$(`[name="${name}"]:checked`, form);
    return { round1_mode: value('round1_mode'), kof_starter: value('kof_starter'), no_repeat: checked('no_repeat'), kof_revival_no_repeat: checked('kof_revival_no_repeat'), random_draw_seconds: Number(value('random_draw_seconds')), ace_draw_seconds: Number(value('ace_draw_seconds')), ace_most_wins: Number(value('ace_most_wins')), ace_most_losses: Number(value('ace_most_losses')), ace_specials: $$('.special-rule', form).map(row => ({ probability: Number($('[name="special_probability"]', row).value), ...specialCandidatesValue(row), player_ids: specialCandidateIDs(row) })) };
  }
  function rulesValue(form) {
    const rules = readRulesDraft(form);
    const probabilities = [rules.ace_most_wins, rules.ace_most_losses, ...rules.ace_specials.map(group => group.probability)];
    if (probabilities.some(value => !Number.isInteger(value) || value < 0) || probabilities.reduce((sum, value) => sum + value, 0) > 100) throw Error(t('probabilityError'));
    if (rules.ace_specials.some(group => group.probability === 0)) throw Error(t('specialProbabilityError'));
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
      const sizes = window.TBKOF.teamSizes(state.rosterOrder.map(id => [id, state.specialTeams?.[id] || state.roster[id]]));
      $('[data-kof-revival-rule]', form).hidden = !window.TBKOF.needsRevivalRule(sizes, Number($('[name=round2_target]', form).value));
    };
    $('#add-special').onclick = () => { $('#special-rules').insertAdjacentHTML('beforeend', specialMarkup()); refresh(); };
    $('#special-rules').addEventListener('click', event => { const remove = event.target.closest('[data-remove-special]'); if (remove) { remove.closest('.special-rule').remove(); refresh(); } });
    form.addEventListener('rosterchange', refresh);
    form.addEventListener('formrestored', refresh);
    form.addEventListener('input', refresh);
    form.addEventListener('change', refresh);
    refresh();
  }
  function drawTimingMarkup(e, editableTiming) {
    const rules = { ...defaultRules(), ...e.rules };
    return `<section class="panel section-gap draw-timing-panel editor-section" id="event-timing"><div class="panel-head"><h2>${t('drawTiming')}</h2><span>${t('drawTimingMutable')}</span></div><div class="panel-body form-grid">${field('randomDrawSeconds', input('random_draw_seconds', rules.random_draw_seconds, 'number', 'min="0" step="1" required' + (editableTiming ? '' : ' disabled')))}${field('aceDrawSeconds', input('ace_draw_seconds', rules.ace_draw_seconds, 'number', 'min="0" step="1" required' + (editableTiming ? '' : ' disabled')))}<p class="help full">${t('drawTimingHint')}</p></div></section>`;
  }
  function editorJumps(correction = false) {
    const sections = [['basics', 'basicInfo'], ['roster', 'players'], ['round1', 'round1'], ['round2', 'round2'], ['ace', 'ace'], ['timing', 'drawTiming']];
    if (correction) sections.push(['correction', 'correct']);
    return `<nav class="editor-jumps" aria-label="${t('eventSections')}">${sections.map(([id, title], index) => `<a href="#event-${id}"><span>${index + 1}</span>${t(title)}</a>`).join('')}</nav>`;
  }
  function backgroundOptions(backgrounds, selected = 0) {
    return `<option value="0"${Number(selected) ? '' : ' selected'}>${t('noBackground')}</option>` + backgrounds.map(background => `<option value="${background.id}"${Number(background.id) === Number(selected) ? ' selected' : ''}>${esc(background.name)}</option>`).join('');
  }
  function eventBackgroundMarkup(e, backgrounds, editable) {
    return `<section class="panel section-gap event-background-panel"><div class="panel-head"><h2>${t('background')}</h2><span>${esc(e.background_name || t('noBackground'))}</span></div><div class="panel-body"><div class="inline-form"><select name="background_id"${editable ? '' : ' disabled'}>${backgroundOptions(backgrounds, e.background_id)}</select></div><p class="help">${t('eventBackgroundHint')}</p></div></section>`;
  }
  // 大将调整与结束确认只出现在赛事设置，不加入公共观赛菜单。
  function eventManagementMarkup(e) {
    const pending = e.status === 'ace' && e.matches.find(match => match.stage === 'ace' && match.status === 'pending');
    const snapshot = JSON.stringify([editable(e), e.status, pending?.id, pending?.player_a_id, pending?.player_b_id]);
    let content = '';
    if (editable(e) && pending) content = `<section class="panel section-gap editor-section"><div class="panel-head"><h2>${t('overrideAce')}</h2></div><div class="panel-body form-grid"><input type="hidden" name="ace_match_id" value="${pending.id}">${field('teamA', `<select name="ace_player_a_id">${options(e.teams[0].players.filter(player => player.active !== false), pending.player_a_id)}</select>`)}${field('teamB', `<select name="ace_player_b_id">${options(e.teams[1].players.filter(player => player.active !== false), pending.player_b_id)}</select>`)}<p class="help full">${t('aceSettingsSaveHint')}</p></div></section>`;
    if (editable(e) && e.status === 'final_confirm') content = `<section class="panel section-gap editor-section"><div class="panel-head"><h2>${t('confirmEnd')}</h2></div><div class="panel-body"><p class="help">${t('confirmEndHint')}</p><button type="button" class="primary accent" id="confirm-end">${t('confirmEnd')}</button></div></section>`;
    return `<div id="event-management" data-snapshot="${esc(snapshot)}">${content}</div>`;
  }
  function refreshEventManagement(e) {
    const panel = $('#event-management');
    if (!panel || state.editor !== 'settings') return;
    const template = document.createElement('template'); template.innerHTML = eventManagementMarkup(e);
    if (panel.dataset.snapshot !== template.content.firstElementChild.dataset.snapshot) panel.replaceWith(template.content);
  }
  const matchOrder = (a, b) => a.round - b.round || a.seq - b.seq;
  const finishedMatches = e => e.matches.filter(match => match.status === 'finished').sort(matchOrder);
  function correctionPlayersMarkup(e, match) {
    const prefix = e.matches.filter(row => row.stage === 'round2' && row.seq < match.seq && row.status === 'finished').sort(matchOrder);
    const pick = (side, index) => {
      const team = e.teams[index].players.filter(player => player.active !== false);
      const candidates = match.stage === 'round2'
        ? window.TBKOF.nextPlayers(team.map(player => player.id), prefix, side, e.round2_target, e.rules.kof_revival_no_repeat)
        : { ids: team.map(player => player.id) };
      const name = 'correction_player_' + side + '_id';
      const label = t(index === 0 ? 'teamA' : 'teamB') + (candidates.carry ? ' · ' + t('archiveCarry') : '');
      return `<label>${label}<select name="${name}"${candidates.carry ? ' disabled' : ''}>${team.filter(player => candidates.ids.includes(player.id)).map(player => `<option value="${player.id}"${player.id === match['player_' + side + '_id'] ? ' selected' : ''}>${esc(player.display_name)}</option>`).join('')}</select></label>`;
    };
    return `<div class="correction-players" data-correction-players="${match.id}"><h3>${t('correctLastMatchPlayers')} · ${t(match.stage)} #${match.seq}</h3><input type="hidden" name="correction_players_match_id" value="${match.id}"><div class="correction-player-picks">${pick('a', 0)}${pick('b', 1)}</div></div>`;
  }
  function readCorrection(form, e) {
    if (!e?.matches || !$('#event-correction', form)) return null;
    const last = finishedMatches(e).at(-1);
    const playerA = Number($('[name="correction_player_a_id"]', form).value) || last.player_a_id;
    const playerB = Number($('[name="correction_player_b_id"]', form).value) || last.player_b_id;
    const playersChanged = Number($('[name="correction_players_match_id"]', form).value) === last.id && (playerA !== last.player_a_id || playerB !== last.player_b_id);
    const corrections = [...$$('[data-correction-winner]', form)].map(field => {
      const match = e.matches.find(row => row.id === Number(field.dataset.matchId) && row.status === 'finished');
      if (!match || match.winner_team_id === Number(field.value)) return null;
      return { match, winner: Number(field.value), playersChanged: false };
    }).filter(Boolean);
    if (playersChanged) {
      const existing = corrections.find(correction => correction.match.id === last.id);
      if (existing) Object.assign(existing, { playersChanged: true, playerA, playerB });
      else corrections.push({ match: last, winner: last.winner_team_id, playersChanged: true, playerA, playerB });
    }
    corrections.sort((a, b) => matchOrder(a.match, b.match));
    return corrections.length ? { corrections, match: corrections[0].match } : null;
  }
  function resultCorrectionMarkup(e) {
    if (!editable(e)) return '';
    const matches = finishedMatches(e);
    if (!matches.length) return '';
    const people = e.teams.flatMap(team => team.players);
    const rounds = ['round1', 'round2', 'ace'].map(stage => {
      const rows = matches.filter(match => match.stage === stage).sort((a, b) => a.seq - b.seq);
      if (!rows.length) return '';
      return `<section class="correction-round"><h3>${t(stage)}</h3><div class="correction-column-head" aria-hidden="true"><span>${t('matchNo')}</span><span>${t('teamA')}</span><span>${t('recordedResult')}</span><span>${t('teamB')}</span><span></span></div>${rows.map(match => {
        const a = people.find(player => player.id === match.player_a_id) || { display_name: match.player_a_name }, b = people.find(player => player.id === match.player_b_id) || { display_name: match.player_b_name };
        const winsA = match.winner_team_id === e.teams[0].id;
        return `<div class="correction-row" data-correction-row="${match.id}"><span class="correction-match-no">#${match.seq}</span><div class="correction-player ${winsA ? 'is-winner' : 'is-loser'}">${boardIdentity(a)}</div><div class="correction-result"><strong>${t(winsA ? 'teamA' : 'teamB')} ${t('win')}</strong><small data-correction-status></small></div><div class="correction-player ${winsA ? 'is-loser' : 'is-winner'}">${boardIdentity(b)}</div><button type="button" class="outline" data-correct-match="${match.id}">${t('changeWinner').replace('{name}', esc(winsA ? b.display_name : a.display_name))}</button></div>`;
      }).join('')}</section>`;
    }).join('');
    return `<section class="panel section-gap editor-section" id="event-correction"><div class="panel-head"><h2>${t('correct')}</h2><span>${matches.length} ${t('playedMatches')}</span></div><div class="panel-body"><p class="correction-warning"><strong>${t('correctHint')}</strong></p><p class="help">${t('correctionSaveHint')}</p><p class="correction-warning" data-correction-scope role="status" hidden></p><div data-correction-inputs hidden></div>${rounds}${correctionPlayersMarkup(e, matches.at(-1))}</div></section>`;
  }
  function syncResultCorrection(form) {
    const panel = $('#event-correction', form);
    if (!panel) return;
    const e = state.event;
    const last = finishedMatches(e).at(-1);
    const playerMatch = $('[name="correction_players_match_id"]', panel);
    if (Number(playerMatch.value) !== last.id) {
      playerMatch.value = String(last.id);
      for (const side of ['a', 'b']) {
        const field = $('[name="correction_player_' + side + '_id"]', panel);
        field.value = String(last['player_' + side + '_id']);
      }
    }
    const draft = readCorrection(form, e);
    const scope = $('[data-correction-scope]', panel);
    const later = draft?.corrections.length === 1 ? e.matches.filter(row => matchOrder(row, draft.match) > 0).length : 0;
    scope.textContent = draft?.corrections.length > 1 ? t('correctionBatchHint').replace('{count}', draft.corrections.length) : later ? t('correctionClearCount').replace('{count}', later) : '';
    scope.hidden = !scope.textContent;
    const changed = new Map((draft?.corrections || []).filter(correction => !correction.playersChanged).map(correction => [correction.match.id, correction]));
    $$('[data-correction-row]', panel).forEach(row => {
      const current = e.matches.find(current => current.id === Number(row.dataset.correctionRow));
      const chosen = draft?.match.id === current.id;
      const discarded = draft?.corrections.length === 1 && matchOrder(current, draft.match) > 0;
      const button = $('[data-correct-match]', row);
      const correction = changed.get(current.id);
      const originalSide = current.who_win === 'a' ? 'b' : 'a';
      const winnerSide = correction?.winner === e.teams[0].id ? 'a' : correction ? 'b' : current.who_win;
      const player = draft?.corrections.find(item => item.playersChanged && current.id === last.id);
      if (current.id === last.id) {
        const a = player ? e.teams[0].players.find(item => item.id === player.playerA) : e.teams[0].players.find(item => item.id === current.player_a_id);
        const b = player ? e.teams[1].players.find(item => item.id === player.playerB) : e.teams[1].players.find(item => item.id === current.player_b_id);
        const identities = $$('.correction-player', row);
        if (a && b && identities.length === 2) {
          identities[0].innerHTML = boardIdentity(a);
          identities[1].innerHTML = boardIdentity(b);
        }
      }
      const displayName = player ? e.teams[originalSide === 'a' ? 0 : 1].players.find(item => item.id === (originalSide === 'a' ? player.playerA : player.playerB))?.display_name : current['player_' + originalSide + '_name'];
      button.setAttribute('aria-pressed', String(Boolean(correction)));
      button.textContent = correction ? t('cancelCorrection') : t('changeWinner').replace('{name}', displayName || current['player_' + originalSide + '_name']);
      row.classList.toggle('is-selected', Boolean(chosen || correction || player)); row.classList.toggle('is-discarded', Boolean(discarded));
      $('[data-correction-status]', row).textContent = correction ? t(winnerSide === 'a' ? 'teamA' : 'teamB') + ' · ' + t('pendingCorrection') : discarded ? t('clearedAfterSave') : player ? t('pendingCorrection') : '';
    });
  }
  function refreshResultCorrection(e) {
    const form = $('#event-form');
    if (!form || state.editor !== 'settings') return;
    const panel = $('#event-correction', form), markup = resultCorrectionMarkup(e);
    const changes = panel ? [...$$('[data-correction-winner]', panel)].map(field => [field.dataset.matchId, field.value]) : [];
    const oldLastID = $('[data-correction-players]', panel || form)?.dataset.correctionPlayers;
    const oldPlayers = panel ? ['a', 'b'].map(side => $('[name="correction_player_' + side + '_id"]', panel)) : [];
    const oldPicks = oldPlayers.map(field => ({ value: field.value, original: Array.from(field.options).find(option => option.defaultSelected)?.value }));
    if (panel) panel.outerHTML = markup;
    else if (markup) { $('.event-editor-actions', form).insertAdjacentHTML('beforebegin', markup); $('.editor-jumps').outerHTML = editorJumps(true); }
    if (markup) {
      const container = $('[data-correction-inputs]', form);
      for (const [matchID, winnerID] of changes) {
        if (!e.matches.some(match => match.id === Number(matchID) && match.status === 'finished')) continue;
        container.insertAdjacentHTML('beforeend', `<input type="hidden" data-correction-winner data-match-id="${matchID}" name="correction_winner_${matchID}" value="${winnerID}">`);
      }
      if ($('[data-correction-players]', form).dataset.correctionPlayers === oldLastID) {
        ['a', 'b'].forEach((side, index) => {
          const field = $('[name="correction_player_' + side + '_id"]', form), pick = oldPicks[index];
          if (pick.original === field.value && [...field.options].some(option => option.value === pick.value)) field.value = pick.value;
        });
      }
      syncResultCorrection(form);
    }
  }
  async function eventEditor(mode = 'create', preserve = false) {
    const source = mode === 'create' ? {} : state.event;
    if (mode !== 'create' && !manages(source)) { state.editor = null; return boardPage(); }
    const readonly = mode !== 'create' && !canEditSettings(source);
    const roundSetupEditable = mode === 'create' || (canEditSettings(source) && !hasGrouping(source));
    const aceSettingsEditable = mode === 'create' || canEditSettings(source);
    const timingEditable = mode === 'create' || (mode !== 'roster' && manages(source) && !terminal(source));
    let e = preserve && state.ruleDraft && !readonly ? { ...source, rules: state.ruleDraft } : source;
    const [users, catalog] = await Promise.all([
      state.user.role === 'superadmin' ? api('/api/users') : terminal(e) ? Promise.resolve({ users: e.teams.flatMap(team => team.players) }) : api('/api/tournaments/' + e.id + '/eligible-users'),
      state.user.role === 'superadmin' ? api('/api/site-settings') : api('/api/settings/catalog')
    ]);
    if (mode === 'create' && !(preserve && state.ruleDraft)) {
      e = { ...e, rules: { ...defaultRules(), ...catalog.tournament_defaults } };
    }
    // 将已录入资料补入目录，停用账号也不能在查看或保存时悄悄消失。
    const directory = new Map(users.users.filter(u => u.role !== 'superadmin').map(user => [user.id, user]));
    for (const player of [...(e.teams || []).flatMap(team => team.players), ...(e.setup_roster || [])]) {
      if (player.display_name && !directory.has(player.id)) directory.set(player.id, player);
    }
    state.users = [...directory.values()];
    state.specialTeams = Object.fromEntries((e.teams || []).flatMap((team, index) => team.players.filter(player => player.active !== false).map(player => [player.id, index === 0 ? 'A' : 'B'])));
    const backgrounds = catalog.backgrounds || [];
    if (!preserve || readonly) {
      // 设置页只还原录入意向；旧赛事未保存意向时，不用实际分队猜测原始选择。
      const entries = e.setup_roster?.length ? e.setup_roster.map(p => [p.id, p.fixed_team || 'R']) : (e.teams || []).flatMap(team => team.players.filter(p => p.active !== false)).sort((a, b) => a.id - b.id).map(p => [p.id, '?']);
      state.roster = Object.fromEntries(entries);
      state.rosterOrder = entries.map(([id]) => id);
    }
    const managers = users.users.filter(user => user.status === 'active' && !user.is_deleted);
    const managerField = state.user.role === 'superadmin' ? field('manager', '<select name="manager_id" data-searchable required><option value="">' + t('selectManager') + '</option>' + managers.map(user => '<option value="' + user.id + '"' + (Number(e.manager_id) === user.id ? ' selected' : '') + '>' + esc(user.display_name) + ' (' + esc(user.username) + ')</option>').join('') + '</select>') : '';
    const createBackground = mode === 'create' ? field('background', `<select name="background_id">${backgroundOptions(backgrounds, e.background_id)}</select>`) : '';
    const basic = mode !== 'roster' ? '<section class="panel editor-section" id="event-basics"><div class="panel-head"><h2>' + t('basicInfo') + '</h2><span>' + t('requiredBasics') + '</span></div><div class="panel-body form-grid">' + field('name', '<input name="name" list="tournament-names" value="' + esc(e.name || '') + '" required><datalist id="tournament-names">' + (catalog.tournament_names || []).map(n => '<option value="' + esc(n.name) + '">').join('') + '</datalist>', true) + field('time', startTimeInput(e.start_time || localTime())) + managerField + createBackground + field('description', '<textarea name="description" rows="2">' + esc(e.description || '') + '</textarea>', true) + '</div></section>' : '';
    const backgroundPanel = mode === 'create' || mode === 'roster' ? '' : eventBackgroundMarkup(e, backgrounds, manages(e) && !terminal(e));
    const correctionPanel = mode === 'settings' ? resultCorrectionMarkup(e) : '';
    const settingsHint = terminal(e) ? 'readOnly' : readonly ? 'settingsLocked' : mode === 'create' ? 'requiredHint' : roundSetupEditable ? 'settingsResetHint' : 'roundSetupLocked';
    const content = '<div class="page-head"><div><h1>' + t(mode === 'create' ? 'createEvent' : mode === 'roster' ? 'editRoster' : 'eventSettings') + '</h1><p class="subtitle">' + t(settingsHint) + '</p></div></div>' + (mode === 'roster' ? '' : editorJumps(!!correctionPanel)) + '<form id="event-form"><fieldset class="event-settings-fields">' + basic + rosterMarkup(state.users, state.roster, roundSetupEditable) + (mode === 'roster' ? '' : rulesMarkup(e, roundSetupEditable, aceSettingsEditable)) + '</fieldset>' + (mode === 'roster' ? '' : drawTimingMarkup(e, timingEditable)) + backgroundPanel + correctionPanel + (mode === 'settings' ? eventManagementMarkup(e) : '') + '<div class="event-editor-actions">' + (mode === 'create' ? '' : '<button type="button" class="outline" id="back-board">' + t('backToMatch') + '</button>') + (!terminal(e) ? '<button type="submit" class="primary accent">' + t(mode === 'create' ? 'createEvent' : 'save') + '</button>' : '') + '</div></form>';
    if (mode === 'create') shell(content, 'createEvent'); else {
      app.innerHTML = '<main class="event-editor-page"><div class="editor-language">' + picker() + '</div>' + content + '</main>';
      bindCommon(); $('#back-board').onclick = () => go(canWatch(state.event) ? '/live/' + state.event.id : '/');
    }
    const form = $('#event-form');
    form.addEventListener('click', click => {
      if (!click.target.closest('#confirm-end') || form.dataset.busy) return;
      click.preventDefault();
      action(async () => {
        const values = new FormData(form), current = state.event;
        if (readCorrection(form, current) || Number(values.get('background_id') || 0) !== Number(current.background_id || 0) || Number(values.get('random_draw_seconds')) !== current.rules.random_draw_seconds || Number(values.get('ace_draw_seconds')) !== current.rules.ace_draw_seconds) throw Error(t('saveSettingsBeforeConfirm'));
        await confirmEventEnd();
      })(click);
    });
    form.addEventListener('click', click => {
      const button = click.target.closest('[data-correct-match]');
      if (!button || form.dataset.busy) return;
      const current = state.event, match = current.matches.find(match => match.id === Number(button.dataset.correctMatch) && match.status === 'finished');
      if (!match || !editable(current)) return;
      const container = $('[data-correction-inputs]', form);
      const existing = $('[data-match-id="' + match.id + '"]', container);
      if (existing) existing.remove();
      else container.insertAdjacentHTML('beforeend', `<input type="hidden" data-correction-winner data-match-id="${match.id}" name="correction_winner_${match.id}" value="${current.teams.find(team => team.id !== match.winner_team_id).id}">`);
      syncResultCorrection(form);
    });
    form.addEventListener('change', event => { if (event.target.closest('[data-correction-players]')) syncResultCorrection(form); });
    form.addEventListener('formrestored', () => syncResultCorrection(form));
    if (mode !== 'create' && mode !== 'roster' && roundSetupEditable) $('#event-ace').insertAdjacentHTML('beforebegin', `<div class="setup-draw-action"><button type="button" class="primary accent" id="start-setup-draw">${t('startSetupDraw')}</button><span class="help">${t('setupDrawHint')}</span></div>`);
    $('#start-setup-draw')?.addEventListener('click', () => {
      if (form.dataset.busy || !form.reportValidity()) return;
      form.dataset.submitAction = 'draw';
      form.requestSubmit();
    });
    bindRoster(form);
    if (mode !== 'roster') bindRules(form);
    $('.event-settings-fields', form).disabled = readonly;
    form.onsubmit = action(async event => {
      event.preventDefault();
      const beginDraw = form.dataset.submitAction === 'draw';
      delete form.dataset.submitAction;
      const f = new FormData(form);
      const body = readonly ? { random_draw_seconds: Number(f.get('random_draw_seconds')), ace_draw_seconds: Number(f.get('ace_draw_seconds')), background_id: Number(f.get('background_id') || 0) } : mode === 'roster' ? rosterValue(form) : {
        name: f.get('name'), start_time: startTimeValue(f), description: f.get('description'),
        ...(state.user.role === 'superadmin' ? { manager_id: Number(f.get('manager_id') || 0) } : {}),
        round1_target: Number(f.get('round1_target')), round2_target: Number(f.get('round2_target')), background_id: Number(f.get('background_id') || 0),
        rules: rulesValue(form), ...(!hasGrouping(e) ? rosterValue(form) : {})
      };
      const correction = readCorrection(form, state.event);
      const aceMatchID = Number(f.get('ace_match_id'));
      if (aceMatchID) {
        const current = state.event.matches.find(match => match.id === aceMatchID && match.stage === 'ace' && match.status === 'pending');
        if (!current || state.event.status !== 'ace') throw Error(t('resultChanged'));
        const playerA = Number(f.get('ace_player_a_id')), playerB = Number(f.get('ace_player_b_id'));
        if (current.player_a_id !== playerA || current.player_b_id !== playerB) {
          if (correction) throw Error(t('saveCorrectionBeforeAce'));
          body.ace_players = { match_id: aceMatchID, player_a_id: playerA, player_b_id: playerB };
        }
      }
      if (correction) {
        const { match, corrections } = correction;
        const matches = [...state.event.matches].sort(matchOrder);
        const latest = matches.at(-1), later = matches.filter(row => matchOrder(row, match) > 0).length;
        const snapshot = JSON.stringify(state.event.matches);
        const checkSnapshot = () => {
          if (!form.isConnected || state.event?.id !== e.id || !editable(state.event) || JSON.stringify(state.event.matches) !== snapshot) throw Error(t('resultChanged'));
        };
        const summary = corrections.map(item => {
          const preview = {
            stage: t(item.match.stage), seq: item.match.seq, winner: item.winner === e.teams[0].id ? 'a' : 'b',
            a: item.playersChanged ? e.teams[0].players.find(player => player.id === item.playerA)?.display_name : item.match.player_a_name,
            b: item.playersChanged ? e.teams[1].players.find(player => player.id === item.playerB)?.display_name : item.match.player_b_name
          };
          return `${preview.stage} #${preview.seq}: ${preview.a} vs ${preview.b} · ${t(preview.winner === 'a' ? 'teamA' : 'teamB')} ${t('win')}`;
        }).join('\n');
        const message = corrections.length === 1
          ? t('correctionReview') + '\n' + summary
          : t('correctionBatchReview').replace('{count}', corrections.length) + '\n' + summary;
        if (!(await window.TBConfirm.ask(message, { title: t('correct') }))) return;
        checkSnapshot();
        if (corrections.length === 1 && later) {
          if (!(await window.TBConfirm.ask(t('correctionClearCount').replace('{count}', later), { title: t('correctionClearTitle') }))) return;
          checkSnapshot();
        }
        body.corrections = corrections.map(item => ({
          match_id: item.match.id, winner_team_id: item.winner,
          ...(item.playersChanged ? { player_a_id: item.playerA, player_b_id: item.playerB } : {}),
          expected: {
            match_id: item.match.id, player_a_id: item.match.player_a_id, player_b_id: item.match.player_b_id, winner_team_id: item.match.winner_team_id,
            latest_match_id: latest.id, latest_winner_team_id: latest.winner_team_id || 0
          }
        }));
      }
      if (mode === 'create') { const created = await post('/api/tournaments', body); toast(t('eventCreated')); go('/live/' + created.id); }
      else {
        await post('/api/tournaments/' + e.id + (mode === 'roster' ? '/players' : '/settings'), body, mode === 'roster' ? 'POST' : 'PATCH');
        if (beginDraw) {
          const result = await post('/api/tournaments/' + e.id + '/setup-draw');
          state.editor = null;
          closeStream(); history.pushState({}, '', '/live/' + e.id);
          await openEvent(e.id);
          await playSetupDraw(result.steps, state.event);
          return;
        }
        state.editor = null;
        if (mode === 'settings') history.replaceState({}, '', '/live/' + e.id);
        if (!canWatch(state.event)) { await openEvent(e.id); } else { await refreshEvent(); }
        toast(t('saved'));
      }
    });
  }

  // 选手状态按本场赛事累计胜局着色，进入下一轮时不重置。
  function playerHasWin(e, playerID) { return e.matches.some(m => m.status === 'finished' && ((m.player_a_id === playerID && m.who_win === 'a') || (m.player_b_id === playerID && m.who_win === 'b'))); }
  function playerHasLoss(e, playerID, stage) { return e.matches.some(m => m.stage === stage && m.status === 'finished' && ((m.player_a_id === playerID && m.who_win === 'b') || (m.player_b_id === playerID && m.who_win === 'a'))); }
  function waitingKOFMatch(e) {
    if (e.status !== 'round2' || e.matches.some(m => m.stage === 'round2' && m.status === 'pending')) return null;
    const last = e.matches.filter(m => m.stage === 'round2').at(-1);
    return { stage: 'round2', status: 'waiting', seq: (last?.seq || 0) + 1, who_win: '0', player_a_id: last?.who_win === 'a' ? last.player_a_id : 0, player_b_id: last?.who_win === 'b' ? last.player_b_id : 0, player_a_name: last?.who_win === 'a' ? last.player_a_name : t('toConfirm'), player_b_name: last?.who_win === 'b' ? last.player_b_name : t('toConfirm') };
  }
  function matchMarkup(m, people) { const a = people.find(p => p.id === m.player_a_id) || { id: m.player_a_id, display_name: m.player_a_name }, b = people.find(p => p.id === m.player_b_id) || { id: m.player_b_id, display_name: m.player_b_name }; const decided = m.who_win === 'a' || m.who_win === 'b'; const resultLabel = !decided ? 'VS' : t(m.who_win === 'a' ? 'win' : 'lose'); const resultClass = !decided ? 'board-result-muted' : m.who_win === 'a' ? 'board-result-win' : 'board-result-lose'; return `<div class="board-match ${m.status === 'pending' ? 'is-pending' : ''}"><span class="board-match-no">#${m.seq}</span><div class="board-player ${m.who_win === 'a' ? 'is-winner' : m.who_win === 'b' ? 'is-loser' : ''}">${boardIdentity(a)}</div><span class="board-match-result" aria-label="${t('teamA')} · ${resultLabel}" title="${t('teamA')} · ${resultLabel}"><b class="${resultClass}">${resultLabel}</b></span><div class="board-player board-player-right ${m.who_win === 'b' ? 'is-winner' : m.who_win === 'a' ? 'is-loser' : ''}">${boardIdentity(b)}</div></div>`; }
  // 姓名和种族作为同一个整体，所有位置均按姓名、种族的顺序显示。
  const boardIdentity = player => `<span class="board-identity">${recordName(player)}${categoryIcon(player)}</span>`;
  function currentPlayerMarkup(e, player, stage, side) { const hasWin = playerHasWin(e, player.id); const hasLoss = e.status === 'round2' && stage === 'round2' && playerHasLoss(e, player.id, stage); const stateClass = hasWin ? 'is-record-winner' : 'is-record-new'; return `<div class="current-player ${side === 'b' ? 'current-player-right' : ''} ${stateClass} ${hasLoss ? 'is-lost' : ''}">${avatar(player)}<div><strong>${boardIdentity(player)}</strong></div></div>`; }
  function currentMatchMarkup(e, match, people) {
    if (match.stage === 'ace' && match.status === 'waiting') return `<section class="board-current-match"><div class="board-current-head"><span class="board-round-no">${t('ace')}</span><span>${t('awaitingAceDraw')}</span></div><div class="board-ace-waiting"><p>${t(editable(e) ? 'aceDrawReviewHint' : 'aceDrawWaitingHint')}</p>${editable(e) ? `<button type="button" class="primary accent" id="draw-ace"${state.drawingAce ? ' disabled' : ''}>${t('drawAce')}</button>` : ''}</div></section>`;
    const a = people.find(p => p.id === match.player_a_id) || { display_name: match.player_a_name }, b = people.find(p => p.id === match.player_b_id) || { display_name: match.player_b_name };
    const canRecord = editable(e) && !state.setupDraw && match.player_a_id && match.player_b_id && (match.status === 'pending' || (preMatch(e) && match.status === 'planned'));
    const result = side => canRecord ? `<button type="button" class="board-current-win" data-current-winner="${e.teams[side].id}" data-current-match="${match.id || 0}" data-player-a="${match.player_a_id}" data-player-b="${match.player_b_id}"${state.recordingResult || state.draw?.endAt > Date.now() ? ' disabled' : ''}>${t('recordWin')}</button>` : '';
    return `<section class="board-current-match" aria-label="${t('currentMatch')}"><div class="board-draw-roll" id="draw-roll" aria-live="polite" hidden><span>${t('drawing')}</span><div><strong id="draw-roll-a">${esc(a.display_name)}</strong><b>VS</b><strong id="draw-roll-b">${esc(b.display_name)}</strong></div></div><div class="board-current-fighters"><div class="board-current-side">${currentPlayerMarkup(e, a, match.stage, 'a')}${result(0)}</div><div class="board-current-vs">VS</div><div class="board-current-side">${currentPlayerMarkup(e, b, match.stage, 'b')}${result(1)}</div></div></section>`;
  }
  function aceSourceLabel(e) { return window.TBMatchDraw.sourceLabel(e.ace_draw_source || 'random', e.ace_draw_special, t); }
  function roundMarkup(round, matches, people, e) {
    const draw = round.stage === 'ace' && e ? window.TBMatchDraw.record(e, { t, esc, recordName }) : '';
    return `<section class="board-round" data-stage="${round.stage}"><div class="board-round-head"><div><span class="board-round-no">${t(round.stage)}</span><h2>${round.score_a} : ${round.score_b}</h2></div></div>${draw}<div class="board-matches">${matches.map(m => matchMarkup(m, people)).join('') || `<div class="board-empty">${t('noMatches')}</div>`}</div></section>`;
  }
  function aceRulesSummary(e, rules) {
    const players = new Map(e.teams.flatMap(team => team.players).concat(e.setup_roster || []).map(player => [player.id, player]));
    const specials = (rules.ace_specials || []).map((group, index) => {
      const split = Array.isArray(group.team_a_ids) || Array.isArray(group.team_b_ids);
      const sides = e.teams.map((team, side) => {
        const ids = split ? group[side === 0 ? 'team_a_ids' : 'team_b_ids'] || [] : group.player_ids || [];
        const names = ids.filter(id => !hasGrouping(e) || team.players.some(player => player.id === Number(id))).map(id => players.get(Number(id))).filter(Boolean).map(recordName);
        return `<span class="special-rule-side" aria-label="${t(side === 0 ? 'teamA' : 'teamB')}">${names.length ? names.join('、') : t('random')}</span>`;
      });
      return `<div class="ace-special-rule"><dt>${t('specialMatchGroup')}${index + 1} <b>${Number(group.probability) || 0}%</b></dt><dd class="special-rules-summary">${sides.join(`<span class="special-rule-versus">${t('matchupAgainst')}</span>`)}</dd></div>`;
    }).join('');
    const configured = Number(rules.ace_most_wins) + Number(rules.ace_most_losses) + (rules.ace_specials || []).reduce((sum, group) => sum + Number(group.probability || 0), 0);
    const odds = [['sourceMostWins', Number(rules.ace_most_wins) || 0], ['sourceMostLosses', Number(rules.ace_most_losses) || 0], ['fallback', Math.max(0, 100 - configured)]];
    return `<div class="board-rule-block ace-rule-block"><div class="ace-rule-heading"><h3>${t('ace')}</h3><div class="ace-rule-odds">${odds.map(([label, probability]) => `<dl><dt>${t(label)}</dt><dd>${probability}%</dd></dl>`).join('')}</div></div>${specials ? `<dl class="ace-special-list">${specials}</dl>` : ''}</div>`;
  }
  function rulesSummary(e) { if (e.is_imported) return `<section class="board-rules"><div class="board-roster-head"><span>${t('eventRules')}</span></div><div class="board-rule-block board-imported-rule">${t('importedTournament')}</div></section>`; const rules = { ...defaultRules(), ...e.rules }; return `<section class="board-rules"><div class="board-roster-head"><span>${t('eventRules')}</span></div><div class="board-rule-block"><h3>${t('round1')}</h3><dl><div><dt>${t('target')}</dt><dd>${e.round1_target}</dd></div><div><dt>${t('round1Mode')}</dt><dd>${t(rules.round1_mode === 'random' ? 'random' : 'manual')}</dd></div><div><dt>${t('noRepeat')}</dt><dd>${t(rules.no_repeat ? 'enabled' : 'disabled')}</dd></div></dl></div><div class="board-rule-block"><h3>${t('round2')}</h3><dl><div><dt>${t('target')}</dt><dd>${e.round2_target}</dd></div><div><dt>${t('kofStarter')}</dt><dd>${t(rules.kof_starter === 'captain' ? 'captainStarter' : 'randomStarter')}</dd></div>${window.TBKOF.needsRevivalRule(hasGrouping(e) ? e.teams.map(team => team.players.filter(player => player.active !== false).length) : window.TBKOF.teamSizes((e.setup_roster || []).map(player => [player.id, player.fixed_team])), e.round2_target) ? `<div><dt>${t('kofRevivalNoRepeat')}</dt><dd>${t(rules.kof_revival_no_repeat ? 'enabled' : 'disabled')}</dd></div>` : ''}</dl></div>${aceRulesSummary(e, rules)}</section>`; }
  // 预抽对阵只用于展示，正式开赛后由比赛引擎按同一份排阵生成真实对局。
  function plannedMatches(e, stage) {
    if (!['round1', 'round2'].includes(stage) || terminal(e) || e.rounds.find(round => round.stage === stage)?.status === 'finished') return [];
    if (stage === 'round1' && preMatch(e) && e.rules.round1_mode === 'manual' && !e.teams.every(team => team.lineup_submitted)) return [];
    const existing = e.matches.filter(match => match.stage === stage);
    const positions = stage === 'round1' ? [...new Set(e.lineups.map(entry => entry.position))].sort((a, b) => a - b) : (e.kof_starters?.length === 2 ? [1] : []);
    return positions.filter(position => !existing.some(match => match.seq === position)).map(position => {
      const player = side => stage === 'round1' ? e.lineups.find(entry => entry.team_id === e.teams[side].id && entry.position === position)?.player_id : e.kof_starters.find(entry => entry.team_id === e.teams[side].id)?.player_id;
      return { stage, seq: position, player_a_id: player(0) || 0, player_b_id: player(1) || 0, player_a_name: t('toConfirm'), player_b_name: t('toConfirm'), who_win: '0', status: 'planned' };
    });
  }
  async function playSetupDraw(steps, e) {
    if (!manages(e)) return;
    const session = { id: e.id, progress: null };
    state.setupDraw = session;
    const active = () => state.setupDraw === session && state.event?.id === e.id && !state.editor && manages(state.event) && preMatch(state.event);
    try {
      await window.TBSetupDraw.play({ steps, event: e, translate: t, isActive: active, playerMarkup: player => `${avatar(player)}<div><strong>${boardIdentity(player)}</strong></div>`, render: progress => {
        session.progress = progress;
        $('#board-content').innerHTML = boardContent(state.event);
        bindBoardSettings();
      } });
    } finally {
      if (state.setupDraw === session) {
        state.setupDraw = null;
        if (!state.editor && state.event?.id === e.id) {
          const previous = $$('.board-current-side > .current-player').map(slot => ({ html: slot.innerHTML, className: slot.className, key: Number(slot.dataset.drawPlayer) || 0 }));
          boardPage();
          $$('.board-current-side > .current-player').forEach((slot, side) => {
            if (!previous[side]) return;
            const final = { html: slot.innerHTML, className: slot.className, key: Number(slot.querySelector('[data-player-record]')?.dataset.playerRecord) || 0 };
            window.TBMatchDraw.swapPlayer(slot, previous[side].html, previous[side].key, { animate: false, className: previous[side].className });
            requestAnimationFrame(() => { if (slot.isConnected) window.TBMatchDraw.swapPlayer(slot, final.html, final.key, { className: final.className }); });
          });
          $$('.board-matches,.board-roster-list').forEach(node => node.scrollTop = 0);
        }
      }
    }
  }
  function boardContent(e) {
    const teams = e.teams, people = teams.flatMap(team => team.players), rounds = e.rounds;
    const ungrouped = !hasGrouping(e);
    const setup = state.setupDraw?.id === e.id ? state.setupDraw.progress : null;
    const panel = stage => {
      const matches = e.matches.filter(match => match.stage === stage);
      const previews = plannedMatches(e, stage).filter(match => !setup || (stage === 'round1' ? match.seq <= setup.round1 : setup.kof));
      matches.push(...previews);
      const waiting = stage === 'round2' ? waitingKOFMatch(e) : null;
      if (waiting && !matches.some(match => match.seq === waiting.seq)) matches.push(waiting);
      if (hasGrouping(e) && stage === 'round1' && preMatch(e) && e.rules.round1_mode === 'manual' && !e.teams.every(team => team.lineup_submitted)) {
        const round = rounds.find(round => round.stage === stage);
        return `<section class="board-round"><div class="board-round-head"><div><span class="board-round-no">${t(stage)}</span><h2>${round.score_a} : ${round.score_b}</h2></div></div><div class="board-matches board-lineup-readiness">${teams.map(team => `<div class="board-empty" data-lineup-status="${team.id}">${teamName(e, team)} · ${t(team.lineup_submitted ? 'lineupSubmitted' : 'lineupWaiting')}</div>`).join('')}</div></section>`;
      }
      return roundMarkup(rounds.find(round => round.stage === stage), matches, people, e);
    };
    const pending = setup?.step ? { stage: setup.step.kind === 'group' ? 'setupGrouping' : setup.step.kind, player_a_id: setup.step.a, player_b_id: setup.step.b, player_a_name: t('toConfirm'), player_b_name: t('toConfirm') } : !terminal(e) ? (e.matches.find(match => match.status === 'pending' && match.stage === e.status) || waitingKOFMatch(e) || (e.status === 'ace' && !e.matches.some(match => match.stage === 'ace') ? { stage: 'ace', status: 'waiting' } : null) || (preMatch(e) ? plannedMatches(e, 'round1').find(match => match.player_a_id && match.player_b_id) : null)) : null;
    // 累计胜局颜色与KOF淘汰线独立：历史保留颜色，淘汰线只用于实时KOF。
    const roster = (team, side, unassigned = false) => `<aside class="board-roster board-roster-${side}"><div class="board-roster-head"><span>${unassigned ? t('unassignedPlayers') : esc(teamName(e, team))}</span><b>${team.players.filter(player => !setup || setup.visible.has(player.id)).length}</b></div><div class="board-roster-list" style="--roster-count:${Math.max(1, Math.ceil(team.players.filter(player => !setup || setup.visible.has(player.id)).length / (unassigned ? 2 : 1)))}">${team.players.filter(player => !setup || setup.visible.has(player.id)).map(player => {
      const lost = e.status === 'round2' && playerHasLoss(e, player.id, 'round2');
      const recordClass = playerHasWin(e, player.id) ? 'is-record-winner' : 'is-record-new';
      return `<div data-roster-player="${player.id}" class="board-roster-player ${lost ? 'is-lost' : ''} ${recordClass}"><div class="board-roster-profile">${avatar(player)}<span class="board-roster-name">${boardIdentity(player)}${player.is_captain ? `<small class="board-captain">${t('captain')}</small>` : ''}</span></div></div>`;
    }).join('')}</div></aside>`;
    const score = `<div class="board-score"><div><span>${t('teamA')}</span><strong>${rounds.filter(round => round.winner_team_id === teams[0].id).length}</strong></div><b>VS</b><div><span>${t('teamB')}</span><strong>${rounds.filter(round => round.winner_team_id === teams[1].id).length}</strong></div></div>`;
    return `<header class="board-top${pending ? ' has-current' : ''}">
      <div class="board-overview"><div class="board-title"><h1>${esc(e.name)}${deletedBadge(e)}</h1>${e.description?.trim() ? `<p class="board-subtitle">${esc(e.description)}</p>` : ''}<p><time>${esc(displayTime(e.start_time))}</time><span class="board-manager">${t('manager')}: <b>${esc(e.manager_name || '')}</b></span></p></div>
        <div class="board-overview-bottom">${score}${['final_confirm', 'completed', 'cancelled'].includes(e.status) ? `<span class="board-status">${t(e.status)}</span>` : ''}</div>
      </div>${pending ? currentMatchMarkup(e, pending, people) : ''}
    </header><div class="board-arena"><div class="board-center"><div class="board-records"><div class="board-columns"><div class="board-left">${panel('round1')}${panel('round2')}</div><div class="board-right">${panel('ace')}<div class="board-rosters${ungrouped ? ' is-ungrouped' : ''}">${ungrouped ? roster({ players: e.setup_roster || [] }, 'unassigned', true) : roster(teams[0], 'a') + roster(teams[1], 'b')}</div></div></div>${rulesSummary(e)}</div></div></div>`;
  }
  async function renderLineupPage(preserve = false) {
    const e = state.event;
    if (!canArrangeLineup(e)) { go('/live/' + e.id); return; }
    const privateLineup = await api('/api/tournaments/' + e.id + '/lineup');
    if (state.event?.id !== e.id || state.editor !== 'lineup') return;
    if (!preserve || state.lineupEvent !== e.id) { state.lineupDrafts = {}; state.lineupTeam = 0; state.lineupEvent = e.id; }
    const teams = lineupTeams(e);
    window.TBLineupPage.render({ root: app, event: { ...e, lineups: privateLineup.lineups }, teams, selectedTeam: state.lineupTeam, drafts: state.lineupDrafts, t, esc, person, picker, action, bindCommon,
      onTeam: id => { state.lineupTeam = id; renderLineupPage(true).catch(error => toast(error.message)); },
      onBack: () => go('/live/' + e.id),
      onSave: async (team, ids) => { await post('/api/tournaments/' + e.id + '/lineup/submit', { team_id: team, player_ids: ids }); toast(t('saved')); go('/live/' + e.id); }
    });
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
    $('#roster-actions')?.close();
    const e = state.event, manager = editable(e), toolbar = $('.board-toolbar');
    // 右上只保留统一的三横线观赛菜单；录分在当前对战区完成。
    $('.board-event-actions', toolbar).innerHTML = `${canArrangeLineup(e) ? `<button type="button" class="board-settings-button" id="board-lineup">${t('round1Lineup')}</button>` : ''}${manager ? `<button type="button" class="board-settings-button" id="board-settings">${t('eventSettings')}</button>` : ''}`;
    $('#board-settings')?.addEventListener('click', () => go('/live/' + state.event.id + '/setting'));
    $('#board-lineup')?.addEventListener('click', () => go('/live/' + state.event.id + '/lineup'));
    $('#draw-ace')?.addEventListener('click', action(async () => {
      if (state.drawingAce) return;
      state.drawingAce = true;
      try { await eventAction('/ace/draw'); }
      finally { state.drawingAce = false; const button = $('#draw-ace'); if (button) button.disabled = false; }
    }));
    $$('[data-current-winner]').forEach(button => button.addEventListener('click', action(async () => {
      if (state.recordingResult || state.setupDraw || (state.draw?.endAt > Date.now())) return;
      state.recordingResult = true;
      $$('[data-current-winner]').forEach(control => control.disabled = true);
      try {
        const id = Number(button.dataset.currentMatch), winner = Number(button.dataset.currentWinner);
        const playerA = Number(button.dataset.playerA), playerB = Number(button.dataset.playerB);
        const match = id ? e.matches.find(match => match.id === id) : plannedMatches(e, 'round1').find(match => match.player_a_id === playerA && match.player_b_id === playerB);
        if (!match) return;
        const people = e.teams.flatMap(team => team.players);
        if (!(await window.TBConfirm.result({ stage: t(match.stage), seq: match.seq, a: people.find(player => player.id === playerA)?.display_name || match.player_a_name, b: people.find(player => player.id === playerB)?.display_name || match.player_b_name, winner: winner === e.teams[0].id ? 'a' : 'b' }))) return;
        // 弹窗期间仍接收SSE；只允许提交打开弹窗时的同一场、同一组对手。
        const current = state.event;
        const latest = current?.id === e.id && (id ? current.matches.find(match => match.id === id && match.status === 'pending' && match.stage === current.status) : preMatch(current) && plannedMatches(current, 'round1').find(match => match.seq === 1));
        if (!latest || !editable(current) || latest.player_a_id !== playerA || latest.player_b_id !== playerB) { toast(t('resultChanged')); return; }
        await eventAction(id ? '/matches/' + id + '/result' : '/opening-result', { winner_team_id: winner, player_a_id: playerA, player_b_id: playerB });
      } finally {
        state.recordingResult = false;
        $$('[data-current-winner]').forEach(control => control.disabled = state.draw?.endAt > Date.now());
      }
    })));
    if (!state.setupDraw && hasRoster(e)) window.TBKOFBoard.bindRows({ root: $('#board-content'), event: state.event, user: state.user, manager: manages(state.event), t, action, onVote: voteForCaptain, onCaptain: assignCaptain });
    if (state.event.status === 'round2' && canOperate(state.event) && !state.setupDraw) renderKOFSelection(state.event, manages(state.event)).catch(error => toast(error.message));
    if (state.setupDraw) $('#board-content')?.setAttribute('data-setup-drawing', 'true');
  }
  function applyEventBackground(e) {
    const board = $('.broadcast-board');
    if (!board) return;
    if (e?.background_url) {
      board.dataset.hasBackground = 'true';
      board.style.setProperty('--event-background-image', `url("${e.background_url}")`);
    } else {
      delete board.dataset.hasBackground;
      board.style.removeProperty('--event-background-image');
    }
  }
  // 透明度是当前浏览器的观赛偏好，不修改赛事资料或广播给其他观众。
  function readBoardTransparency() {
    try {
      const saved = localStorage.getItem('tb_board_transparency');
      if (saved === null || saved.trim() === '' || !Number.isFinite(Number(saved))) return 30;
      return Math.max(0, Math.min(100, Math.round(Number(saved))));
    } catch (_) { return 30; }
  }
  function bindBoardTransparency() {
    const board = $('.broadcast-board'), slider = $('#board-transparency'), output = $('#board-transparency-value');
    if (!board || !slider) return;
    const apply = () => {
      board.style.setProperty('--board-panel-opacity', String((100 - state.boardTransparency) / 100));
      slider.value = String(state.boardTransparency);
      output.value = state.boardTransparency + '%';
      slider.setAttribute('aria-valuetext', output.value);
    };
    slider.addEventListener('input', () => {
      state.boardTransparency = Number(slider.value); apply();
      try { localStorage.setItem('tb_board_transparency', String(state.boardTransparency)); } catch (_) {}
    });
    apply();
  }
  function boardPage() {
    const e = state.event;
    app.innerHTML = `<main class="broadcast-board" data-variant="${boardVariant(e)}"><div class="board-toolbar"><div class="board-toolbar-actions board-event-actions"></div><details class="board-menu"><summary aria-label="${t('displaySettings')}" title="${t('displaySettings')}">☰</summary><div>${picker()}<fieldset class="board-contrast-setting"><legend>${t('contrastSettings')}</legend><label for="board-transparency">${t('panelTransparency')}<output id="board-transparency-value" for="board-transparency">${state.boardTransparency}%</output></label><input id="board-transparency" type="range" min="0" max="100" step="1" value="${state.boardTransparency}"></fieldset><button data-go="/">${t('homeLink')}</button>${!state.user ? `<button data-go="/login?return=/live/${e.id}">${t('openLogin')}</button>` : ''}<span id="connection-state" class="help">${terminal(e) ? t('readOnly') : t(state.connection || 'reconnecting')}</span></div></details></div><div id="board-content">${boardContent(e)}</div></main>`;
    applyEventBackground(e); bindBoardTransparency(); bindCommon(); bindBoardSettings();
  }
  function currentPending(event) {
    if (!event || terminal(event)) return null;
    return event.matches.find(match => match.status === 'pending' && match.stage === event.status) || waitingKOFMatch(event);
  }
  function drawCandidates(event, side, previous, next) {
    const team = event.teams[side];
    if (!team) return [];
    const id = side ? next.player_b_id : next.player_a_id;
    const final = team.players.filter(player => player.id === id);
    const players = team.players.filter(player => player.active !== false);
    if (next.stage === 'round1') {
      // 已排定的选手直接进入当前对战区，只有未排定的名额才需要转盘。
      if (event.lineups.some(entry => entry.team_id === team.id && entry.position === next.seq)) return final;
      const matches = event.matches.filter(match => match.stage === 'round1' && match.seq < next.seq);
      const last = matches.at(-1), lastID = side ? last?.player_b_id : last?.player_a_id;
      const eligible = players.filter(player => !event.rules.no_repeat || players.length === 1 || player.id !== lastID);
      const count = player => matches.filter(match => (side ? match.player_b_id : match.player_a_id) === player.id).length;
      const minimum = Math.min(...eligible.map(count));
      return eligible.filter(player => count(player) === minimum);
    }
    if (next.stage === 'round2') {
      if (next.seq === 1) return event.rules.kof_starter === 'captain' ? final : players;
      const finished = event.matches.filter(match => match.stage === 'round2' && match.seq < next.seq && match.status === 'finished');
      const last = finished.at(-1);
      const lost = player => finished.some(match => (side ? match.player_b_id : match.player_a_id) === player.id && match.who_win === (side ? 'a' : 'b'));
      const lostLast = last && last.who_win === (side ? 'a' : 'b');
      if (!lostLast || event.round2_target <= players.length || !players.every(lost)) return final;
      if (!event.rules.kof_revival_no_repeat) return players;
      const eligible = window.TBKOF.revivalCandidates(players.map(player => player.id), finished.map(match => ({ a_id: match.player_a_id, b_id: match.player_b_id, who_win: match.who_win })), side ? 'b' : 'a');
      return players.filter(player => eligible.includes(player.id));
    }
    if (next.stage === 'ace') {
      if (event.ace_draw?.candidate_ids?.length === 2) {
        const ids = event.ace_draw.candidate_ids[side];
        return players.filter(player => ids.includes(player.id));
      }
      const source = event.ace_draw_source;
      if (source === 'special') {
        const group = event.rules.ace_specials[(event.ace_draw_special || 1) - 1];
        const ids = group?.[side ? 'team_b_ids' : 'team_a_ids'] || group?.player_ids || [];
        const special = players.filter(player => ids.includes(player.id));
        return special.length ? special : players;
      }
      if (source === 'most_wins' || source === 'most_losses') {
        const record = player => {
          let wins = 0, losses = 0;
          for (const match of event.matches.filter(match => match.stage !== 'ace' && match.status === 'finished')) {
            const win = match.player_a_id === player.id ? 'a' : match.player_b_id === player.id ? 'b' : '';
            if (win) { if (match.who_win === win) wins += 1; else losses += 1; }
          }
          return { player, wins, diff: source === 'most_wins' ? wins - losses : losses - wins };
        };
        const records = players.map(record).sort((a, b) => b.diff - a.diff || (source === 'most_wins' ? b.wins - a.wins : a.wins - b.wins));
        return records.filter(item => item.diff === records[0].diff && item.wins === records[0].wins).map(item => item.player);
      }
    }
    return players;
  }
  function startDrawAnimation(previous, next) {
    if (state.setupDraw) return;
    // 仅管理员在状态推进时播放抽签，首次进入、刷新和重新连接均不补播。
    if (!previous || previous.id !== next.id || !manages(next)) return;
    const match = currentPending(next);
    if (!match || match.status !== 'pending' || !['round1', 'round2', 'ace'].includes(match.stage)) return;
    // 开赛及进入KOF时沿用赛前已公开的抽签，不把同一组人选再滚动一次。
    const announced = plannedMatches(previous, match.stage).find(item => item.seq === match.seq);
    if (announced?.player_a_id === match.player_a_id && announced?.player_b_id === match.player_b_id) return;
    const key = `${next.id}:${match.id}:${match.stage}:${match.seq}:${match.player_a_id}:${match.player_b_id}`;
    const continuing = state.draw?.key === key && state.draw.endAt > Date.now();
    // 提交响应和SSE可能先后返回，同一对局只播放一次，重绘沿用剩余时间。
    if (state.draw?.key === key && !continuing) return;
    const oldMatch = currentPending(previous);
    // 等待报名的占位对局和正式待确认对局可以使用同一个序号，只有旧状态本身已经是待确认时才跳过。
    if (!continuing && oldMatch && oldMatch.status === 'pending' && oldMatch.stage === match.stage && oldMatch.seq >= match.seq) return;
    const roll = $('#draw-roll');
    if (!roll) return;
    if (state.drawTimer) clearInterval(state.drawTimer);
    state.drawToken += 1;
    const token = state.drawToken, people = next.teams.flatMap(team => team.players);
    const finals = [match.player_a_id, match.player_b_id].map(id => people.find(player => player.id === id));
    const candidates = continuing ? state.draw.candidates : [drawCandidates(next, 0, previous, match), drawCandidates(next, 1, previous, match)];
    const plan = continuing ? state.draw.plan : window.TBMatchDraw.plan(next, match, candidates);
    const startedAt = continuing ? state.draw.startedAt : Date.now();
    const endAt = startedAt + plan.duration;
    state.draw = { key, candidates, plan, startedAt, endAt };
    const board = $('.broadcast-board');
    const slots = $$('.board-current-side > .current-player');
    let firstPaint = true;
    const paintPlayers = players => {
      players.forEach((player, side) => {
        if (!slots[side]) return;
        const template = document.createElement('template');
        const value = player || { display_name: t('toConfirm') };
        template.innerHTML = currentPlayerMarkup(next, value, match.stage, side ? 'b' : 'a');
        const content = template.content.firstElementChild;
        window.TBMatchDraw.swapPlayer(slots[side], content.innerHTML, player?.id || 0, { animate: !firstPaint, className: content.className });
      });
      firstPaint = false;
    };
    const caption = html => {
      if (roll.innerHTML === html) return;
      roll.innerHTML = html;
      roll.animate([{ opacity: .3, transform: 'translateY(4px)' }, { opacity: 1, transform: 'translateY(0)' }], { duration: 160, easing: 'ease-out' });
    };
    const update = () => {
      // 导航、纠错或人选被覆盖后，旧动画不能继续占住新对战区。
      const current = currentPending(state.event);
      if (!roll.isConnected || state.editor || !manages(state.event) || current?.id !== match.id || current.player_a_id !== match.player_a_id || current.player_b_id !== match.player_b_id) {
        clearInterval(state.drawTimer); state.drawTimer = null; state.draw.endAt = 0;
        board?.classList.remove('is-drawing', 'is-drawing-rule');
        return;
      }
      const frame = window.TBMatchDraw.frame(plan, Date.now() - startedAt);
      board.classList.toggle('is-drawing', !!frame);
      board.classList.toggle('is-drawing-rule', frame?.kind === 'rule');
      $$('[data-current-winner]').forEach(button => button.disabled = !!frame || state.recordingResult);
      if (!frame) {
        if (plan.duration) paintPlayers(finals);
        clearInterval(state.drawTimer); state.drawTimer = null; roll.hidden = true;
        return;
      }
      roll.hidden = false;
      const step = `${frame.index + 1}/${plan.phases.length}`;
      roll.dataset.phase = frame.kind;
      if (frame.kind === 'rule') {
        const source = frame.choices[frame.tick % frame.choices.length];
        paintPlayers([null, null]);
        const label = frame.settling ? aceSourceLabel(next) : window.TBMatchDraw.sourceLabel(source, 0, t);
        caption(`<span>${t('aceRuleStep')} · ${step}</span><div class="draw-rule-name">${esc(label)}</div>`);
      } else {
        paintPlayers(candidates.map((list, side) => frame.settling || list.length <= 1 ? finals[side] : list[frame.tick % list.length]));
        const title = match.stage === 'ace' ? `${t('aceCandidateStep')} · ${step} · ${aceSourceLabel(next)}` : t('drawing');
        caption(`<span>${esc(title)}</span>`);
      }
    };
    update();
    if (Date.now() < endAt && !roll.hidden) state.drawTimer = setInterval(() => { if (token === state.drawToken) update(); }, 250);
  }
  async function refreshEvent() { const previous = state.event; state.event = await api('/api/public/tournaments/' + state.event.id); if (!state.editor) { boardPage(); startDrawAnimation(previous, state.event); } }
  async function eventAction(path, body = {}, method = 'POST') { await post('/api/tournaments/' + state.event.id + path, body, method); await refreshEvent(); }
  async function voteForCaptain(id) { await eventAction('/captain-votes', { target_id: id }); toast(t('voteSaved')); }
  async function assignCaptain(team, player) { await eventAction('/captains/select', { team_id: team, player_id: player }); }
  async function renderKOFSelection(e, manager) {
    const target = $('.board-rosters');
    if (!target || !canOperate(e) || e.status !== 'round2') return;
    const choices = await api('/api/tournaments/' + e.id + '/kof/options');
    if (!target.isConnected || state.event?.id !== e.id || state.event.status !== 'round2' || !canOperate(state.event)) return;
    const paint = () => window.TBKOFBoard.render({ root: $('#board-content'), event: state.event, choices, user: state.user, manager, t, action, onVote: voteForCaptain, onCaptain: assignCaptain,
      onSend: async body => {
        try { await eventAction(choices.match_id ? '/kof/next-player' : '/kof/starter', body); toast(t('reservationSaved')); }
        catch (error) { await renderKOFSelection(state.event, manages(state.event)); throw error; }
      }
    });
    const snapshot = JSON.stringify(choices) + ':' + state.language;
    if (target.dataset.options === snapshot) return;
    target.dataset.options = snapshot; paint();
  }
  async function confirmEventEnd() {
    const e = state.event;
    if (!editable(e) || e.status !== 'final_confirm' || state.confirmingEnd) return;
    const results = event => JSON.stringify(event.matches.map(match => [match.id, match.status, match.winner_team_id]));
    const snapshot = results(e);
    state.confirmingEnd = true;
    try {
      if (!(await window.TBConfirm.ask(t('confirmEndHint'), { title: t('confirmEnd') }))) return;
      // 确认期间仍接收推送，录分已改变时必须重新核对，不能封存旧结果。
      const current = state.event;
      if (current?.id !== e.id || !editable(current) || current.status !== 'final_confirm' || results(current) !== snapshot) { toast(t('resultChanged')); return; }
      await post('/api/tournaments/' + e.id + '/confirm');
      state.editor = null;
      history.replaceState({}, '', '/live/' + e.id);
      await refreshEvent();
    } finally { state.confirmingEnd = false; }
  }
  async function openEvent(id, preserve = false, setting = false) {
    if (state.event?.id !== Number(id)) state.event = null;
    state.event = await api('/api/public/tournaments/' + id);
    if (!canWatch(state.event) && !setting) {
      if (state.user && manages(state.event)) {
        history.replaceState({}, '', '/live/' + id + '/setting');
        state.editor = 'settings';
        return eventEditor('settings', preserve);
      }
      throw new Error(t('eventRosterRequired'));
    }
    if (setting) state.editor = 'settings';
    if (state.editor === 'lineup') await renderLineupPage(preserve);
    else if (state.editor) await eventEditor(state.editor, preserve); else { boardPage(); startDrawAnimation(state.event, state.event); }
    if (terminal(state.event)) { closeStream(); return; }
    if (!canWatch(state.event)) return;
    if (state.stream) return;
    const stream = new EventSource('/api/public/tournaments/' + id + '/stream'); state.stream = stream;
    stream.onopen = () => { state.connection = 'connected'; if ($('#connection-state')) $('#connection-state').textContent = t('connected'); };
    stream.onerror = () => { state.connection = 'reconnecting'; if ($('#connection-state')) $('#connection-state').textContent = t('reconnecting'); };
    stream.onmessage = message => {
      const data = JSON.parse(message.data);
      if (data.unavailable) { const target = manages(state.event) ? '/live/' + id + '/setting' : '/'; closeStream(); go(target); return; }
      const previous = state.event; const changed = JSON.stringify(state.event) !== JSON.stringify(data); const variantChanged = boardVariant(state.event) !== boardVariant(data); const settingsLocked = canEditSettings(state.event) && !canEditSettings(data); state.event = data;
      if (state.editor === 'lineup') { if (!canArrangeLineup(data)) go('/live/' + id); }
      else if (state.editor && (settingsLocked || (!hasGrouping(previous) && hasGrouping(data)))) { eventEditor(state.editor).catch(error => toast(error.message)); }
      if (variantChanged && !state.editor) { boardPage(); }
      if (changed && !state.editor) applyEventBackground(data);
      if (changed && state.editor) {
        const backgroundName = $('.event-background-panel .panel-head span');
        if (backgroundName) backgroundName.textContent = data.background_name || t('noBackground');
        refreshResultCorrection(data);
        refreshEventManagement(data);
      }
      if (changed && !state.editor && $('#board-content') && !state.setupDraw) { $('#board-content').innerHTML = boardContent(data); applyEventBackground(data); bindBoardSettings(); startDrawAnimation(previous, data); }
      if (terminal(data)) { closeStream(); if (!state.editor) boardPage(); }
    };
  }

  async function archivePage(preserve = false) {
    const [directory, backgrounds] = await Promise.all([api('/api/users'), api('/api/backgrounds')]);
    if (!preserve || !state.archiveDraft) state.archiveDraft = window.TBArchivePage.create(localTime());
    window.TBArchivePage.render({
      draft: state.archiveDraft, managers: directory.users.filter(user => user.status === 'active'), users: directory.users.filter(user => user.role !== 'superadmin'), backgrounds: backgrounds.backgrounds || [],
      t, esc, field, input, startTimeInput, startTimeValue, backgroundOptions, shell, action, toast, person,
      onSave: async body => { const result = await post('/api/tournaments/archive', body); state.archiveDraft = null; go('/live/' + result.id); }
    });
  }
  async function renderRoute(preserve = false) {
    window.TBDatePicker.destroyAll();
    document.documentElement.lang = state.language;
    const path = location.pathname;
    if (path.startsWith('/live/')) {
      const parts = path.split('/').filter(Boolean);
      if (parts[2] === 'lineup') state.editor = 'lineup';
      return openEvent(parts[1], preserve, parts[2] === 'setting');
    }
    closeStream(); state.event = null;
    if (path === '/login') return loginPage();
    const page = pageForPath(path);
    if (page === 'home') return homePage();
    if (!page) return go('/');
    if (!state.user) return go('/login?return=' + encodeURIComponent(path + location.search));
    if (page === 'settings') return accountPage();
    if (state.user.role !== 'superadmin') return go('/');
    if (page === 'createEvent') return eventEditor('create', preserve);
    if (page === 'tournamentManagement') return tournamentManagementPage();
    if (page === 'accounts') return usersPage(path.split('/')[2] || '');
    if (page === 'siteSettings') return siteSettingsPage();
    if (page === 'backgrounds') return backgroundsPage();
    if (page === 'archive') return archivePage(preserve);
    return homePage();
  }
  window.addEventListener('popstate', () => { closeStream(); state.editor = null; renderRoute().catch(error => toast(error.message)); });
  // 由接口的401响应处理会话失效，不为检查登录状态发起轮询。
  // 人数不足时尚无公开SSE，设置页检查其他管理端是否已补全名单或抽签。
  let checkingGrouping = false;
  setInterval(async () => {
    if (checkingGrouping || state.stream || document.hidden || !state.editor || !state.event || hasRoster(state.event) || terminal(state.event)) return;
    const id = state.event.id, token = state.drawToken;
    checkingGrouping = true;
    try {
      const event = await api('/api/public/tournaments/' + id);
      if (state.drawToken === token && state.event?.id === id && state.editor && canWatch(event)) await openEvent(id, false, true);
    } catch (_) {} finally { checkingGrouping = false; }
  }, 3000);
  // 预报名不进入公开推送，赛事页名单旁单独刷新本队报名状态。
  let pollingKOF = false;
  setInterval(async () => {
    if (pollingKOF || document.hidden || state.editor || state.event?.status !== 'round2' || !canOperate(state.event) || !$('.board-rosters')) return;
    pollingKOF = true;
    try { await renderKOFSelection(state.event, manages(state.event)); } catch (_) {} finally { pollingKOF = false; }
  }, 3000);
  window.addEventListener('beforeunload', closeStream);
  (async () => { try { state.user = (await api('/api/auth/me')).user; } catch (_) {} await renderRoute(); })().catch(error => { app.innerHTML = `<main class="page"><p>${esc(error.message)}</p><button data-go="/">${t('homeLink')}</button></main>`; bindCommon(); });
})();
