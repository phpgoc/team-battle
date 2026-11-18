(() => {
  'use strict';
  const rows = root => [...root.querySelectorAll('[data-roster-player]')];
  const choices = (ctx, chosen, allowRandom) => [
    ...(chosen === '?' ? [['?', 'rosterUnknown']] : []),
    ...(allowRandom ? [['R', 'randomTeam']] : []), ['A', 'teamA'], ['B', 'teamB']
  ].map(([value, key]) => `<option value="${value}"${chosen === value ? ' selected' : ''}>${ctx.t(key)}</option>`).join('');

  function playerRow(user, side, ctx, editable, allowRandom) {
    return `<li class="roster-entry" data-roster-player="${user.id}"><span class="roster-entry-person">${ctx.person(user)}</span><select name="pick_${user.id}" aria-label="${ctx.esc(user.display_name)} · ${ctx.t('rosterAssignment')}"${editable ? '' : ' disabled'}>${choices(ctx, side, allowRandom)}</select>${editable ? `<button type="button" class="link danger" data-remove-player aria-label="${ctx.t('delete')} ${ctx.esc(user.display_name)}">${ctx.t('delete')}</button>` : ''}</li>`;
  }

  function markup(ctx) {
    const { users, entries, editable, allowRandom, t, esc } = ctx;
    const directory = new Map(users.map(user => [Number(user.id), user]));
    const selected = entries.filter(([id]) => directory.has(Number(id)));
    return `<section class="panel section-gap editor-section" id="event-roster" data-roster-editable="${editable}" data-roster-random="${allowRandom}"><div class="panel-head"><h2>${t('players')}</h2><span id="roster-count"></span></div><div class="panel-body"><p class="help">${t(allowRandom ? 'rosterEntryHint' : 'archiveRosterHint')}</p>${editable ? `<div class="roster-add"><div class="roster-search"><label class="field-label" for="player-search">${t('rosterChoosePlayer')}</label><input id="player-search" name="roster_search" role="combobox" aria-autocomplete="list" aria-expanded="false" aria-controls="player-suggestions" autocomplete="off" placeholder="${esc(t('searchPlayers'))}"><div class="roster-suggestions" id="player-suggestions" role="listbox" aria-label="${t('rosterChoosePlayer')}" hidden></div></div><button type="button" class="primary" id="add-player" disabled>${t('addPlayer')}</button></div>` : ''}<p class="roster-feedback help" id="roster-feedback" role="status" aria-live="polite"></p><div class="roster-list-heading"><span>${t('rosterEntries')}</span><span>${t('rosterAssignment')}</span>${editable ? '<span></span>' : ''}</div><ol class="roster-entries" id="roster-entries">${selected.map(([id, side]) => playerRow(directory.get(Number(id)), side, ctx, editable, allowRandom)).join('')}</ol><p class="help roster-empty" id="roster-empty"${selected.length ? ' hidden' : ''}>${t('rosterEmpty')}</p></div></section>`;
  }

  // 按录入顺序读取；禁用控件仍可查看原始意向，不依赖会跳过禁用项的FormData。
  function value(root) {
    const player_ids = [], fixed_assignments = {};
    for (const row of rows(root)) {
      const id = Number(row.dataset.rosterPlayer), side = row.querySelector('select').value;
      player_ids.push(id);
      if (side === 'A' || side === 'B') fixed_assignments[id] = side;
    }
    return { player_ids, fixed_assignments };
  }

  function bind(root, ctx) {
    const { users, t, onChange } = ctx;
    const editable = root.dataset.rosterEditable === 'true', allowRandom = root.dataset.rosterRandom === 'true';
    const list = root.querySelector('#roster-entries'), search = root.querySelector('#player-search');
    const menu = root.querySelector('#player-suggestions'), add = root.querySelector('#add-player');
    const feedback = root.querySelector('#roster-feedback');
    let selectedID = 0, matches = [], active = -1, selection = null;
    const entered = () => new Set(rows(root).map(row => Number(row.dataset.rosterPlayer)));
    const available = () => { const ids = entered(); return users.filter(user => !ids.has(Number(user.id)) && (!user.status || user.status === 'active') && user.selectable !== false); };
    const exact = query => available().filter(user => [user.display_name, user.username].some(name => String(name || '').toLocaleLowerCase() === query));
    const update = () => {
      const count = rows(root).length;
      root.querySelector('#roster-count').textContent = `${t('selected')} ${count} / 12`;
      root.querySelector('#roster-empty').hidden = count > 0;
      if (add) add.disabled = !selectedID || count >= 12;
      feedback.textContent = count >= 12 ? t('rosterLimit') : '';
      onChange();
    };
    const close = () => {
      menu.hidden = true; search.setAttribute('aria-expanded', 'false');
      search.removeAttribute('aria-activedescendant'); active = -1;
    };
    const highlight = () => {
      [...menu.querySelectorAll('[role=option]')].forEach((option, index) => {
        option.setAttribute('aria-selected', String(index === active));
        if (index === active) { search.setAttribute('aria-activedescendant', option.id); option.scrollIntoView({ block: 'nearest' }); }
      });
    };
    const suggest = () => {
      const query = search.value.trim().toLocaleLowerCase();
      matches = available().filter(user => `${user.display_name} ${user.username || ''}`.toLocaleLowerCase().includes(query));
      // 精确和前缀命中优先，重名时通过账号名区分。
      const rank = user => [user.display_name, user.username].some(name => String(name || '').toLocaleLowerCase() === query) ? 0 : String(user.display_name).toLocaleLowerCase().startsWith(query) ? 1 : 2;
      matches.sort((a, b) => rank(a) - rank(b));
      menu.innerHTML = matches.map(user => `<button type="button" role="option" aria-selected="false" id="player-option-${user.id}" data-suggest-player="${user.id}">${ctx.person(user)}</button>`).join('') || `<p class="help">${t('noPlayerMatches')}</p>`;
      active = -1; search.removeAttribute('aria-activedescendant');
      menu.hidden = false; search.setAttribute('aria-expanded', 'true');
    };
    const choose = id => {
      const user = available().find(user => Number(user.id) === id);
      if (!user) return;
      selectedID = id; selection.value = String(id); search.value = user.display_name; close(); update(); search.focus();
    };
    if (editable) {
      // 将已选账号ID纳入表单快照，重名选手在切换语言后也保持同一账号。
      selection = document.createElement('input');
      selection.type = 'hidden'; selection.name = 'roster_search_id';
      root.querySelector('.roster-add').append(selection);
      search.addEventListener('focus', () => { if (!selectedID) suggest(); });
      const syncSearch = () => {
        const restored = available().find(user => Number(user.id) === Number(selection.value) && user.display_name === search.value);
        const found = exact(search.value.trim().toLocaleLowerCase());
        selectedID = restored ? Number(restored.id) : found.length === 1 ? Number(found[0].id) : 0;
        add.disabled = !selectedID || rows(root).length >= 12;
      };
      search.addEventListener('input', () => {
        selection.value = '';
        syncSearch();
        suggest();
      });
      root.closest('form').addEventListener('formrestored', syncSearch);
      search.addEventListener('keydown', event => {
        if (event.key === 'Escape') { event.preventDefault(); close(); }
        else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
          event.preventDefault(); if (menu.hidden) suggest();
          if (matches.length) { active = (active + (event.key === 'ArrowDown' ? 1 : active < 0 ? 0 : -1) + matches.length) % matches.length; highlight(); }
        } else if (event.key === 'Enter') {
          // 回车只选择联想项，不提交整张赛事表单。
          event.preventDefault(); if (!menu.hidden && active >= 0) choose(Number(matches[active].id)); else close();
        }
      });
      menu.addEventListener('click', event => { const option = event.target.closest('[data-suggest-player]'); if (option) choose(Number(option.dataset.suggestPlayer)); });
      root.querySelector('.roster-add').addEventListener('focusout', event => { if (!event.currentTarget.contains(event.relatedTarget)) close(); });
      add.addEventListener('click', () => {
        if (rows(root).length >= 12) { feedback.textContent = t('rosterLimit'); return; }
        // 语言切换会恢复输入文本，仍须匹配已有账号，不能把自由文本当成选手。
        const candidates = selectedID ? available().filter(user => Number(user.id) === selectedID) : exact(search.value.trim().toLocaleLowerCase());
        if (candidates.length !== 1) { feedback.textContent = t('choosePlayerFirst'); return; }
        list.insertAdjacentHTML('beforeend', playerRow(candidates[0], allowRandom ? 'R' : 'A', ctx, editable, allowRandom));
        selectedID = 0; selection.value = ''; search.value = ''; close(); update(); search.focus();
      });
      list.addEventListener('change', update);
      list.addEventListener('click', event => {
        const remove = event.target.closest('[data-remove-player]');
        if (remove) { remove.closest('[data-roster-player]').remove(); update(); }
      });
    }
    update();
  }

  window.TBRosterEditor = { markup, bind, value };
})();
