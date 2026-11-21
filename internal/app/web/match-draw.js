(() => {
  'use strict';
  const sourceLabel = (source, special, t) => source === 'most_wins' ? t('sourceMostWins') : source === 'most_losses' ? t('sourceMostLosses') : source === 'special' ? `${t('specialMatchGroup')}${special || 1}` : source.startsWith('special:') ? `${t('specialMatchGroup')}${Number(source.slice(8)) + 1}` : source === 'mixed' ? t('mixed') : t('random');
  const stepTime = 1000 / 3;
  const reducedMotion = matchMedia('(prefers-reduced-motion: reduce)');
  const slotFrames = new WeakMap();

  // 每秒推进三个候选，最后一格延长并缓出。候选只用于展示，落点由服务端给出。
  function reelFrame(pool, result, elapsed, duration, offset = 0) {
    if (pool.length < 2 || elapsed >= duration || reducedMotion.matches) return { from: result, to: result, progress: 1 };
    const lastStep = Math.max(0, Math.ceil(duration / stepTime) - 2);
    const index = Math.floor(elapsed / stepTime);
    const candidate = step => pool[(step + offset) % pool.length];
    if (index >= lastStep) {
      const start = lastStep * stepTime;
      const progress = Math.min(1, (elapsed - start) / Math.max(1, duration - start - 100));
      const from = candidate(lastStep) === result ? candidate(lastStep - 1) : candidate(lastStep);
      return { from, to: result, progress: 1 - (1 - progress) ** 3 };
    }
    const progress = Math.min(1, (elapsed - index * stepTime) / (stepTime * .88));
    return { from: candidate(index), to: candidate(index + 1), progress: progress * progress * (3 - 2 * progress) };
  }

  // 两层内容在同一裁切框内交接，停定后保留实际出场选手的节点。
  function paintSlot(slot, { from, to, progress }, markup, mode) {
    if (!slot) return;
    let frames = slotFrames.get(slot);
    if (!frames) {
      const first = slot.firstElementChild;
      const second = first.cloneNode(false);
      slot.append(second);
      frames = [first, second];
      slotFrames.set(slot, frames);
    }
    const moving = from !== to && progress < 1;
    const gradient = mode === 'gradient';
    const leading = moving ? from : to;
    const first = frames.find(node => node.dataset.drawValue === String(leading))
      || frames.find(node => node.dataset.drawValue !== String(to)) || frames[0];
    const second = frames.find(node => node !== first);
    frames.used = first;
    for (const [index, layer] of [first, second].entries()) {
      if (index && !moving) {
        layer.hidden = true;
        break;
      }
      const value = index ? to : leading;
      if (layer.dataset.drawValue !== String(value)) {
        layer.innerHTML = markup(value);
        layer.dataset.drawValue = String(value);
      }
      const distance = moving ? (index - progress) : 0;
      layer.hidden = false;
      layer.inert = moving;
      layer.style.transform = gradient || !distance ? '' : `translate3d(0,${distance * 100}%,0)`;
      layer.style.opacity = moving ? String(gradient ? 1 - Math.abs(distance) : 1 - Math.abs(distance) * .62) : '';
      // 渐变时头像可叠化。姓名在交接点先隐去再显现，避免两行字重叠。
      const labelOpacity = moving && gradient ? String(Math.max(0, 1 - Math.abs(distance) * 2)) : '';
      layer.style.setProperty('--draw-label-opacity', labelOpacity);
    }
  }

  function stopSlot(slot) {
    const frames = slotFrames.get(slot);
    if (!frames) return;
    const active = frames.used;
    const inactive = frames.find(node => node !== active);
    active.inert = false;
    active.style.transform = '';
    active.style.opacity = '';
    active.style.removeProperty('--draw-label-opacity');
    inactive.remove();
    slotFrames.delete(slot);
  }

  function extraInfoItems(event, match) {
    const values = match.extra_info || {};
    const selections = event.rules.extra_info?.[match.stage] || [];
    const items = [];
    const seen = new Set();
    for (const selection of selections) {
      const name = selection.name;
      if (!name || !Object.prototype.hasOwnProperty.call(values, name) || !values[name]) continue;
      items.push({ name, value: values[name] });
      seen.add(name);
    }
    for (const [name, value] of Object.entries(values)) {
      if (name && value && !seen.has(name)) items.push({ name, value });
    }
    return items;
  }

  function extraInfoVisibleCount(plan, elapsed) {
    if (!plan.extraInfo.length || elapsed < plan.duration + 1000) return 0;
    return Math.min(plan.extraInfo.length, Math.floor((elapsed - plan.duration) / 1000));
  }

  window.TBMatchDraw = {
    sourceLabel,
    reelFrame,
    paintSlot,
    stopSlot,
    tickMilliseconds: stepTime,
    extraInfoVisibleCount,
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
      const extraInfo = extraInfoItems(event, match);
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
      const drawDuration = phases.reduce((sum, phase) => sum + phase.duration, 0);
      const extraInfoDuration = extraInfo.length ? (extraInfo.length + 1) * 1000 : 0;
      return { phases, duration: drawDuration, extraInfo, totalDuration: drawDuration + extraInfoDuration };
    },
    // 每段按绝对开始时间推进；SSE重绘不会重启，真实结果始终取自服务端。
    frame(plan, elapsed) {
      let start = 0;
      for (const [index, phase] of plan.phases.entries()) {
        if (elapsed < start + phase.duration) return { ...phase, index, elapsed: elapsed - start };
        start += phase.duration;
      }
      return null;
    }
  };
})();
