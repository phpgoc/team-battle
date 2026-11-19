(() => {
  'use strict';
  window.TBTournamentManagement = {
    render({ tournaments, t, shell, eventTable, action, confirmAction, api, toast, reload }) {
      shell(`<div class="page-head"><div><h1>${t('tournamentManagement')}</h1><p class="subtitle">${t('tournamentManagementHint')}</p></div></div><section class="panel">${eventTable(tournaments, true)}</section>`, 'tournamentManagement');
      document.querySelectorAll('[data-tournament-action]').forEach(button => {
        button.addEventListener('click', action(async () => {
          const id = Number(button.dataset.tournamentId), deleted = button.dataset.tournamentAction === 'delete';
          const tournament = tournaments.find(item => item.id === id);
          if (!tournament) return;
          if (deleted && !(await confirmAction(t('deleteTournamentConfirm').replace('{name}', tournament.name)))) return;
          // 导航离开列表后不执行旧按钮的操作，也不将用户带回旧页面。
          if (!button.isConnected) return;
          await api('/api/tournaments/' + id + (deleted ? '' : '/restore'), { method: deleted ? 'DELETE' : 'POST' });
          if (button.isConnected) await reload();
          toast(t(deleted ? 'tournamentDeleted' : 'tournamentRestored'));
        }));
      });
    }
  };
})();
