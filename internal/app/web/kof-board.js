(() => {
  'use strict';
  // 报名资料来自私有接口，只标注当前登录者有权查看的队伍，不写入公开看板数据。
  window.TBKOFBoard = {
    render({ root, event, choices, user, manager, selectedTeam, t, onTeam, onSend, action }) {
      const toolbar = root.querySelector('#board-kof-controls');
      if (!toolbar) return;
      const teams = event.teams.filter(team => manager || team.players.some(player => player.id === user.id && player.active !== false));
      const team = teams.find(team => team.id === selectedTeam) || teams[0];
      if (!team) return;
      const choice = choices.teams.find(choice => choice.team_id === team.id);
      const occupied = choice?.player_id || 0;
      const teamLabel = team => t(event.teams[0].id === team.id ? 'teamA' : 'teamB');
      toolbar.innerHTML = `<div class="kof-board-heading"><strong>${t(choices.match_id ? 'nextSlot' : 'kofStarter')}</strong>${manager ? `<select id="kof-board-team" aria-label="${t('lineupTeam')}">${teams.map(candidate => `<option value="${candidate.id}"${candidate.id === team.id ? ' selected' : ''}>${teamLabel(candidate)}</option>`).join('')}</select>` : `<span>${teamLabel(team)}</span>`}</div><p>${t(!choice ? 'kofNoSlotNeeded' : occupied ? 'slotOccupied' : choice.candidate_ids.length ? 'kofClickPlayer' : 'noEligible')}</p><small>${t('reservationHint')}</small>`;
      root.querySelector('#kof-board-team')?.addEventListener('change', event => onTeam(Number(event.target.value)));
      root.querySelectorAll('[data-roster-player]').forEach(row => {
        row.querySelector('.kof-player-action')?.remove();
        delete row.dataset.kofSelected;
        const id = Number(row.dataset.rosterPlayer);
        if (!team.players.some(player => player.id === id) || !choice) return;
        if (id === occupied) {
          row.dataset.kofSelected = 'true';
          row.insertAdjacentHTML('beforeend', `<span class="kof-player-action kof-reserved-label">${t('reservedPlayer')}</span>`);
          return;
        }
        const captain = choice.captain_id === user.id;
        const self = !choice.captain_id && id === user.id;
        if (!choice.candidate_ids.includes(id) || (!manager && !captain && !self) || (occupied && !manager)) return;
        const button = document.createElement('button');
        button.type = 'button'; button.className = 'kof-player-action kof-send-player';
        button.dataset.kofPlayer = String(id); button.dataset.kofTeam = String(team.id);
        button.textContent = t(manager || captain ? 'sendNext' : 'signUpNext');
        const player = team.players.find(player => player.id === id);
        button.setAttribute('aria-label', `${button.textContent} · ${player.display_name}`);
        button.onclick = action(() => onSend({ match_id: choices.match_id || 0, team_id: team.id, player_id: id }));
        row.append(button);
      });
    }
  };
})();
