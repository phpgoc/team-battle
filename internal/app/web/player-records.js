(() => {
  'use strict';
  const dialog = document.getElementById('player-records');
  const t = key => window.TB_I18N[document.documentElement.lang]?.[key] || window.TB_I18N.zh[key] || key;
  const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  let requestID = 0;

  function scoreMarkup(score, wins = 'recordWins', losses = 'recordLosses') {
    return `<dl class="record-score"><div class="record-wins"><dt>${t(wins)}</dt><dd>${score.wins}</dd></div><div class="record-losses"><dt>${t(losses)}</dt><dd>${score.losses}</dd></div></dl>`;
  }

  function recordsMarkup(data) {
    const user = data.user;
    const avatar = user.avatar_url ? `<img class="record-avatar" src="${esc(user.avatar_url)}" alt="">` : `<span class="record-avatar record-avatar-fallback">${esc(user.display_name.slice(0, 1))}</span>`;
    const categoryIcon = user.category_icon ? `<img class="record-category-icon" src="${esc(user.category_icon)}" alt="">` : '';
    return `<div class="record-person">${avatar}<div><h3>${esc(user.display_name)}</h3><p>${categoryIcon}${esc(user.category_name || t('uncategorized'))}</p></div></div><div class="record-totals"><section class="record-team-battle"><h3>${t('teamBattleRecord')}</h3>${scoreMarkup(data.team_battle, 'teamBattleWins', 'teamBattleLosses')}</section><section><h3>${t('overallRecord')}</h3>${scoreMarkup(data.overall)}</section><section><h3>${t('aceRecord')}</h3>${scoreMarkup(data.ace)}</section></div><section class="record-categories"><h3>${t('byOpponentCategory')}</h3>${data.by_category.length ? `<table><thead><tr><th>${t('opponentCategory')}</th><th>${t('recordWins')}</th><th>${t('recordLosses')}</th></tr></thead><tbody>${data.by_category.map(category => `<tr><td><span class="record-category">${category.icon_url ? `<img class="record-category-icon" src="${esc(category.icon_url)}" alt="">` : ''}${esc(category.name || t('uncategorized'))}</span></td><td class="record-wins">${category.wins}</td><td class="record-losses">${category.losses}</td></tr>`).join('')}</tbody></table>` : `<p class="record-empty">${t('noPlayerRecords')}</p>`}</section><p class="record-hint">${t('recordScope')}</p>`;
  }

  async function openRecords(id) {
    if (!id) return;
    const currentRequest = ++requestID;
    dialog.innerHTML = `<section class="records-modal"><header class="modal-head"><h2 id="player-records-title">${t('playerRecords')}</h2><button type="button" class="ghost" data-close-records autofocus>${t('close')}</button></header><div class="modal-body" aria-live="polite"><p class="record-empty">${t('loadingRecords')}</p></div></section>`;
    if (!dialog.open) dialog.showModal();
    try {
      const response = await fetch('/api/public/users/' + id + '/records', { cache: 'no-store' });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error || t('requestFailed'));
      // 弹层独立于赛事看板，SSE刷新不会关闭弹层；过期请求不能覆盖新内容。
      if (currentRequest === requestID && dialog.open) dialog.querySelector('.modal-body').innerHTML = recordsMarkup(data);
    } catch (error) {
      if (currentRequest === requestID && dialog.open) dialog.querySelector('.modal-body').innerHTML = `<p class="record-empty" role="alert">${esc(error.message)}</p>`;
    }
  }

  document.addEventListener('click', event => {
    const trigger = event.target.closest('[data-player-record]');
    if (trigger) openRecords(Number(trigger.dataset.playerRecord));
  });
  dialog.addEventListener('click', event => {
    if (event.target === dialog || event.target.closest('[data-close-records]')) dialog.close();
  });
  dialog.addEventListener('close', () => { requestID++; });
  window.addEventListener('popstate', () => dialog.close());
})();
