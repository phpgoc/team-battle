(() => {
  'use strict';
  const $ = (s, root) => root.querySelector(s);
  const score = matches => matches.reduce((result, match) => { result[match.who_win === 'a' ? 0 : 1]++; return result; }, [0, 0]);
  const winner = matches => { const [a, b] = score(matches); return a > b ? 'a' : 'b'; };
  const finished = (draft, round) => Math.max(...score(draft.matches[round])) >= (round === 3 ? 1 : draft['round' + round + '_target']);
  const needsAce = draft => finished(draft, 1) && finished(draft, 2) && winner(draft.matches[1]) !== winner(draft.matches[2]);
  const teamIDs = (draft, side) => draft.entries.filter(([, team]) => team === side.toUpperCase()).map(([id]) => id);

  // 历史复活由录入者选择真实人选；只在全队均输过且目标高于人数时开放。
  function nextPlayers(draft, side) {
    const ids = teamIDs(draft, side), matches = draft.matches[2], last = matches.at(-1);
    if (last?.who_win === side) return { ids: [last[side + '_id']], carry: true, revival: false };
    const lost = new Set(matches.filter(match => match.who_win !== side).map(match => match[side + '_id']));
    const fresh = ids.filter(id => !lost.has(id));
    const revival = fresh.length === 0 && draft.round2_target > ids.length;
    const eligible = revival && draft.kof_revival_no_repeat ? window.TBKOF.revivalCandidates(ids, matches, side) : ids;
    return { ids: revival ? eligible : fresh, carry: false, revival };
  }

  function create(time) {
    return { step: 0, manager_id: 0, name: '', start_time: time, background_id: 0, round1_target: 4, round2_target: 5, kof_revival_no_repeat: false, ace_source: 'random', entries: [], matches: { 1: [], 2: [], 3: [] }, picks: {}, signature: '' };
  }

  function render(ctx) {
    const { draft: d, users, t, esc, field, input, startTimeInput, startTimeValue, backgroundOptions, backgrounds, shell, action, toast, person } = ctx;
    const redraw = () => render(ctx);
    const label = side => t(side === 'a' ? 'teamA' : 'teamB');
    const name = id => users.find(user => user.id === id)?.display_name || String(id);
    const player = id => users.find(user => user.id === id);
    const roundName = round => t(round === 1 ? 'round1' : round === 2 ? 'round2' : 'ace');
    const steps = [t('archiveBasics'), t('round1'), t('round2'), ...(needsAce(d) ? [t('ace')] : []), t('archiveReview')];
    const round = d.step >= 1 && d.step <= 3 ? d.step : 0;
    const currentIndex = d.step === 4 ? steps.length - 1 : d.step;
    const rows = (stage, editable) => `<ol class="archive-results">${d.matches[stage].map((match, index) => `<li><span class="archive-seq">${index + 1}</span><span class="${match.who_win === 'a' ? 'archive-won' : ''}">${esc(name(match.a_id))}</span><b>${label(match.who_win)} ${t('win')}</b><span class="${match.who_win === 'b' ? 'archive-won' : ''}">${esc(name(match.b_id))}</span>${editable ? `<button type="button" class="link" data-redo="${index}">${t('archiveRedo')}</button>` : ''}</li>`).join('')}</ol>`;
    const stageSummary = stage => `<div class="archive-stage-summary"><span>${roundName(stage)}</span><strong>A ${score(d.matches[stage]).join(' : ')} B</strong><span>${label(winner(d.matches[stage]))} ${t('win')}</span></div>`;
    let body;
    if (d.step === 0) {
      body = `<section class="panel"><div class="panel-body form-grid">${field('name', input('name', d.name, 'text', 'required'), true)}${field('time', startTimeInput(d.start_time))}${field('manager', `<select name="manager_id" data-searchable required><option value="">${t('selectManager')}</option>${ctx.managers.map(user => `<option value="${user.id}"${d.manager_id === user.id ? ' selected' : ''}>${esc(user.display_name)} (${esc(user.username)})</option>`).join('')}</select>`)}${field('background', `<select name="background_id">${backgroundOptions(backgrounds, d.background_id)}</select>`)}${field('round1Target', input('round1_target', d.round1_target, 'number', 'min="1" max="99" required'))}${field('round2Target', input('round2_target', d.round2_target, 'number', 'min="1" max="99" required'))}<div class="full" data-kof-revival-rule><label class="check"><input name="kof_revival_no_repeat" type="checkbox"${d.kof_revival_no_repeat ? ' checked' : ''}>${t('kofRevivalNoRepeat')}</label><p class="help">${t('kofRevivalNoRepeatHint')}</p></div></div></section>` + window.TBRosterEditor.markup({ users, entries: d.entries, editable: true, allowRandom: false, t, esc, person });
    } else if (d.step === 4) {
      const stages = [1, 2, ...(needsAce(d) ? [3] : [])];
      const wins = stages.filter(stage => winner(d.matches[stage]) === 'a').length;
      body = `<section class="panel"><div class="panel-head"><h2>${esc(d.name)}</h2><strong>${label(wins >= 2 ? 'a' : 'b')} ${t('win')} · ${wins} : ${stages.length - wins}</strong></div><div class="panel-body"><p class="help">${esc(d.start_time.slice(0, 10))} · ${esc(d.start_time.slice(11, 13))} ${t('hour')}</p><div class="archive-team-summary">${['a', 'b'].map(side => `<div><strong>${label(side)}</strong><p>${teamIDs(d, side).map(id => esc(name(id))).join('、')}</p></div>`).join('')}</div>${stages.map(stage => `<section class="archive-review-stage">${stageSummary(stage)}${stage === 3 ? `<p class="help">${t('aceSourceLabel')}：${sourceLabel(d.ace_source, t)}</p>` : ''}${rows(stage, false)}</section>`).join('')}<p class="help">${t('confirmEndHint')}</p></div></section>`;
    } else {
      const complete = finished(d, round), target = round === 3 ? 1 : d['round' + round + '_target'];
      const candidates = Object.fromEntries(['a', 'b'].map(side => [side, round === 2 ? nextPlayers(d, side) : { ids: teamIDs(d, side) }]));
      d.picks[round] ||= {};
      const picks = d.picks[round];
      for (const side of ['a', 'b']) if (!candidates[side].ids.includes(picks[side])) picks[side] = candidates[side].ids[0] || 0;
      const blocked = ['a', 'b'].some(side => !candidates[side].ids.length);
      const source = round === 3 ? field('aceSourceLabel', `<select name="ace_source">${['random', 'most_wins', 'most_losses', 'special:0', 'special:1'].map(value => `<option value="${value}"${d.ace_source === value ? ' selected' : ''}>${sourceLabel(value, t)}</option>`).join('')}</select>`) : '';
      body = `<section class="panel"><div class="panel-head"><h2>${roundName(round)}</h2><span class="archive-score">A <strong>${score(d.matches[round]).join(' : ')}</strong> B</span></div><div class="panel-body">${round === 3 ? '' : `<p class="help">${t('target')} ${target}</p>`}${round === 2 ? `<p class="help">${t('archiveKOFHint')}</p>` : ''}${source}${complete ? `<div class="archive-complete" role="status">${label(winner(d.matches[round]))} ${t('win')} · ${t('archiveStageComplete')}</div>` : `<h3>${t('matchNo')} ${d.matches[round].length + 1}</h3><div class="archive-current">${['a', 'b'].map(side => `<div class="archive-contender"><label class="field-label" for="archive-${side}">${label(side)}${candidates[side].carry ? ' · ' + t('archiveCarry') : ''}${candidates[side].revival ? ' · ' + t('archiveRevival') : ''}</label><select id="archive-${side}" name="past_${side}" required${candidates[side].carry ? ' disabled' : ''}>${candidates[side].ids.map(id => `<option value="${id}"${picks[side] === id ? ' selected' : ''}>${esc(name(id))}</option>`).join('')}</select><div class="archive-person" data-preview="${side}">${player(picks[side]) ? person(player(picks[side])) : ''}</div><button type="submit" class="primary" name="result" value="${side}"${blocked ? ' disabled' : ''}>${label(side)} ${t('win')}</button></div>`).join('<span class="archive-vs">VS</span>')}</div>${blocked ? `<p class="form-error">${t('archiveNoEligible')}</p>` : ''}`}${rows(round, true)}</div></section>`;
    }
    shell(`<div class="page-head"><div><h1>${t('archive')}</h1><p class="subtitle">${t('archiveIntro')}</p></div></div><ol class="archive-steps" aria-label="${t('stage')}">${steps.map((title, index) => `<li${index === currentIndex ? ' aria-current="step"' : ''}><span>${index + 1}</span>${title}</li>`).join('')}</ol><form id="archive-form">${body}<div class="archive-actions">${d.step > 0 ? `<button type="button" class="outline" id="archive-back">${t('previous')}</button>` : ''}${d.step === 0 ? `<button type="submit" class="primary">${t('next')}</button>` : d.step === 4 ? `<button type="submit" class="primary accent">${t('pastSave')}</button>` : `<button type="button" class="primary" id="archive-next"${finished(d, round) ? '' : ' disabled'}>${t('next')}</button>`}</div></form>`, 'archive');
    const form = document.getElementById('archive-form');
    const readBasics = () => {
      const values = new FormData(form);
      d.name = String(values.get('name') || ''); d.start_time = startTimeValue(values); d.background_id = Number(values.get('background_id')); d.manager_id = Number(values.get('manager_id'));
      d.round1_target = Number(values.get('round1_target')); d.round2_target = Number(values.get('round2_target'));
      d.kof_revival_no_repeat = values.has('kof_revival_no_repeat');
      refreshRevivalRule();
    };
    const refreshRevivalRule = () => { $('[data-kof-revival-rule]', form).hidden = !window.TBKOF.needsRevivalRule(window.TBKOF.teamSizes(d.entries), d.round2_target); };
    if (d.step === 0) {
      refreshRevivalRule();
      window.TBRosterEditor.bind($('#event-roster', form), { users, t, esc, person, onChange: () => {
        d.entries = [...form.querySelectorAll('[data-roster-player]')].map(row => [Number(row.dataset.rosterPlayer), $('select', row).value]);
        refreshRevivalRule();
      } });
      form.addEventListener('input', readBasics); form.addEventListener('change', readBasics);
    } else if (round) {
      form.addEventListener('change', event => {
        if (event.target.name === 'ace_source') d.ace_source = event.target.value;
        for (const side of ['a', 'b']) {
          const select = $(`[name="past_${side}"]`, form);
          if (!select) continue;
          d.picks[round][side] = Number(select.value);
          $(`[data-preview="${side}"]`, form).innerHTML = player(Number(select.value)) ? person(player(Number(select.value))) : '';
        }
      });
      form.querySelectorAll('[data-redo]').forEach(button => button.onclick = action(async () => {
        if (!(await window.TBConfirm.ask(t('archiveRedoConfirm')))) return;
        d.matches[round].splice(Number(button.dataset.redo));
        for (let stage = round + 1; stage <= 3; stage++) d.matches[stage] = [];
        d.picks = {}; redraw();
      }));
      $('#archive-next', form).onclick = () => {
        if (!finished(d, round)) return;
        d.step = round === 1 ? 2 : round === 2 && needsAce(d) ? 3 : 4;
        redraw();
      };
    }
    $('#archive-back', form)?.addEventListener('click', () => { d.step = d.step === 4 ? needsAce(d) ? 3 : 2 : d.step - 1; redraw(); });
    form.onsubmit = action(async event => {
      event.preventDefault();
      if (d.step === 0) {
        readBasics();
        if (!d.name.trim()) throw Error(t('archiveNameRequired'));
        if (d.entries.length < 6 || d.entries.length > 12 || !teamIDs(d, 'a').length || !teamIDs(d, 'b').length) throw Error(t('archiveRosterRequired'));
        if (teamIDs(d, 'a').length !== Math.ceil(d.entries.length / 2) || teamIDs(d, 'b').length !== Math.floor(d.entries.length / 2)) throw Error(t('archiveRosterBalance'));
        const signature = JSON.stringify([d.entries, d.round1_target, d.round2_target, d.kof_revival_no_repeat]);
        if (d.signature && d.signature !== signature && Object.values(d.matches).some(matches => matches.length)) {
          if (!(await window.TBConfirm.ask(t('archiveRosterReset')))) return;
          d.matches = { 1: [], 2: [], 3: [] }; d.picks = {};
        }
        d.signature = signature; d.step = 1; redraw();
      } else if (d.step === 4) {
        if (![1, 2, ...(needsAce(d) ? [3] : [])].every(stage => finished(d, stage))) throw Error(t('archiveIncomplete'));
        if (!(await window.TBConfirm.ask(t('confirmEndHint')))) return;
        await ctx.onSave({ name: d.name, manager_id: d.manager_id, start_time: d.start_time, background_id: d.background_id, round1_target: d.round1_target, round2_target: d.round2_target, kof_revival_no_repeat: d.kof_revival_no_repeat, ace_source: d.ace_source, player_ids: d.entries.map(([id]) => id), fixed_assignments: Object.fromEntries(d.entries), matches: [1, 2, 3].flatMap(stage => d.matches[stage]) });
      } else {
        if (finished(d, round)) return;
        const side = event.submitter?.value;
        if (!['a', 'b'].includes(side)) return;
        const picks = d.picks[round];
        if (!picks.a || !picks.b) { toast(t('archiveNoEligible')); return; }
        // 历史逐局直接记入草稿，核对后统一确认保存。
        d.matches[round].push({ round, a_id: picks.a, b_id: picks.b, who_win: side });
        delete d.picks[round]; redraw();
      }
    });
  }

  function sourceLabel(source, t) {
    if (source === 'most_wins') return t('sourceMostWins');
    if (source === 'most_losses') return t('sourceMostLosses');
    if (source.startsWith('special:')) return t('specialMatchGroup') + (Number(source.split(':')[1]) + 1);
    return t('random');
  }
  window.TBArchivePage = { create, render };
})();
