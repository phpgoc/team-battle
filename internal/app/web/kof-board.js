(() => {
  'use strict';
  const dialog = document.getElementById('roster-actions');
  const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  dialog.addEventListener('click', event => { if (event.target === dialog || event.target.closest('[data-close-roster]')) dialog.close(); });
  window.addEventListener('popstate', () => dialog.close());

  // 点击名单位置统一选择队长或KOF出场；权限与候选始终来自最新赛事和私有接口。
  function bindRows({ root, event, user, manager, choices, selectedTeam, t, action, onVote, onSend, onCaptain }) {
    const ownTeam = event.teams.find(team => team.players.some(player => player.id === user?.id && player.active !== false));
    const selected = event.teams.find(team => team.id === selectedTeam) || (manager ? event.teams[0] : ownTeam);
    const usable = !['completed', 'cancelled'].includes(event.status);
    root.querySelectorAll('[data-roster-player]').forEach(row => {
      const id = Number(row.dataset.rosterPlayer);
      const team = event.teams.find(team => team.players.some(player => player.id === id));
      const player = team.players.find(player => player.id === id);
      const captain = team.players.find(player => player.is_captain && player.active !== false);
      const choice = choices?.teams.find(choice => choice.team_id === team.id);
      const lost = event.status === 'round2' && row.classList.contains('is-lost');
      const vote = usable && !lost && user && ownTeam?.id === team.id && !captain && id !== user.id && player.active !== false;
      const appoint = usable && manager && player.active !== false;
      const send = usable && !lost && choice && selected?.id === team.id && choice.candidate_ids.includes(id) && (manager || captain?.id === user?.id || (!captain && id === user?.id)) && (!choice.player_id || (manager && choice.player_id !== id));
      row.querySelector('.roster-action-area')?.remove();
      row.querySelector('.kof-player-action')?.remove();
      delete row.dataset.kofSelected; delete row.dataset.rosterVote; delete row.dataset.kofPlayer; delete row.dataset.captainPlayer;
      if (choice?.player_id === id && selected?.id === team.id) {
        row.dataset.kofSelected = 'true';
        row.querySelector('.board-roster-name').insertAdjacentHTML('beforeend', `<span class="kof-player-action kof-reserved-label">${t('reservedPlayer')}</span>`);
      }
      if (vote) row.dataset.rosterVote = String(id);
      if (send) row.dataset.kofPlayer = String(id);
      if (appoint) row.dataset.captainPlayer = String(id);
      const enabled = vote || send || appoint;
      row.classList.toggle('roster-operable', Boolean(enabled));
      row.onclick = null; row.onkeydown = null;
      row.removeAttribute('tabindex'); row.removeAttribute('aria-haspopup');
      if (!enabled) return;
      const trigger = document.createElement('button');
      trigger.type = 'button'; trigger.className = 'roster-action-area';
      trigger.setAttribute('aria-haspopup', 'dialog');
      trigger.setAttribute('aria-label', `${t('playerActions')} · ${player.display_name}`);
      trigger.textContent = '⋯';
      row.append(trigger);
      const open = click => {
        click.preventDefault(); click.stopPropagation();
        dialog.dataset.player = String(id);
        const votes = event.votes.find(vote => vote.player_id === id)?.count || 0;
        dialog.innerHTML = `<section class="records-modal roster-actions-modal"><header class="modal-head"><h2 id="roster-actions-title">${esc(player.display_name)}</h2><button type="button" class="ghost" data-close-roster>${t('close')}</button></header><div class="modal-body roster-action-list">${appoint ? `<button type="button" class="primary" data-set-captain>${t(player.is_captain ? 'removeCaptain' : 'electCaptain')}</button>` : vote ? `<p class="help">${t('voteThreshold').replace('{count}', Math.ceil(team.players.filter(player => player.active !== false).length / 2))} · ${votes} ${t('votes')}</p><button type="button" class="primary" data-vote-captain>${t('electCaptain')}</button>` : ''}${send ? `<button type="button" class="primary accent" data-send-kof>${t(manager || captain ? 'sendNext' : 'signUpNext')}</button>` : ''}<button type="button" class="outline" data-player-record="${id}">${t('viewPlayerRecords')}</button></div></section>`;
        dialog.querySelector('[data-set-captain]')?.addEventListener('click', action(async () => { await onCaptain(team.id, player.is_captain ? 0 : id); dialog.close(); }));
        dialog.querySelector('[data-vote-captain]')?.addEventListener('click', action(async () => { await onVote(id); dialog.close(); }));
        dialog.querySelector('[data-send-kof]')?.addEventListener('click', action(async () => { await onSend({ match_id: choices.match_id || 0, team_id: team.id, player_id: id }); dialog.close(); }));
        dialog.querySelector('[data-player-record]').addEventListener('click', () => dialog.close());
        if (!dialog.open) dialog.showModal();
      };
      trigger.onclick = open;
    });
    if (dialog.open && !root.querySelector(`[data-roster-player="${Number(dialog.dataset.player)}"].roster-operable`)) dialog.close();
  }

  window.TBKOFBoard = {
    bindRows,
    render(options) {
      const { root, event, choices, user, manager, selectedTeam, t, onTeam } = options;
      const toolbar = root.querySelector('#board-kof-controls');
      if (!toolbar) return;
      const teams = event.teams.filter(team => manager || team.players.some(player => player.id === user.id && player.active !== false));
      const team = teams.find(team => team.id === selectedTeam) || teams[0];
      if (!team) return;
      const choice = choices.teams.find(choice => choice.team_id === team.id);
      const teamLabel = team => t(event.teams[0].id === team.id ? 'teamA' : 'teamB');
      toolbar.innerHTML = `<div class="kof-board-heading"><strong>${t(choices.match_id ? 'nextSlot' : 'kofStarter')}</strong>${manager ? `<select id="kof-board-team" aria-label="${t('lineupTeam')}">${teams.map(candidate => `<option value="${candidate.id}"${candidate.id === team.id ? ' selected' : ''}>${teamLabel(candidate)}</option>`).join('')}</select>` : `<span>${teamLabel(team)}</span>`}</div><p>${t(!choice ? 'kofNoSlotNeeded' : choice.player_id ? 'slotOccupied' : choice.candidate_ids.length ? 'kofClickPlayer' : 'noEligible')}</p>`;
      toolbar.querySelector('#kof-board-team')?.addEventListener('change', change => onTeam(Number(change.target.value)));
      bindRows({ ...options, selectedTeam: team.id });
    }
  };
})();
