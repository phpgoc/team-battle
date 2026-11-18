(() => {
  'use strict';
  const dialog = document.getElementById('confirm-dialog');
  const t = key => window.TB_I18N[document.documentElement.lang]?.[key] || window.TB_I18N.zh[key] || key;
  const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  let pending = null;

  function settle(accepted) {
    if (!pending) return;
    const current = pending;
    pending = null;
    if (dialog.open) dialog.close();
    current.resolve(accepted && current.url === location.href);
  }

  function ask(message, { title = t('confirm'), match = null } = {}) {
    if (pending) return Promise.resolve(false);
    const matchup = match ? `<p class="confirm-stage">${esc(match.stage)} · ${t('matchNo')} ${Number(match.seq)}</p><div class="confirm-match">${['a', 'b'].map(side => `<div class="confirm-player ${match.winner === side ? 'confirm-winner' : 'confirm-loser'}"><span>${t(side === 'a' ? 'teamA' : 'teamB')}</span><strong>${esc(match[side])}</strong><b>${t(match.winner === side ? 'win' : 'lose')}</b></div>`).join('<span class="confirm-vs">VS</span>')}</div>` : '';
    dialog.innerHTML = `<section><header class="confirm-heading"><h2 id="confirm-dialog-title">${esc(title)}</h2></header><div class="confirm-body">${matchup}<p id="confirm-dialog-message">${esc(message)}</p></div><footer class="confirm-actions"><button type="button" class="outline" data-confirm-cancel autofocus>${t('cancel')}</button><button type="button" class="primary accent" data-confirm-accept tabindex="-1">${t('confirm')}</button></footer></section>`;
    return new Promise(resolve => {
      pending = { resolve, url: location.href };
      dialog.showModal();
      dialog.querySelector('[data-confirm-cancel]').focus({ preventScroll: true });
    });
  }

  dialog.addEventListener('click', event => {
    if (event.target === dialog || event.target.closest('[data-confirm-cancel]')) settle(false);
    // 键盘激活或脚本click的detail为0；确定只接受实际点击和触屏点按。
    else if (event.target.closest('[data-confirm-accept]') && event.detail > 0) settle(true);
  });
  dialog.addEventListener('keydown', event => {
    // 不设置默认提交按钮；阻止回车及确定按钮的所有键盘激活方式。
    if (event.key === 'Enter' || (event.target.closest('[data-confirm-accept]') && [' ', 'Spacebar'].includes(event.key))) {
      event.preventDefault(); event.stopPropagation();
    }
  });
  dialog.addEventListener('cancel', event => { event.preventDefault(); settle(false); });
  dialog.addEventListener('close', () => { if (!dialog.open) settle(false); });
  window.addEventListener('popstate', () => settle(false));
  window.TBConfirm = { ask, result: match => ask(t('recordWinConfirmHint'), { title: t('confirmRecordWin'), match }), cancel: () => settle(false) };
})();
