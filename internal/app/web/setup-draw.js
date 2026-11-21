(() => {
  'use strict';
  // 动画只消费服务端已保存的抽签结果，不在浏览器决定比赛人选。
  window.TBSetupDraw = {
    initialProgress(steps, event) {
      const drawn = new Set(steps.filter(step => step.kind === 'group').flatMap(step => [step.a, step.b, step.extra_a]));
      const visible = new Set(event.teams.flatMap(team => team.players).filter(player => !drawn.has(player.id)).map(player => player.id));
      return { visible, round1: 0, kof: false, step: steps[0], extraInfoVisibleCount: 0 };
    },
    async play({ steps, event, progress, render, playerMarkup, translate, isActive }) {
      const people = event.teams.flatMap(team => team.players);
      const byID = id => people.find(player => player.id === id);
      const pause = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));
      const waitUntil = async deadline => {
        while (Date.now() < deadline) {
          if (!isActive()) return false;
          await pause(Math.min(100, deadline - Date.now()));
        }
        return isActive();
      };
      const extraInfoCount = step => (event.rules.extra_info?.[step.kind] || []).filter(selection => step.extra_info?.[selection.name]).length;
      const draw = () => { if (isActive()) render(progress); };
      const slots = () => [...document.querySelectorAll('.board-current-side > .current-player')];
      const animation = window.TBMatchDraw;
      const mode = event.rules.draw_animation;
      const reduced = matchMedia('(prefers-reduced-motion: reduce)').matches;
      const paint = (step, elapsed, duration) => {
        slots().forEach((slot, side) => {
          const id = side ? step.b : step.a;
          const pool = side ? step.pool_b : step.pool_a;
          const frame = animation.reelFrame(id ? pool || [] : [], id || 0, elapsed, duration, side);
          animation.paintSlot(slot, frame, id => playerMarkup(byID(id) || { display_name: translate('toConfirm') }, step.kind), mode);
        });
      };
      const fly = async (sources, step) => {
        const animations = [];
        for (const [side, id] of [[0, step.a], [1, step.b], [0, step.extra_a]]) {
          if (reduced || !id || !sources[side]) continue;
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
          ghost.setAttribute('aria-hidden', 'true');
          ghost.inert = true;
          ghost.innerHTML = playerMarkup(byID(id), step.kind);
          Object.assign(ghost.style, { left: `${source.left}px`, top: `${source.top}px`, width: `${source.width}px`, height: `${source.height}px` });
          document.body.append(ghost);
          target.classList.add('setup-arriving');
          const sx = Math.min(1, destination.width / Math.max(1, source.width));
          const sy = Math.min(1, destination.height / Math.max(1, source.height));
          const animation = ghost.animate([
            { transform: 'translate(0,0) scale(1)', opacity: 1 },
            { transform: `translate(${destination.left - source.left}px,${destination.top - source.top}px) scale(${sx},${sy})`, opacity: 0 }
          ], { duration: 650, easing: 'cubic-bezier(.2,.7,.3,1)', fill: 'forwards' });
          animations.push(animation.finished.catch(() => {}).then(() => { ghost.remove(); target.classList.remove('setup-arriving'); }));
        }
        await Promise.all(animations);
      };
      try {
        for (const step of steps) {
          if (!isActive()) return;
          progress.step = step;
          progress.extraInfoVisibleCount = 0;
          draw();
          const duration = Math.max(0, Number(event.rules.random_draw_seconds) || 0) * 1000;
          const drawStartedAt = Date.now();
          const start = performance.now();
          const section = document.querySelector('.board-current-match');
          section.dataset.drawPhase = 'players';
          const roll = section.querySelector('#draw-roll');
          roll.hidden = !duration;
          roll.querySelector('.draw-roll-label').textContent = translate(step.kind === 'group' ? 'setupGrouping' : step.kind);
          await new Promise(resolve => {
            const frame = now => {
              if (!isActive()) { resolve(); return; }
              const elapsed = Math.max(0, now - start);
              paint(step, elapsed, duration);
              if (elapsed < duration) requestAnimationFrame(frame);
              else resolve();
            };
            frame(start);
          });
          if (!isActive()) return;
          roll.hidden = true;
          const sources = slots().map(slot => slot.getBoundingClientRect());
          if (step.kind === 'group') [step.a, step.b, step.extra_a].filter(Boolean).forEach(id => progress.visible.add(id));
          if (step.kind === 'round1') progress.round1 = step.position;
          if (step.kind === 'round2') progress.kof = true;
          draw();
          await fly(sources, step);
          const infoCount = extraInfoCount(step);
          if (infoCount) {
            for (let count = 1; count <= infoCount; count++) {
              if (!(await waitUntil(drawStartedAt + duration + count * 1000))) return;
              progress.extraInfoVisibleCount = count;
              draw();
            }
            if (!(await waitUntil(drawStartedAt + duration + (infoCount + 1) * 1000))) return;
          }
        }
      } finally {
        document.querySelectorAll('.setup-flying-player').forEach(node => node.remove());
      }
    }
  };
})();
