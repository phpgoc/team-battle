(() => {
  'use strict';
  window.TBTournamentManagement = {
    render({ tournaments, filters, t, shell, field, input, tournamentDateField, eventTable, go, action, confirmAction, api, toast, reload }) {
      shell(`
        <div class="page-head"><div><h1>${t('tournamentManagement')}</h1><p class="subtitle">${t('tournamentManagementHint')}</p></div></div>
        <section class="panel">
          <form id="tournament-filter" class="filter-bar">
            ${field('name', input('name', filters.name, 'search'))}
            ${tournamentDateField('from', filters.from)}
            ${tournamentDateField('to', filters.to)}
            <div class="history-filter-actions"><button class="primary">${t('search')}</button><button type="button" class="ghost" data-go="/tournaments">${t('clear')}</button></div>
          </form>
          ${eventTable(tournaments, true)}
        </section>`, 'tournamentManagement');
      document.getElementById('tournament-filter').onsubmit = action(event => {
        event.preventDefault();
        const values = Object.fromEntries(new FormData(event.currentTarget));
        if (values.from && values.to && values.from > values.to) throw Error(t('dateRangeError'));
        go('/tournaments?' + new URLSearchParams(values));
      });
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
