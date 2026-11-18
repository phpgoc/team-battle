(() => {
  'use strict';
  const sourceLabel = (source, special, t) => source === 'most_wins' ? t('sourceMostWins') : source === 'most_losses' ? t('sourceMostLosses') : source === 'special' ? `${t('specialMatchGroup')}${special || 1}` : source.startsWith('special:') ? `${t('specialMatchGroup')}${Number(source.slice(8)) + 1}` : source === 'mixed' ? t('mixed') : t('random');
  const playerFrames = new WeakMap();

  // 只平滑替换对战位中的人像和姓名，最终结果继续使用同一个对战位。
  function swapPlayer(slot, markup, key, { animate = true, className = slot.className } = {}) {
    const previous = playerFrames.get(slot);
    if (previous?.key === key && previous.markup === markup && slot.className === className) return;
    previous?.animations.forEach(animation => animation.cancel());
    slot.querySelectorAll('.draw-player-leaving').forEach(node => node.remove());
    const leaving = animate && slot.hasChildNodes() ? slot.cloneNode(true) : null;
    slot.className = className;
    slot.innerHTML = markup;
    slot.dataset.drawPlayer = String(key || 0);
    const animations = [];
    if (leaving) {
      leaving.removeAttribute('data-draw-player');
      leaving.classList.add('draw-player-leaving');
      leaving.setAttribute('aria-hidden', 'true');
      leaving.querySelectorAll('button,a').forEach(node => node.setAttribute('tabindex', '-1'));
      const incoming = [...slot.children];
      slot.append(leaving);
      const options = { duration: 180, easing: 'cubic-bezier(.2,.7,.3,1)' };
      const outgoing = leaving.animate([{ opacity: 1, transform: 'translateY(0)' }, { opacity: 0, transform: 'translateY(-16px)' }], options);
      animations.push(outgoing, ...incoming.map(node => node.animate([{ opacity: 0, transform: 'translateY(16px)' }, { opacity: 1, transform: 'translateY(0)' }], options)));
      outgoing.finished.catch(() => {}).then(() => leaving.remove());
    }
    playerFrames.set(slot, { key, markup, animations });
  }

  window.TBMatchDraw = {
    sourceLabel,
    swapPlayer,
    record(event, { t, esc, recordName }) {
      const draw = event.ace_draw;
      if (!event.ace_draw_source) return '';
      const label = sourceLabel(event.ace_draw_source, event.ace_draw_special, t);
      const people = event.teams.flatMap(team => team.players);
      const player = id => people.find(person => person.id === id);
      const names = ids => ids.map(player).filter(Boolean).map(recordName).join('、');
      const candidates = draw?.candidate_ids?.length === 2 ? `<div class="ace-draw-candidates"><b>② ${t('aceCandidateStep')}</b>${draw.candidate_ids.map((ids, side) => `<p><span>${t(side ? 'teamB' : 'teamA')} · ${t(draw.rules?.[side] === 'random_fallback' ? 'random' : ids.length === 1 ? 'aceDirectPick' : 'aceCandidateStep')}</span>${names(ids)}</p>`).join('')}</div>` : '';
      const picked = draw?.player_a_id && draw?.player_b_id ? `<div><b>③ ${t('aceFinalStep')}</b><p>${names([draw.player_a_id])} <span>VS</span> ${names([draw.player_b_id])}</p></div>` : '';
      return `<div class="ace-draw-record"><span class="board-ace-source">① ${t('aceRuleStep')} · <strong>${esc(label)}</strong></span>${candidates || picked ? `<details class="ace-draw-details"><summary>${t('aceDrawDetails')}${draw.adjusted ? ` · ${t('aceAdjusted')}` : ''}</summary><div class="ace-draw-popover"><div><b>① ${t('aceRuleStep')}</b><p>${esc(label)}</p></div>${candidates}${picked}${draw.adjusted ? `<p>${t('aceAdjustedHint')}</p>` : ''}</div></details>` : ''}</div>`;
    },
    plan(event, match, candidates) {
      const duration = Math.max(0, Number(match.stage === 'ace' ? event.rules.ace_draw_seconds : event.rules.random_draw_seconds) || 0) * 1000;
      const phases = [];
      if (match.stage === 'ace') {
        const rules = event.rules, choices = [];
        if (rules.ace_most_wins > 0) choices.push('most_wins');
        if (rules.ace_most_losses > 0) choices.push('most_losses');
        (rules.ace_specials || []).forEach((group, index) => { if (group.probability > 0) choices.push(`special:${index}`); });
        const used = Number(rules.ace_most_wins) + Number(rules.ace_most_losses) + (rules.ace_specials || []).reduce((sum, group) => sum + Number(group.probability || 0), 0);
        if (used < 100) choices.push('random');
        phases.push({ kind: 'rule', duration, choices: choices.length ? choices : ['random'] });
      }
      if (candidates.some(list => list.length > 1)) phases.push({ kind: 'players', duration });
      return { phases, duration: phases.reduce((sum, phase) => sum + phase.duration, 0) };
    },
    // 每段按绝对开始时间推进；SSE重绘不会重启，真实结果始终取自服务端。
    frame(plan, elapsed) {
      let start = 0;
      for (const [index, phase] of plan.phases.entries()) {
        if (elapsed < start + phase.duration) return { ...phase, index, tick: Math.floor((elapsed - start) / 250), settling: start + phase.duration - elapsed <= 250 };
        start += phase.duration;
      }
      return null;
    }
  };
})();
