(() => {
  'use strict';
  // 每支队伍独立保存未提交的排阵草稿，管理员切队时不丢失输入。
  window.TBLineupPage = {
    render({ root, event, teams, selectedTeam, drafts, t, esc, person, picker, onTeam, onSave, onBack, bindCommon, action }) {
      const team = teams.find(team => team.id === selectedTeam) || teams[0];
      const total = event.teams.reduce((count, team) => count + team.players.filter(player => player.active !== false).length, 0);
      const players = team.players.filter(player => player.active !== false);
      const count = Math.min(players.length, Math.floor(total / 2));
      const values = drafts[team.id] || Array.from({ length: count }, (_, index) => event.lineups.find(entry => entry.team_id === team.id && entry.position === index + 1)?.player_id || 0);
      const teamLabel = team => t(event.teams[0].id === team.id ? 'teamA' : 'teamB');
      root.innerHTML = `<main class="event-editor-page lineup-page"><div class="editor-language">${picker()}</div><div class="page-head"><div><h1>${t('round1Lineup')}</h1><p class="subtitle">${esc(event.name)}</p></div></div><section class="panel"><div class="panel-head"><h2>${t('lineupTeam')}</h2><select id="lineup-team" aria-label="${t('lineupTeam')}"${teams.length === 1 ? ' disabled' : ''}>${teams.map(candidate => `<option value="${candidate.id}"${candidate.id === team.id ? ' selected' : ''}>${teamLabel(candidate)}</option>`).join('')}</select></div><form id="round1-lineup-form" class="panel-body" data-team-id="${team.id}"><p class="help">${t('lineupHint')}</p><div class="lineup-slots">${Array.from({ length: count }, (_, index) => `<div class="lineup-slot"><span class="lineup-position">${index + 1}</span><div class="lineup-slot-person" data-lineup-preview="${index}"></div><label><span class="field-label">${t('choosePlayer')}</span><select name="player_${index}" aria-label="${t('matchNo')} ${index + 1}"><option value="0">${t('randomSlot')}</option>${players.map(player => `<option value="${player.id}"${Number(values[index]) === player.id ? ' selected' : ''}>${esc(player.display_name)}</option>`).join('')}</select></label></div>`).join('')}</div><div class="event-editor-actions"><button type="button" class="outline" id="lineup-back">${t('backToMatch')}</button><button type="submit" class="primary accent">${t('saveLineup')}</button></div></form></section></main>`;
      const form = root.querySelector('#round1-lineup-form');
      const read = () => [...form.querySelectorAll('select')].map(select => Number(select.value));
      const update = () => {
        drafts[team.id] = read();
        for (const select of form.querySelectorAll('select')) {
          const index = Number(select.name.slice(7)), player = players.find(player => player.id === Number(select.value));
          root.querySelector(`[data-lineup-preview="${index}"]`).innerHTML = player ? person(player) : `<span class="help">${t('randomSlot')}</span>`;
          for (const option of select.options) option.disabled = Number(option.value) !== 0 && option.value !== select.value && drafts[team.id].includes(Number(option.value));
        }
      };
      form.addEventListener('change', update);
      form.addEventListener('formrestored', update);
      root.querySelector('#lineup-team').onchange = event => { update(); onTeam(Number(event.target.value)); };
      root.querySelector('#lineup-back').onclick = onBack;
      form.onsubmit = action(async event => {
        event.preventDefault();
        const ids = read(), manual = []; let random = false;
        for (const id of ids) {
          if (!id) random = true;
          else { if (random) throw Error(t('lineupHint')); manual.push(id); }
        }
        if (new Set(manual).size !== manual.length) throw Error(t('lineupDuplicate'));
        await onSave(team.id, manual);
      });
      bindCommon(); update();
    }
  };
})();
