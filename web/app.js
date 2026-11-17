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
  const preMatch = e => ['draft', 'captain_vote', 'lineup_confirm'].includes(e.status);
  const manages = e => state.user && (state.user.role === 'superadmin' || Number(state.user.id) === Number(e.manager_id));
  const editable = e => manages(e) && !terminal(e);
  const noScore = e => !(e.matches || []).some(m => m.status === 'finished');
  const person = u => `<span class="person">${avatar(u)}<span><strong>${esc(u.display_name)}</strong><small>${esc(u.username || '')}</small></span>${categoryIcon(u)}</span>`;
  const avatar = u => u.avatar_url ? `<img class="board-avatar" src="${esc(u.avatar_url)}" alt="">` : `<span class="board-avatar board-avatar-fallback">${esc((u.display_name || '?').slice(0, 1))}</span>`;
  const categoryIcon = u => u.category_icon ? `<img class="board-category-icon" src="${esc(u.category_icon)}" alt="${esc(u.category_name || '')}" title="${esc(u.category_name || '')}">` : '';
  const field = (label, html, full = false) => `<div class="field${full ? ' full' : ''}"><label>${t(label)}</label>${html}</div>`;
  const input = (name, value = '', type = 'text', attrs = '') => `<input name="${name}" type="${type}" value="${esc(value)}" ${attrs}>`;
  const options = (users, chosen = 0, empty = '') => (empty ? `<option value="0">${t(empty)}</option>` : '') + users.map(u => `<option value="${u.id}"${Number(chosen) === Number(u.id) ? ' selected' : ''}>${esc(u.display_name)}</option>`).join('');
  const localTime = () => { const date = new Date(); return new Date(date - date.getTimezoneOffset() * 60000).toISOString().slice(0, 13); };
  const displayTime = value => { const text = String(value || ''); return text ? `${text.slice(0, 10)} · ${text.slice(11, 13)} ${t('hour')}` : '—'; };
  function eventTimeInput(value = localTime()) { return `<div class="event-time-input"><input type="date" name="event_date" value="${esc(value.slice(0, 10))}" required aria-label="${t('date')}"><select name="event_hour" required aria-label="${t('hour')}">${Array.from({ length: 24 }, (_, hour) => `<option value="${String(hour).padStart(2, '0')}"${hour === Number(value.slice(11, 13)) ? ' selected' : ''}>${String(hour).padStart(2, '0')} ${t('hour')}</option>`).join('')}</select></div>`; }
  const eventTimeValue = form => `${form.get('event_date')}T${form.get('event_hour')}`;
  function toast(message) { const box = $('#toast'); box.textContent = message; box.classList.remove('hidden'); clearTimeout(state.toastTimer); state.toastTimer = setTimeout(() => box.classList.add('hidden'), 4000); }
  async function api(url, options = {}) { const headers = { ...options.headers }; if (options.body !== undefined && !(options.body instanceof FormData)) headers['Content-Type'] = 'application/json'; const res = await fetch(url, { ...options, headers }); const data = await res.json().catch(() => ({})); if (!res.ok) throw new Error(data.error || t('requestFailed')); return data; }
  const post = (url, body = {}, method = 'POST') => api(url, { method, body: JSON.stringify(body) });
  function action(fn) { return async event => { try { await fn(event); } catch (error) { toast(error.message); } }; }
  function picker() { return `<select class="language-picker" aria-label="Language">${Object.entries(langs).map(([key, name]) => `<option value="${key}"${state.language === key ? ' selected' : ''}>${name}</option>`).join('')}</select>`; }
  function closeStream() { state.stream?.close(); state.stream = null; }
  function pageURL(page) { return page === 'home' ? '/' : `/workspace?page=${page}`; }
  function go(url) { closeStream(); state.editor = null; state.ruleDraft = null; state.operations = false; history.pushState({}, '', url); renderRoute().catch(error => toast(error.message)); }
  // 语言只改变当前视图的文案，保留表单值和路由。
  function captureForms() { return $$('form[id]').map(form => ({ id: form.id, fields: $$('input,select,textarea', form).map(el => ({ name: el.name, type: el.type, value: el.value, checked: el.checked, selected: el.multiple ? [...el.selectedOptions].map(o => o.value) : null, files: el.type === 'file' ? el.files : null })) })); }
  function restoreForms(forms) { for (const data of forms) { const form = document.getElementById(data.id); if (!form) continue; const used = new Set(); for (const item of data.fields) { const el = $$('input,select,textarea', form).find(el => !used.has(el) && el.name === item.name && el.type === item.type); if (!el) continue; used.add(el); if (item.type === 'file') { try { el.files = item.files; } catch (_) {} } else if (item.selected) { for (const option of el.options) option.selected = item.selected.includes(option.value); } else { el.value = item.value; el.checked = item.checked; } } } }
  function bindCommon() {
    $$('[data-go]').forEach(el => el.addEventListener('click', () => go(el.dataset.go)));
    $$('.language-picker').forEach(el => el.addEventListener('change', action(async () => { const forms = captureForms(); if ($('#event-form') && $('[name=\"round1_mode\"]')) state.ruleDraft = readRulesDraft($('#event-form')); state.language = el.value; localStorage.setItem('tb_language', state.language); document.documentElement.lang = state.language; await renderRoute(true); restoreForms(forms); refreshSpecialCounts(); })));
    $('#logout')?.addEventListener('click', action(async () => { await post('/api/auth/logout'); state.user = null; go('/'); }));
  }
  function shell(content, page = 'home') {
    const admin = state.user?.role === 'superadmin';
    const nav = ['home', ...(admin ? ['createEvent', 'createUser', 'passwords', 'archive', 'siteSettings'] : []), ...(state.user ? ['settings'] : [])];
    app.innerHTML = `<div class="shell"><aside class="sidebar"><div class="brand"><div class="brand-mark">TB</div><div><strong>TEAM BATTLE</strong><small>${t('home')}</small></div></div><nav class="nav">${nav.map(key => `<button class="${page === key ? 'active' : ''}" data-go="${pageURL(key)}">${t(key)}</button>`).join('')}</nav>${state.user ? `<div class="side-footer">${person(state.user)}</div>` : ''}</aside><main class="main"><header class="topbar"><span>${t(page)}</span><div class="top-actions">${picker()}${state.user ? `<button class="ghost" id="logout">${t('logout')}</button>` : `<button class="outline" data-go="/login">${t('login')}</button>`}</div></header><div class="page">${content}</div></main></div>`;
    bindCommon();
  }
  function eventTable(events) { return events.length ? `<div class="table-wrap"><table class="table"><thead><tr><th>${t('name')}</th><th>${t('time')}</th><th>${t('status')}</th><th></th></tr></thead><tbody>${events.map(e => `<tr><td><strong>${esc(e.name)}</strong></td><td>${esc(displayTime(e.event_time))}</td><td><span class="tag ${terminal(e) ? 'done' : 'live'}">${t(e.status)}</span></td><td><button class="link" data-go="/live/${e.id}">${t('view')}</button></td></tr>`).join('')}</tbody></table></div>` : `<div class="empty">${t('noEvents')}</div>`; }
  async function homePage() {
    const query = new URLSearchParams(location.search); state.page = Math.max(1, Number(query.get('p') || 1)); state.filters = { name: query.get('name') || '', from: query.get('from') || '', to: query.get('to') || '' };
    const search = new URLSearchParams({ ...state.filters, page: state.page, page_size: 10 });
    const [past, live, mine] = await Promise.all([api('/api/public/history?' + search), api('/api/public/events'), state.user ? api('/api/events') : Promise.resolve({ events: [] })]);
    const publicIDs = new Set(live.events.map(e => e.id)); const own = mine.events.filter(e => !terminal(e) && !publicIDs.has(e.id));
    shell(`<div class="page-head"><h1>${t('home')}</h1></div>${live.events.length ? `<section class="panel section-gap"><div class="panel-head"><h2>${t('ongoing')}</h2></div>${eventTable(live.events)}</section>` : ''}${own.length ? `<section class="panel section-gap"><div class="panel-head"><h2>${t('myEvents')}</h2></div>${eventTable(own)}</section>` : ''}<section class="panel"><div class="panel-head"><h2>${t('history')}</h2><span>${t('total')} ${past.total}</span></div><form id="history-filter" class="filter-bar">${field('name', input('name', state.filters.name))}${field('from', input('from', state.filters.from, 'date'))}${field('to', input('to', state.filters.to, 'date'))}<button class="primary">${t('search')}</button><button type="button" class="outline" data-go="/">${t('clear')}</button></form>${eventTable(past.events)}<div class="pagination"><button class="outline" id="previous" ${state.page <= 1 ? 'disabled' : ''}>${t('previous')}</button><span>${t('page')} ${state.page} / ${Math.max(1, Math.ceil(past.total / 10))}</span><button class="outline" id="next" ${state.page * 10 >= past.total ? 'disabled' : ''}>${t('next')}</button></div></section>`);
    const filterURL = (page = 1, values = state.filters) => '/?' + new URLSearchParams({ ...values, p: page });
    $('#history-filter').addEventListener('submit', event => { event.preventDefault(); go(filterURL(1, Object.fromEntries(new FormData(event.target)))); });
    $('#previous').onclick = () => go(filterURL(state.page - 1)); $('#next').onclick = () => go(filterURL(state.page + 1));
  }
  function loginPage() {
    app.innerHTML = `<main class="login-shell"><div class="login-card"><div class="page-head"><h1>${t('login')}</h1>${picker()}</div><form id="login-form" class="login-form">${field('username', input('username', '', 'text', 'required autocomplete="username"'))}${field('password', input('password', '', 'password', 'required autocomplete="current-password"'))}<button class="primary accent">${t('login')}</button></form><p class="help">${t('initHint')}</p><button class="link" data-go="/">${t('homeLink')}</button></div></main>`; bindCommon();
    $('#login-form').onsubmit = action(async event => { event.preventDefault(); const form = new FormData(event.target); state.user = (await post('/api/auth/login', Object.fromEntries(form))).user; const target = new URLSearchParams(location.search).get('return'); go(target?.startsWith('/live/') ? target : '/'); });
  }
  async function accountPage() {
    const categories = (await api('/api/settings/catalog')).categories;
    shell(`<div class="page-head"><h1>${t('settings')}</h1></div><section class="panel"><form id="account-form" class="panel-body form-grid">${field('displayName', input('display_name', state.user.display_name, 'text', 'required'), true)}${field('newPassword', input('new_password', '', 'password', 'minlength="6" autocomplete="new-password"'))}${field('confirmPassword', input('new_password_confirm', '', 'password', 'autocomplete="new-password"'))}<p class="help full">${t('passwordHint')}</p>${field('category', `<div class="category-choices"><label><input type="radio" name="category_id" value="0"${!state.user.category_id ? ' checked' : ''}>${t('noCategory')}</label>${categories.map(c => `<label><input type="radio" name="category_id" value="${c.id}"${Number(c.id) === Number(state.user.category_id) ? ' checked' : ''}>${c.icon_url ? `<img src="${esc(c.icon_url)}" alt="">` : ''}${esc(c.name)}</label>`).join('')}</div>`, true)}${field('avatar', `<div class="avatar-upload"><div id="avatar-preview">${state.user.avatar_url ? `<img class="account-avatar-preview" src="${esc(state.user.avatar_url)}" alt="">` : `<span class="account-avatar-empty">${t('noAvatar')}</span>`}</div><div><input type="file" name="avatar" accept="image/jpeg,image/png,image/gif,image/webp"><p class="help">${t('imageHint')}</p></div></div>`, true)}<button class="primary accent">${t('save')}</button></form></section>`, 'settings');
    $('[name="avatar"]').onchange = event => { const file = event.target.files[0]; if (file) { const url = URL.createObjectURL(file); $('#avatar-preview').innerHTML = `<img class="account-avatar-preview" src="${url}" alt="">`; } };
    $('#account-form').onsubmit = action(async event => { event.preventDefault(); const form = new FormData(event.target); if (form.get('new_password') !== form.get('new_password_confirm')) throw Error(t('passwordMismatch')); state.user = (await post('/api/auth/profile', { display_name: form.get('display_name'), category_id: Number(form.get('category_id')), new_password: form.get('new_password'), new_password_confirm: form.get('new_password_confirm') }, 'PATCH')).user; const file = form.get('avatar'); if (file?.size) { const upload = new FormData(); upload.append('avatar', file); state.user = (await api('/api/auth/avatar', { method: 'POST', body: upload })).user; } toast(t('saved')); await accountPage(); });
  }
  async function usersPage(reset = false) {
    const users = (await api('/api/users')).users;
    const form = reset ? `${field('account', `<select name="user_id">${options(users.filter(u => u.role !== 'superadmin'))}</select>`)}${field('newPassword', input('password', '', 'password', 'required minlength="6"'))}${field('confirmPassword', input('confirm', '', 'password', 'required'))}` : `${field('username', input('username', '', 'text', 'required'))}${field('displayName', input('display_name'))}${field('password', input('password', '', 'password', 'required minlength="6"'))}${field('role', `<select name="role"><option value="user">${t('user')}</option><option value="superadmin">${t('superadmin')}</option></select>`)}`;
    shell(`<div class="page-head"><h1>${t(reset ? 'passwords' : 'createUser')}</h1></div><section class="panel"><form id="user-form" class="form-grid panel-body">${form}<button class="primary accent">${t('save')}</button>${reset ? `<p class="help full">${t('resetHint')}</p>` : ''}</form></section>${!reset ? `<section class="panel section-gap"><div class="table-wrap"><table class="table"><thead><tr><th>${t('account')}</th><th>${t('role')}</th><th>${t('category')}</th></tr></thead><tbody>${users.map(u => `<tr><td>${person(u)}</td><td>${t(u.role)}</td><td>${esc(u.category_name || '—')}</td></tr>`).join('')}</tbody></table></div></section>` : ''}`, reset ? 'passwords' : 'createUser');
    $('#user-form').onsubmit = action(async event => { event.preventDefault(); const form = new FormData(event.target); if (reset) { if (form.get('password') !== form.get('confirm')) throw Error(t('passwordMismatch')); await post('/api/users/' + form.get('user_id') + '/password', { password: form.get('password') }, 'PATCH'); } else await post('/api/users', Object.fromEntries(form)); toast(t(reset ? 'saved' : 'accountCreated')); await usersPage(reset); });
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
    return `<section class="panel section-gap"><div class="panel-head"><h2>${t('players')}</h2><span id="roster-count"></span></div><div class="panel-body"><p class="help">${t('rosterHint')}</p><div class="roster-tools"><input id="player-search" placeholder="${t('searchPlayers')}"><button type="button" class="outline" id="all-random">${t('allRandom')}</button></div><div class="roster-summary" id="roster-summary"></div><div class="roster-cards">${users.map(u => `<article class="roster-card" data-player-card="${u.id}" data-search="${esc((u.username + ' ' + u.display_name + ' ' + (u.category_name || '')).toLowerCase())}">${person(u)}<div class="team-switch">${['', 'R', 'A', 'B'].map(value => `<label><input type="radio" name="pick_${u.id}" value="${value}"${(assignments[u.id] || '') === value ? ' checked' : ''}><span>${value === '' ? t('notSelected') : value === 'R' ? t('randomTeam') : t(value === 'A' ? 'teamA' : 'teamB')}</span></label>`).join('')}</div></article>`).join('') || `<div class="empty">${t('emptyPlayers')}</div>`}</div></div></section>`;
  }
  function rosterValue(form) { const formData = new FormData(form); const ids = [], fixed = {}; for (const user of state.users) { const pick = formData.get('pick_' + user.id); if (pick) { ids.push(user.id); if (pick !== 'R') fixed[user.id] = pick; } } return { player_ids: ids, fixed_assignments: fixed }; }
  function bindRoster(form) {
    const update = () => { const roster = rosterValue(form); state.roster = Object.fromEntries(roster.player_ids.map(id => [id, roster.fixed_assignments[id] || 'R'])); const count = team => Object.values(state.roster).filter(v => v === team).length; $('#roster-count').textContent = `${t('selected')} ${roster.player_ids.length} / 12`; $('#roster-summary').innerHTML = ['A', 'B', 'R'].map(team => `<span class="roster-total ${team.toLowerCase()}">${t(team === 'A' ? 'teamA' : team === 'B' ? 'teamB' : 'unassigned')} <b>${count(team)}</b></span>`).join(''); $$('[data-player-card]').forEach(card => { card.dataset.team = state.roster[card.dataset.playerCard] || ''; });
      $$('[data-special-candidates]', form).forEach(container => {
        const selected = $$('input:checked', container).map(el => Number(el.value));
        container.innerHTML = specialCandidatesMarkup(selected);
      });
      refreshSpecialCounts();
    };
    $$('[name^="pick_"]', form).forEach(el => el.addEventListener('change', update));
    $('#player-search').oninput = event => { const text = event.target.value.trim().toLowerCase(); $$('[data-player-card]').forEach(card => card.hidden = !card.dataset.search.includes(text)); };
    $('#all-random').onclick = () => { for (const user of state.users) if (state.roster[user.id]) $(`[name="pick_${user.id}"][value="R"]`, form).checked = true; update(); };
    update();
  }
  const defaultRules = () => ({ round1_mode: 'manual', kof_starter: 'random', no_repeat: true, all_first: true, ace_most_wins: 20, ace_most_losses: 20, ace_specials: [] });
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
    return `<section class="panel section-gap"><div class="panel-head"><h2>${t('round1')}</h2></div><div class="panel-body form-grid">${field('round1Target', input('round1_target', e.round1_target || 4, 'number', 'min="1" max="99" required'))}${field('round1Mode', `<select name="round1_mode"><option value="manual"${rules.round1_mode === 'manual' ? ' selected' : ''}>${t('manual')}</option><option value="random"${rules.round1_mode === 'random' ? ' selected' : ''}>${t('random')}</option></select>`)}<label class="check"><input name="no_repeat" type="checkbox"${rules.no_repeat ? ' checked' : ''}>${t('noRepeat')}</label><label class="check"><input name="all_first" type="checkbox"${rules.all_first ? ' checked' : ''}>${t('allFirst')}</label></div></section><section class="panel section-gap"><div class="panel-head"><h2>${t('round2')}</h2></div><div class="panel-body form-grid">${field('round2Target', input('round2_target', e.round2_target || 5, 'number', 'min="1" max="99" required'))}${field('kofStarter', `<div class="mode-choices">${['random', 'captain'].map(mode => `<label><input type="radio" name="kof_starter" value="${mode}"${rules.kof_starter === mode ? ' checked' : ''}><span>${t(mode === 'random' ? 'randomStarter' : 'captainStarter')}</span></label>`).join('')}</div>`)}<p class="help full">${t('kofHint')}</p></div></section><section class="panel section-gap"><div class="panel-head"><h2>${t('aceRules')}</h2><span id="fallback-probability"></span></div><div class="panel-body"><div class="form-grid">${field('mostWins', input('ace_most_wins', rules.ace_most_wins, 'number', 'min="0" max="100" required'))}${field('mostLosses', input('ace_most_losses', rules.ace_most_losses, 'number', 'min="0" max="100" required'))}</div><p class="help">${t('aceHint')}</p><div id="special-rules">${rules.ace_specials.map(specialMarkup).join('')}</div><button type="button" class="outline" id="add-special">${t('addSpecial')}</button></div></section>`;
  }
  function readRulesDraft(form) {
    const f = new FormData(form);
    return { round1_mode: f.get('round1_mode'), kof_starter: f.get('kof_starter'), no_repeat: f.has('no_repeat'), all_first: f.has('all_first'), ace_most_wins: Number(f.get('ace_most_wins')), ace_most_losses: Number(f.get('ace_most_losses')), ace_specials: $$('.special-rule', form).map(row => ({ probability: Number($('[name="special_probability"]', row).value), player_ids: $$('input[type=checkbox]:checked', row).map(el => Number(el.value)) })) };
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
  async function eventEditor(mode = 'create', preserve = false) {
    const source = mode === 'create' ? {} : state.event;
    const e = preserve && state.ruleDraft ? { ...source, rules: state.ruleDraft } : source;
    const [users, catalog] = await Promise.all([mode === 'create' ? api('/api/users') : api('/api/events/' + e.id + '/eligible-users'), state.user.role === 'superadmin' ? api('/api/site-settings') : Promise.resolve({ event_names: [] })]);
    state.users = users.users.filter(u => u.role !== 'superadmin');
    if (!preserve) state.roster = Object.fromEntries((e.teams || []).flatMap((team, i) => team.players.map(p => [p.id, i ? 'B' : 'A'])));
    const basic = mode !== 'roster' ? `<section class="panel"><div class="panel-body form-grid">${field('name', '<input name="name" list="event-names" value="' + esc(e.name || '') + '" required><datalist id="event-names">' + catalog.event_names.map(n => '<option value="' + esc(n.name) + '">').join('') + '</datalist>', true)}${field('time', eventTimeInput(e.event_time || localTime()))}${mode === 'create' ? field('manager', `<select name="manager_id"><option value="0">${t('managerDefault')}</option>${options(state.users)}</select>`) : ''}${field('description', `<textarea name="description" rows="2">${esc(e.description || '')}</textarea>`, true)}</div></section>` : '';
    const content = `<div class="page-head"><div><h1>${t(mode === 'create' ? 'createEvent' : mode === 'roster' ? 'editRoster' : 'eventSettings')}</h1><p class="subtitle">${t(mode === 'create' ? 'requiredHint' : mode === 'roster' ? 'rosterResetHint' : 'settingsResetHint')}</p></div>${mode === 'create' ? '' : `<button class="outline" id="back-board">${t('close')}</button>`}</div><form id="event-form">${basic}${mode === 'settings' ? '' : rosterMarkup(state.users, state.roster)}${mode === 'roster' ? '' : rulesMarkup(e)}<div class="form-footer"><button class="primary accent">${t(mode === 'create' ? 'createEvent' : 'save')}</button></div></form>`;
    if (mode === 'create') shell(content, 'createEvent'); else { app.innerHTML = `<main class="event-editor-page"><div class="editor-language">${picker()}</div>${content}</main>`; bindCommon(); $('#back-board').onclick = () => { state.editor = null; boardPage(); }; }
    if (mode !== 'settings') bindRoster($('#event-form'));
    if (mode !== 'roster') bindRules($('#event-form'));
    $('#event-form').onsubmit = action(async event => { event.preventDefault(); const form = event.target, f = new FormData(form); const body = mode === 'roster' ? rosterValue(form) : { name: f.get('name'), event_time: eventTimeValue(f), description: f.get('description'), manager_id: Number(f.get('manager_id') || 0), round1_target: Number(f.get('round1_target')), round2_target: Number(f.get('round2_target')), rules: rulesValue(form), ...(mode === 'create' ? rosterValue(form) : {}) }; if (mode === 'create') { const created = await post('/api/events', body); toast(t('eventCreated')); go('/live/' + created.id); } else { await post('/api/events/' + e.id + (mode === 'roster' ? '/players' : '/settings'), body, mode === 'roster' ? 'POST' : 'PATCH'); state.editor = null; await refreshEvent(); toast(t('saved')); } });
  }

  function playerHasWin(e, playerID, stage) { return e.matches.some(m => m.stage === stage && m.status === 'finished' && ((m.player_a_id === playerID && m.who_win === 'a') || (m.player_b_id === playerID && m.who_win === 'b'))); }
  function playerHasLoss(e, playerID, stage) { return e.matches.some(m => m.stage === stage && m.status === 'finished' && ((m.player_a_id === playerID && m.who_win === 'b') || (m.player_b_id === playerID && m.who_win === 'a'))); }
  function matchMarkup(m, people) { const a = people.find(p => p.id === m.player_a_id) || { display_name: m.player_a_name }, b = people.find(p => p.id === m.player_b_id) || { display_name: m.player_b_name }; const decided = m.who_win === 'a' || m.who_win === 'b'; const resultLabel = (side) => !decided ? '—' : m.who_win === side ? t('win') : t('lose'); const resultClass = (side) => !decided ? 'board-result-muted' : m.who_win === side ? 'board-result-win' : 'board-result-lose'; return `<div class="board-match ${m.status === 'pending' ? 'is-pending' : ''}"><span class="board-match-no">#${m.seq}</span><div class="board-player ${m.who_win === 'a' ? 'is-winner' : m.who_win === 'b' ? 'is-loser' : ''}"><span>${esc(a.display_name)}</span>${categoryIcon(a)}</div><span class="board-match-result"><b class="${resultClass('a')}">${resultLabel('a')}</b><span class="board-vs">VS</span><b class="${resultClass('b')}">${resultLabel('b')}</b></span><div class="board-player board-player-right ${m.who_win === 'b' ? 'is-winner' : m.who_win === 'a' ? 'is-loser' : ''}">${categoryIcon(b)}<span>${esc(b.display_name)}</span></div></div>`; }
  function currentPlayerMarkup(e, player, stage, side) { const hasWin = playerHasWin(e, player.id, stage); const hasLoss = stage === 'round2' && playerHasLoss(e, player.id, stage); const stateClass = hasWin ? 'is-record-winner' : 'is-record-new'; return `<div class="current-player ${side === 'b' ? 'current-player-right' : ''} ${stateClass} ${hasLoss ? 'is-lost' : ''}">${side === 'a' ? avatar(player) : ''}<div><strong>${esc(player.display_name)}</strong>${categoryIcon(player)}</div>${side === 'b' ? avatar(player) : ''}</div>`; }
  function currentMatchMarkup(e, match, people) { const a = people.find(p => p.id === match.player_a_id) || { display_name: match.player_a_name }, b = people.find(p => p.id === match.player_b_id) || { display_name: match.player_b_name }; return `<section class="board-current-match"><div class="board-current-head"><span class="board-round-no">${t(match.stage)}</span><span>${t('currentMatch')}</span></div><div class="board-current-fighters">${currentPlayerMarkup(e, a, match.stage, 'a')}<div class="board-current-vs">VS</div>${currentPlayerMarkup(e, b, match.stage, 'b')}</div></section>`; }
  function roundMarkup(round, matches, people) { return `<section class="board-round"><div class="board-round-head"><div><span class="board-round-no">${t(round.stage)}</span><h2>${round.score_a} : ${round.score_b}</h2></div><span class="board-target">${t('target')} ${round.target_score}</span></div><div class="board-matches">${matches.map(m => matchMarkup(m, people)).join('') || `<div class="board-empty">${t('noMatches')}</div>`}</div></section>`; }
  function rulesSummary(e) {
    const rules = { ...defaultRules(), ...e.rules };
    const fallback = 100 - rules.ace_most_wins - rules.ace_most_losses - rules.ace_specials.reduce((sum, group) => sum + group.probability, 0);
    const specialGroups = rules.ace_specials.map((group, index) => `<div class="summary-special"><div class="summary-special-head"><strong>${t('special')} ${index + 1}</strong><b>${group.probability}%</b></div>${e.teams.map((team, side) => {
      const candidates = team.players.filter(p => group.player_ids.includes(p.id));
      return `<div class="summary-team"><span>${t(side ? 'teamB' : 'teamA')}</span><div>${candidates.length ? candidates.map(p => `<span class="summary-candidate">${avatar(p)}<span>${esc(p.display_name)}</span>${categoryIcon(p)}</span>`).join('') : `<small>${t('noCandidateFallback')}</small>`}</div></div>`;
    }).join('')}</div>`).join('');
    return `<section class="board-rules"><div class="board-roster-head"><span>${t('eventSettings')}</span></div><div class="board-rule-block"><h3>${t('round1')}</h3><dl><div><dt>${t('target')}</dt><dd>${e.round1_target}</dd></div><div><dt>${t('round1Mode')}</dt><dd>${t(rules.round1_mode === 'random' ? 'random' : 'manual')}</dd></div><div><dt>${t('noRepeat')}</dt><dd>${t(rules.no_repeat ? 'enabled' : 'disabled')}</dd></div><div><dt>${t('allFirst')}</dt><dd>${t(rules.all_first ? 'enabled' : 'disabled')}</dd></div></dl></div><div class="board-rule-block"><h3>${t('round2')}</h3><dl><div><dt>${t('target')}</dt><dd>${e.round2_target}</dd></div><div><dt>${t('kofStarter')}</dt><dd>${t(rules.kof_starter === 'captain' ? 'captainStarter' : 'randomStarter')}</dd></div></dl></div><div class="board-rule-block"><h3>${t('aceRules')}</h3><dl><div><dt>${t('mostWinsRule')}</dt><dd>${rules.ace_most_wins}%</dd></div><div><dt>${t('mostLossesRule')}</dt><dd>${rules.ace_most_losses}%</dd></div><div><dt>${t('random')}</dt><dd>${fallback}%</dd></div></dl>${specialGroups}</div>${e.description ? `<div class="board-rule-block"><h3>${t('description')}</h3><p class="summary-description">${esc(e.description)}</p></div>` : ''}</section>`;
  }
  function boardContent(e) { const teams = e.teams, people = teams.flatMap(t => t.players), rounds = e.rounds; const stages = ['round1', 'round2', 'ace']; const round = stage => rounds.find(r => r.stage === stage); const panel = stage => roundMarkup(round(stage), e.matches.filter(m => m.stage === stage), people); const pending = !terminal(e) ? e.matches.find(m => m.status === 'pending' && m.stage === e.status) : null; const roster = teams.map(team => `<section class="board-roster-team"><h3>${esc(team.name)} · ${team.players.length}</h3>${team.players.map(p => { const lost = playerHasLoss(e, p.id, 'round2'); const recordClass = e.status === 'round2' ? (playerHasWin(e, p.id, 'round2') ? 'is-record-winner' : 'is-record-new') : ''; return `<div class="board-roster-player ${lost ? 'is-lost' : ''} ${recordClass}">${avatar(p)}<span class="board-roster-name">${esc(p.display_name)}</span>${categoryIcon(p)}${p.is_captain ? `<b class="board-captain">${t('captain')}</b>` : ''}</div>`; }).join('')}</section>`).join(''); return `<div class="board-top"><div><span class="board-live-mark">TEAM BATTLE</span><h1>${esc(e.name)}</h1><p>${esc(displayTime(e.event_time))}</p></div><span class="board-status">${t(e.status)}</span></div><div class="board-score"><div><span>${esc(teams[0].name)}</span><strong>${rounds.filter(r => r.winner_team_id === teams[0].id).length}</strong></div><b>VS</b><div><span>${esc(teams[1].name)}</span><strong>${rounds.filter(r => r.winner_team_id === teams[1].id).length}</strong></div></div>${pending ? currentMatchMarkup(e, pending, people) : ''}<div class="board-records"><div class="board-columns"><div class="board-left">${panel(stages[0])}${panel(stages[1])}</div><div class="board-right">${panel(stages[2])}${rulesSummary(e)}<section class="board-roster"><div class="board-roster-head"><span>${t('players')}</span><b>${people.length}</b></div>${roster}</section></div></div></div>`; }
  function canOperate(e) { return state.user && !terminal(e) && (manages(e) || e.teams.some(team => team.players.some(p => p.id === state.user.id))); }
  function boardPage() {
    const e = state.event;
    app.innerHTML = `<main class="broadcast-board"><div id="board-content">${boardContent(e)}</div><div id="event-operations" class="event-operations${state.operations ? '' : ' hidden'}"></div><details class="board-menu"><summary aria-label="${t('eventControls')}">☰</summary><div>${picker()}<button data-go="/">${t('homeLink')}</button>${canOperate(e) ? `<button id="toggle-operations">${t('eventControls')}</button>` : ''}${!state.user ? `<button data-go="/login?return=/live/${e.id}">${t('openLogin')}</button>` : ''}<span id="connection-state" class="help">${terminal(e) ? t('readOnly') : t(state.connection || 'reconnecting')}</span></div></details></main>`;
    bindCommon(); $('#toggle-operations')?.addEventListener('click', () => { state.operations = !state.operations; $('#event-operations').classList.toggle('hidden', !state.operations); if (state.operations) { renderOperations(); $('#event-operations').scrollIntoView({ behavior: 'smooth' }); } });
    if (state.operations) renderOperations();
  }
  async function refreshEvent() { state.event = await api('/api/public/events/' + state.event.id); if (!state.editor) boardPage(); }
  async function eventAction(path, body = {}, method = 'POST') { await post('/api/events/' + state.event.id + path, body, method); await refreshEvent(); }
  function renderOperations() {
    const e = state.event, target = $('#event-operations'); if (!target) return;
    if (!canOperate(e)) { target.innerHTML = ''; return; }
    const manager = manages(e), people = e.teams.flatMap(team => team.players), myTeam = e.teams.find(team => team.players.some(p => p.id === state.user.id));
    let content = `<div class="page-head"><h2>${t('eventControls')}</h2><div class="inline-form">${manager && noScore(e) ? `<button class="outline" id="edit-event">${t('eventSettings')}</button><button class="outline" id="edit-roster">${t('editRoster')}</button>` : ''}</div></div>`;
    if (preMatch(e)) {
      if (myTeam) content += `<section class="panel"><form id="vote-form" class="panel-body inline-form">${field('vote', `<select name="target_id">${options(myTeam.players)}</select>`)}<button class="primary">${t('vote')}</button></form></section>`;
      if (manager && e.teams.every(team => team.players.length)) content += `<section class="panel section-gap"><form id="captains-form" class="panel-body form-grid">${e.teams.map((team, i) => { const candidate = [...team.players].sort((a, b) => (e.votes.find(v => v.player_id === b.id)?.count || 0) - (e.votes.find(v => v.player_id === a.id)?.count || 0))[0]; return field(i ? 'teamB' : 'teamA', `<select name="team_${i ? 'b' : 'a'}_player_id">${team.players.map(p => `<option value="${p.id}"${p.id === candidate.id ? ' selected' : ''}>${esc(p.display_name)} · ${e.votes.find(v => v.player_id === p.id)?.count || 0} ${t('votes')}</option>`).join('')}</select>`); }).join('')}<button class="primary">${t('confirmCaptains')}</button></form></section>`;
      content += `<section class="panel section-gap"><div class="panel-head"><h2>${t('lineup')}</h2><span>${t(e.rules.round1_mode === 'random' ? 'random' : 'manual')}</span></div><div class="panel-body"><p class="help">${t('lineupHint')}</p><div class="team-lineups">${e.teams.filter(team => manager || team.players.some(p => p.id === state.user.id && p.is_captain)).map(team => `<form id="lineup-${team.id}" data-lineup="${team.id}" class="field"><h3>${esc(team.name)}</h3>${Array.from({ length: Math.min(team.players.length, Math.floor(people.length / 2)) }, (_, i) => field('matchNo', `<span>${i + 1}</span><select name="player_${i}"${e.rules.round1_mode === 'random' ? ' disabled' : ''}>${options(team.players, (e.lineups.find(l => l.team_id === team.id && l.position === i + 1)?.player_id || 0), 'randomSlot')}</select>`)).join('')}<button class="outline">${t('saveLineup')}</button></form>`).join('')}</div>${manager ? `<div class="form-footer"><button class="outline" id="random-lineup">${t('randomLineup')}</button><button class="primary accent" id="start-event">${t('start')}</button></div>` : ''}</div></section>`;
    }
    const pending = e.matches.find(m => m.status === 'pending' && m.stage === e.status);
    if (manager && pending) content += `<section class="panel section-gap"><div class="panel-head"><h2>${t(e.status)} · #${pending.seq}</h2></div><div class="panel-body"><div class="fighters"><div class="fighter">${person(people.find(p => p.id === pending.player_a_id))}</div><span class="vs">VS</span><div class="fighter">${person(people.find(p => p.id === pending.player_b_id))}</div></div><div class="result-actions"><button class="win-a" data-winner="${pending.team_a_id}" data-match="${pending.id}">${esc(pending.player_a_name)} · ${t('win')}</button><button class="win-b" data-winner="${pending.team_b_id}" data-match="${pending.id}">${esc(pending.player_b_name)} · ${t('win')}</button></div></div></section>`;
    if (e.status === 'round2' && !e.matches.some(m => m.stage === 'round2') && e.rules.kof_starter === 'captain') {
      content += `<section class="panel section-gap"><div class="panel-head"><h2>${t('captainStarter')}</h2></div><div class="panel-body"><p class="help">${t('waitingStarters')}</p><div class="team-lineups">${e.teams.map((team, side) => {
        const selected = (e.kof_starters || []).find(choice => choice.team_id === team.id);
        const player = selected && team.players.find(p => p.id === selected.player_id);
        const captain = team.players.some(p => p.id === state.user.id && p.is_captain);
        return `<section><h3>${t(side ? 'teamB' : 'teamA')}</h3>${player ? `<div class="starter-selection">${person(player)}<span class="tag done">${t('submitted')}</span></div>` : `<p class="help">${t('notSubmitted')}</p>`}${manager || (captain && !selected) ? `<form id="starter-${team.id}" data-starter="${team.id}" class="inline-form"><select name="player_id" required>${options(team.players, selected?.player_id)}</select><button class="primary">${t('sendStarter')}</button></form>` : ''}</section>`;
      }).join('')}</div></div></section>`;
    }
    if (e.status === 'round2' && !pending) {
      const last = e.matches.filter(m => m.stage === 'round2').at(-1);
      if (last) { const loser = e.teams.find(team => team.id !== last.winner_team_id); const allowed = manager || loser.players.some(p => p.id === state.user.id && p.is_captain); const lost = id => e.matches.some(m => m.stage === 'round2' && m.status === 'finished' && m.winner_team_id !== loser.id && (m.player_a_id === id || m.player_b_id === id));
        content += `<section class="panel section-gap"><div class="panel-body"><p>${t('waitingKOF')} · ${esc(loser.name)}</p>${allowed ? `<form id="kof-form" class="inline-form"><select name="player_id" required>${options(loser.players.filter(p => !lost(p.id)))}</select><button class="primary accent">${t('choosePlayer')}</button></form>` : ''}</div></section>`;
      }
    }
    if (manager && e.status === 'ace' && pending) content += `<section class="panel section-gap"><form id="ace-form" class="panel-body form-grid">${field('teamA', `<select name="player_a_id">${options(e.teams[0].players, pending.player_a_id)}</select>`)}${field('teamB', `<select name="player_b_id">${options(e.teams[1].players, pending.player_b_id)}</select>`)}<button class="outline">${t('overrideAce')}</button></form></section>`;
    if (manager && e.status === 'final_confirm') content += `<section class="panel section-gap"><div class="panel-body"><p>${t('confirmEndHint')}</p><button class="primary accent" id="confirm-end">${t('confirmEnd')}</button></div></section>`;
    if (manager && e.matches.some(m => m.status === 'finished')) content += `<section class="panel section-gap"><div class="panel-head"><h2>${t('correct')}</h2></div><form id="correct-form" class="panel-body form-grid"><p class="notice full">${t('correctHint')}</p>${field('matchNo', `<select name="match_id">${e.matches.filter(m => m.status === 'finished').map(m => `<option value="${m.id}">${t(m.stage)} #${m.seq} · ${esc(m.player_a_name)} / ${esc(m.player_b_name)}</option>`).join('')}</select>`)}${field('winner', `<select name="winner_team_id">${e.teams.map(team => `<option value="${team.id}">${esc(team.name)}</option>`).join('')}</select>`)}<button class="outline">${t('correct')}</button></form></section>`;
    target.innerHTML = content;
    $('#edit-event')?.addEventListener('click', action(async () => { state.ruleDraft = null; state.editor = 'settings'; await eventEditor('settings'); })); $('#edit-roster')?.addEventListener('click', action(async () => { state.ruleDraft = null; state.editor = 'roster'; await eventEditor('roster'); }));
    $('#vote-form')?.addEventListener('submit', action(async event => { event.preventDefault(); await eventAction('/captain-votes', { target_id: Number(new FormData(event.target).get('target_id')) }); }));
    $('#captains-form')?.addEventListener('submit', action(async event => { event.preventDefault(); const f = new FormData(event.target); await eventAction('/captains/confirm', { team_a_player_id: Number(f.get('team_a_player_id')), team_b_player_id: Number(f.get('team_b_player_id')) }); }));
    $$('[data-lineup]').forEach(form => form.onsubmit = action(async event => { event.preventDefault(); const ids = [...new FormData(form).values()].map(Number); const manual = []; let randomSeen = false; for (const id of ids) { if (!id) randomSeen = true; else { if (randomSeen) throw Error(t('lineupHint')); manual.push(id); } } await eventAction('/lineup/submit', { team_id: Number(form.dataset.lineup), player_ids: manual }); toast(t('saved')); }));
    $('#random-lineup')?.addEventListener('click', action(() => eventAction('/lineup/generate'))); $('#start-event')?.addEventListener('click', action(() => eventAction('/start')));
    $$('[data-winner]').forEach(button => button.onclick = action(() => eventAction('/matches/' + button.dataset.match + '/result', { winner_team_id: Number(button.dataset.winner) })));
    $$('[data-starter]').forEach(form => form.onsubmit = action(async event => { event.preventDefault(); await eventAction('/kof/starter', { team_id: Number(form.dataset.starter), player_id: Number(new FormData(form).get('player_id')) }); }));
    $('#kof-form')?.addEventListener('submit', action(async event => { event.preventDefault(); const last = e.matches.filter(m => m.stage === 'round2').at(-1); await eventAction('/kof/next-player', { match_id: last.id, player_id: Number(new FormData(event.target).get('player_id')) }); }));
    $('#ace-form')?.addEventListener('submit', action(async event => { event.preventDefault(); const f = new FormData(event.target); await eventAction('/ace/confirm', { player_a_id: Number(f.get('player_a_id')), player_b_id: Number(f.get('player_b_id')) }); }));
    $('#correct-form')?.addEventListener('submit', action(async event => { event.preventDefault(); if (!confirm(t('correctHint'))) return; const f = new FormData(event.target); await eventAction('/matches/' + f.get('match_id') + '/result', { winner_team_id: Number(f.get('winner_team_id')) }, 'PATCH'); }));
    $('#confirm-end')?.addEventListener('click', action(async () => { if (confirm(t('confirmEndHint'))) await eventAction('/confirm'); }));
  }
  async function openEvent(id, preserve = false) {
    if (state.event?.id !== Number(id)) state.event = null;
    state.event = await api('/api/public/events/' + id);
    if (state.editor) await eventEditor(state.editor, preserve); else boardPage();
    if (terminal(state.event)) { closeStream(); return; }
    if (state.stream) return;
    const stream = new EventSource('/api/public/events/' + id + '/stream'); state.stream = stream;
    stream.onopen = () => { state.connection = 'connected'; if ($('#connection-state')) $('#connection-state').textContent = t('connected'); };
    stream.onerror = () => { state.connection = 'reconnecting'; if ($('#connection-state')) $('#connection-state').textContent = t('reconnecting'); };
    stream.onmessage = message => {
      const data = JSON.parse(message.data); const changed = JSON.stringify(state.event) !== JSON.stringify(data); state.event = data;
      if (changed && !state.editor && $('#board-content')) { $('#board-content').innerHTML = boardContent(data); const forms = captureForms(); renderOperations(); restoreForms(forms); }
      if (terminal(data)) { closeStream(); if (!state.editor) boardPage(); }
    };
  }

  function pastMatchRow(round = 1) { return `<div class="past-match"><select name="past_round">${[1, 2, 3].map(r => `<option value="${r}"${r === round ? ' selected' : ''}>${t(r === 1 ? 'round1' : r === 2 ? 'round2' : 'ace')}</option>`).join('')}</select><select name="past_a" required>${options(state.users.filter(u => state.roster[u.id] === 'A'))}</select><span>VS</span><select name="past_b" required>${options(state.users.filter(u => state.roster[u.id] === 'B'))}</select><select name="past_winner"><option value="a">${t('teamA')} · ${t('win')}</option><option value="b">${t('teamB')} · ${t('win')}</option></select><button type="button" class="link danger" data-remove-match>${t('delete')}</button></div>`; }
  async function archivePage(preserve = false) {
    state.users = (await api('/api/users')).users.filter(u => u.role !== 'superadmin'); if (!preserve) state.roster = {};
    shell(`<div class="page-head"><div><h1>${t('archive')}</h1><p class="subtitle">${t('pastHint')}</p></div></div><form id="archive-form"><section class="panel"><div class="panel-body form-grid">${field('name', input('name', '', 'text', 'required'), true)}${field('time', eventTimeInput())}</div></section>${rosterMarkup(state.users, state.roster)}<section class="panel section-gap"><div class="panel-head"><h2>${t('results')}</h2></div><div class="panel-body"><div id="past-matches">${pastMatchRow()}</div><button type="button" class="outline" id="add-match">${t('addMatch')}</button></div></section><div class="form-footer"><button class="primary accent">${t('pastSave')}</button></div></form>`, 'archive');
    bindRoster($('#archive-form'));
    // 过去赛事直接指定两队，避免历史对局被重新随机分配。
    $$('[name^="pick_"][value="R"]').forEach(el => el.closest('label').remove()); $('#all-random').remove();
    const bindRows = () => $$('[data-remove-match]').forEach(button => button.onclick = () => button.closest('.past-match').remove()); bindRows();
    $('#add-match').onclick = () => { $('#past-matches').insertAdjacentHTML('beforeend', pastMatchRow()); bindRows(); };
    $$('[name^="pick_"]').forEach(el => el.addEventListener('change', () => { for (const side of ['a', 'b']) $$(`[name="past_${side}"]`).forEach(select => { const chosen = select.value; select.innerHTML = options(state.users.filter(u => state.roster[u.id] === side.toUpperCase()), chosen); }); }));
    $('#archive-form').onsubmit = action(async event => { event.preventDefault(); const f = new FormData(event.target); const matches = $$('.past-match').map(row => ({ round: Number($('[name="past_round"]', row).value), a_id: Number($('[name="past_a"]', row).value), b_id: Number($('[name="past_b"]', row).value), who_win: $('[name="past_winner"]', row).value })); if (!confirm(t('confirmEndHint'))) return; const result = await post('/api/events/archive', { name: f.get('name'), event_time: eventTimeValue(f), ...rosterValue(event.target), matches }); go('/live/' + result.id); });
  }
  async function renderRoute(preserve = false) {
    document.documentElement.lang = state.language;
    const path = location.pathname;
    if (path.startsWith('/live/')) return openEvent(path.split('/').pop(), preserve);
    closeStream(); state.event = null;
    if (path === '/login') return loginPage();
    const page = new URLSearchParams(location.search).get('page') || 'home';
    if (page === 'home') return homePage();
    if (!state.user) return go('/login');
    if (page === 'settings') return accountPage();
    if (state.user.role !== 'superadmin') return go('/');
    if (page === 'createEvent' || page === 'events') return eventEditor('create', preserve);
    if (page === 'createUser' || page === 'users') return usersPage();
    if (page === 'passwords') return usersPage(true);
    if (page === 'siteSettings' || page === 'site-settings') return siteSettingsPage();
    if (page === 'archive') return archivePage(preserve);
    return homePage();
  }
  window.addEventListener('popstate', () => { closeStream(); state.editor = null; state.operations = false; renderRoute().catch(error => toast(error.message)); });
  window.addEventListener('beforeunload', closeStream);
  (async () => { try { state.user = (await api('/api/auth/me')).user; } catch (_) {} await renderRoute(); })().catch(error => { app.innerHTML = `<main class="page"><p>${esc(error.message)}</p><button data-go="/">${t('homeLink')}</button></main>`; bindCommon(); });
})();
