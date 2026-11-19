(() => {
  'use strict';
  let sequence = 0;

  // 保留原始select提交账号ID；搜索文本只用于选择，不能当作账号提交。
  function bind(select, t) {
    if (select.dataset.searchBound) return;
    select.dataset.searchBound = 'true';
    const required = select.required, id = `search-options-${++sequence}`;
    const wrapper = document.createElement('div'); wrapper.className = 'search-select';
    select.before(wrapper); wrapper.append(select);
    const input = document.createElement('input');
    input.type = 'search'; input.autocomplete = 'off'; input.required = required;
    input.dataset.formKey = `search:${select.name}`;
    input.setAttribute('role', 'combobox'); input.setAttribute('aria-autocomplete', 'list');
    input.setAttribute('aria-expanded', 'false'); input.setAttribute('aria-controls', id);
    for (const name of ['aria-label', 'aria-labelledby']) if (select.hasAttribute(name)) input.setAttribute(name, select.getAttribute(name));
    const empty = [...select.options].find(option => !option.value);
    input.placeholder = empty?.textContent || t('searchPlayers');
    const toggle = document.createElement('button'); toggle.type = 'button'; toggle.className = 'search-select-toggle';
    toggle.setAttribute('aria-label', t('showChoices')); toggle.setAttribute('aria-controls', id);
    toggle.innerHTML = '<svg viewBox="0 0 20 20" width="16" height="16" aria-hidden="true"><path d="m5 7 5 5 5-5" fill="none" stroke="currentColor" stroke-width="1.6"/></svg>';
    const clear = document.createElement('button'); clear.type = 'button'; clear.className = 'search-select-clear';
    clear.setAttribute('aria-label', t('clear')); clear.textContent = '×';
    const menu = document.createElement('div'); menu.id = id; menu.className = 'search-select-menu';
    menu.setAttribute('role', 'listbox'); menu.hidden = true;
    if (input.hasAttribute('aria-labelledby')) menu.setAttribute('aria-labelledby', input.getAttribute('aria-labelledby'));
    else menu.setAttribute('aria-label', input.getAttribute('aria-label') || t('showChoices'));
    wrapper.append(input, clear, toggle, menu);
    select.hidden = true; select.required = false; select.tabIndex = -1;
    select.setAttribute('aria-hidden', 'true');
    let matches = [], active = -1;
    const options = () => [...select.options].filter(option => !option.disabled);
    const close = () => { menu.hidden = true; input.setAttribute('aria-expanded', 'false'); input.removeAttribute('aria-activedescendant'); active = -1; };
    const validate = () => { input.setCustomValidity((required && !select.value) || (input.value.trim() && !select.value) ? t('chooseExistingAccount') : ''); clear.hidden = !input.value || select.disabled; };
    const sync = (preserveText = false) => {
      input.disabled = select.disabled; toggle.disabled = select.disabled;
      if (!preserveText || select.value) input.value = select.value ? select.selectedOptions[0]?.textContent || '' : '';
      validate(); close();
    };
    const highlight = () => {
      [...menu.querySelectorAll('[role=option]')].forEach((option, index) => {
        option.classList.toggle('is-active', index === active);
        if (index === active) { input.setAttribute('aria-activedescendant', option.id); option.scrollIntoView({block:'nearest'}); }
      });
    };
    const suggest = (all = false) => {
      if (select.matches(':disabled')) return;
      const query = all ? '' : input.value.trim().toLocaleLowerCase();
      matches = options().filter(option => option.textContent.toLocaleLowerCase().includes(query));
      const rank = option => option.textContent.toLocaleLowerCase() === query ? 0 : option.textContent.toLocaleLowerCase().startsWith(query) ? 1 : 2;
      matches.sort((a, b) => rank(a) - rank(b)); menu.replaceChildren();
      matches.forEach((option, index) => {
        const item = document.createElement('button'); item.type = 'button'; item.id = `${id}-${index}`;
        item.setAttribute('role', 'option'); item.setAttribute('aria-selected', String(option.value === select.value));
        item.dataset.index = index; item.textContent = option.textContent; menu.append(item);
      });
      if (!matches.length) { const message = document.createElement('p'); message.className = 'help'; message.textContent = t('noAccountMatches'); menu.append(message); }
      active = -1; input.removeAttribute('aria-activedescendant'); menu.hidden = false; input.setAttribute('aria-expanded', 'true');
    };
    const choose = index => {
      if (!matches[index]) return;
      select.value = matches[index].value; sync();
      select.dispatchEvent(new Event('change', {bubbles:true})); input.focus(); close();
    };
    input.addEventListener('focus', () => suggest(true));
    input.addEventListener('click', () => { if (menu.hidden) suggest(true); });
    input.addEventListener('input', () => {
      const query = input.value.trim().toLocaleLowerCase();
      const exact = options().filter(option => option.value && option.textContent.toLocaleLowerCase() === query);
      select.value = exact.length === 1 ? exact[0].value : '';
      validate(); suggest();
      select.dispatchEvent(new Event('change', {bubbles:true}));
    });
    input.addEventListener('keydown', event => {
      if (event.key === 'Escape') { event.preventDefault(); close(); }
      else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
        event.preventDefault(); if (menu.hidden) suggest(true);
        if (matches.length) { active = (active + (event.key === 'ArrowDown' ? 1 : active < 0 ? 0 : -1) + matches.length) % matches.length; highlight(); }
      } else if (event.key === 'Enter' && !menu.hidden) {
        // 与添加选手一致，回车只选候选，不能误提交整张表单。
        event.preventDefault(); if (active >= 0) choose(active); else close();
      }
    });
    toggle.addEventListener('click', () => { if (!menu.hidden) close(); else { input.focus(); suggest(true); } });
    clear.addEventListener('click', () => { select.value = ''; sync(); select.dispatchEvent(new Event('change', {bubbles:true})); input.focus(); suggest(true); });
    menu.addEventListener('click', event => { const item = event.target.closest('[data-index]'); if (item) choose(Number(item.dataset.index)); });
    wrapper.addEventListener('focusout', event => { if (!wrapper.contains(event.relatedTarget)) close(); });
    select.addEventListener('change', () => { if (select.value) sync(); });
    select.form?.addEventListener('formrestored', () => sync(true));
    select.form?.addEventListener('reset', () => queueMicrotask(() => sync()));
    sync();
  }

  window.TBSearchSelect = { bindAll: (root, t) => root.querySelectorAll('select[data-searchable]').forEach(select => bind(select, t)) };
})();
