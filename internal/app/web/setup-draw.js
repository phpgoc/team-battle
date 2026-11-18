(() => {
  'use strict';
  // 动画只消费服务端已保存的抽签结果，不在浏览器决定比赛人选。
  window.TBSetupDraw = {
    async play({ steps, event, render, playerMarkup, translate, isActive }) {
      const people = event.teams.flatMap(team => team.players);
      const byID = id => people.find(player => player.id === id);
      const visible = new Set((event.setup_roster || []).filter(player => player.fixed_team).map(player => player.id));
      const progress = { visible, round1: 0, kof: false, step: null };
      const pause = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));
      const draw = () => { if (isActive()) render(progress); };
      const slots = () => [...document.querySelectorAll('.board-current-fighters .current-player')];
      const paint = ids => {
        slots().forEach((slot, index) => {
          const player = byID(ids[index]);
          slot.innerHTML = player ? playerMarkup(player) : `<span class="setup-empty-slot">${translate('toConfirm')}</span>`;
          slot.dataset.drawPlayer = String(player?.id || 0);
        });
      };
      const fly = async (sources, step) => {
        const animations = [];
        for (const [side, id] of [[0, step.a], [1, step.b], [0, step.extra_a]]) {
          if (!id || !sources[side]) continue;
          const column = document.querySelectorAll('.board-left > .board-round')[step.kind === 'round1' ? 0 : 1];
          const row = column?.querySelectorAll('.board-match')[(step.position || 1) - 1];
          const target = step.kind === 'group' ? document.querySelector(`[data-roster-player="${id}"]`) : row?.querySelectorAll('.board-player')[side];
          if (!target) continue;
          // 长名单和对阵表在自己的容器内滚动，不推动整个直播画面。
          const scroller = target.closest('.board-roster-list,.board-matches');
          if (scroller) {
            const box = target.getBoundingClientRect(), parent = scroller.getBoundingClientRect();
            if (box.bottom > parent.bottom || box.top < parent.top) scroller.scrollTop += box.top - parent.top;
          }
          const destination = target.getBoundingClientRect(), source = sources[side];
          const ghost = document.createElement('div');
          ghost.className = 'setup-flying-player';
          ghost.innerHTML = playerMarkup(byID(id));
          Object.assign(ghost.style, { left: `${source.left}px`, top: `${source.top}px`, width: `${source.width}px`, height: `${source.height}px` });
          document.body.append(ghost);
          target.classList.add('setup-arriving');
          const sx = Math.min(1, destination.width / Math.max(1, source.width));
          const sy = Math.min(1, destination.height / Math.max(1, source.height));
          const animation = ghost.animate([
            { transform: 'translate(0,0) scale(1)', opacity: 1 },
            { transform: `translate(${destination.left - source.left}px,${destination.top - source.top}px) scale(${sx},${sy})`, opacity: .35 }
          ], { duration: 650, easing: 'cubic-bezier(.2,.7,.3,1)', fill: 'forwards' });
          animations.push(animation.finished.catch(() => {}).then(() => { ghost.remove(); target.classList.remove('setup-arriving'); }));
        }
        await Promise.all(animations);
      };
      try {
        for (const step of steps) {
          if (!isActive()) return;
          progress.step = step;
          draw();
          const duration = Math.max(0, Number(event.rules.random_draw_seconds) || 0) * 1000;
          const until = Date.now() + duration;
          let tick = 0;
          while (Date.now() < until) {
            if (!isActive()) return;
            const aPool = step.pool_a || [], bPool = step.pool_b || [];
            const a = aPool.length ? aPool[tick % aPool.length] : step.a;
            let b = bPool.length ? bPool[(tick + 1) % bPool.length] : step.b;
            if (step.kind === 'group' && a === b) b = bPool.find(id => id !== a) || 0;
            paint([step.a ? a : 0, step.b ? b : 0]);
            tick += 1;
            await pause(Math.min(250, Math.max(0, until - Date.now())));
          }
          if (!isActive()) return;
          paint([step.a, step.b]);
          if (duration) await pause(180);
          if (!isActive()) return;
          const sources = slots().map(slot => slot.getBoundingClientRect());
          if (step.kind === 'group') [step.a, step.b, step.extra_a].filter(Boolean).forEach(id => visible.add(id));
          if (step.kind === 'round1') progress.round1 = step.position;
          if (step.kind === 'round2') progress.kof = true;
          draw();
          paint([step.a, step.b]);
          await fly(sources, step);
        }
      } finally {
        document.querySelectorAll('.setup-flying-player').forEach(node => node.remove());
      }
    }
  };
})();
