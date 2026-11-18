(() => {
  'use strict';
  // 预估随机分队后的两队人数，与服务端优先补齐人数较少一队的规则一致。
  function teamSizes(entries) {
    const sizes = ['A', 'B'].map(side => entries.filter(([, team]) => team === side).length);
    for (let left = entries.length - sizes[0] - sizes[1]; left > 0; left--) sizes[sizes[0] <= sizes[1] ? 0 : 1]++;
    return sizes;
  }
  const needsRevivalRule = (sizes, target) => sizes.every(size => size > 0) && target - Math.min(...sizes) >= 2;
  // 仅供败方全员淘汰后的复活使用；胜者连续留场不增加淘汰次数。
  function revivalCandidates(ids, matches, side) {
    const losses = new Map(ids.map(id => [id, 0]));
    for (const match of matches) {
      const id = match[side + '_id'];
      if (match.who_win !== side && losses.has(id)) losses.set(id, losses.get(id) + 1);
    }
    const minimum = Math.min(...losses.values());
    return ids.filter(id => losses.get(id) === minimum);
  }
  window.TBKOF = { teamSizes, needsRevivalRule, revivalCandidates };
})();
