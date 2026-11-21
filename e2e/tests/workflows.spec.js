const { test, expect, login, json, players, assignments, createTournament, image } = require('./helpers.cjs');

test('额外信息在选手抽签后逐秒揭晓并留出展示时间', async ({ page }) => {
  await page.goto('/login');
  const timeline = await page.evaluate(() => {
    const event = { rules: { random_draw_seconds: 4, ace_draw_seconds: 5, extra_info: { round1: [{ name: '地图' }, { name: '赛事模式' }] } } };
    const match = { stage: 'round1', extra_info: { '地图': 'Circuit Breaker', '赛事模式': '昼间' } };
    const plan = window.TBMatchDraw.plan(event, match, [[1, 2], [3, 4]]);
    const times = [3999, 4000, 4999, 5000, 5999, 6000, 6999, 7000];
    return { total: plan.totalDuration, visible: times.map(time => window.TBMatchDraw.extraInfoVisibleCount(plan, time)) };
  });
  expect(timeline).toEqual({ total: 7000, visible: [0, 0, 0, 1, 1, 2, 2, 2] });
});

test('抽签转盘每秒切换三次且结果相邻结果不同', async ({ page }) => {
  await page.goto('/login');
  const frame = await page.evaluate(() => window.TBMatchDraw.reelFrame([1, 2], 1, 3800, 4000));
  expect(frame.from).toBe(2);
  expect(frame.to).toBe(1);
  expect(frame.progress).toBeGreaterThan(0);
  expect(frame.progress).toBeLessThan(1);
  expect(await page.evaluate(() => window.TBMatchDraw.tickMilliseconds)).toBeCloseTo(1000 / 3, 4);
});

test('管理员KOF抽签先揭晓选手，再逐项揭晓额外信息', async ({ page }) => {
  await login(page);
  await json(page.request, 'PATCH', '/api/site-settings/tournament-defaults', { draw_animation: 'slide' });
  for (const [name, option] of [['地图演示', '地图甲'], ['比赛模式', '昼间']]) {
    const response = await page.request.post('/api/site-settings/extra-infos', { multipart: { name, options: option } });
    expect(response.ok(), await response.text()).toBeTruthy();
  }
  const definitions = (await json(page.request, 'GET', '/api/site-settings')).extra_infos;
  const roster = await players(page.request, '逐项揭晓');
  const extraInfo = { round2: definitions.map(definition => ({ definition_id: definition.id, selected: [] })) };
  const created = await createTournament(page.request, '逐项揭晓赛事', roster, {
    round1_target: 1, round2_target: 4,
    rules: {
      round1_mode: 'random', kof_starter: 'random', no_repeat: true,
      random_draw_seconds: 4, ace_draw_seconds: 0, ace_most_wins: 20, ace_most_losses: 20, ace_specials: [],
      extra_info: extraInfo
    }
  });
  await json(page.request, 'POST', `/api/tournaments/${created.id}/setup-draw`, {});
  await json(page.request, 'POST', `/api/tournaments/${created.id}/captains/confirm`, { team_a_player_id: roster[0].id, team_b_player_id: roster[3].id });
  await json(page.request, 'POST', `/api/tournaments/${created.id}/start`, {});
  let before = await json(page.request, 'GET', `/api/public/tournaments/${created.id}`);
  const firstRoundMatch = before.matches.find(match => match.status === 'pending');
  await json(page.request, 'POST', `/api/tournaments/${created.id}/matches/${firstRoundMatch.id}/result`, { winner_team_id: before.teams[0].id });
  before = await json(page.request, 'GET', `/api/public/tournaments/${created.id}`);
  const firstKOFMatch = before.matches.find(match => match.stage === 'round2' && match.status === 'pending');
  expect(firstKOFMatch).toBeTruthy();
  await page.goto(`/live/${created.id}`);
  await expect(page.locator('.board-current-extra-info-rows dd')).toHaveCount(2);
  const recordAWin = async () => {
    await page.locator(`[data-current-winner="${before.teams[0].id}"]`).click();
    await page.locator('[data-confirm-accept]').click();
  };
  const sendNextKOFPlayer = async verifyDraw => {
    const choices = await json(page.request, 'GET', `/api/tournaments/${created.id}/kof/options`);
    const choice = choices.teams.find(team => team.candidate_ids?.length);
    expect(choice).toBeTruthy();
    const row = page.locator(`[data-roster-player="${choice.candidate_ids[0]}"]`);
    await row.locator('.roster-action-area').click();
    await page.locator('#roster-actions [data-send-kof]').click();
    if (verifyDraw) {
      await expect(page.locator('.board-current-match')).toHaveAttribute('data-draw-phase', /^(players|reveal)$/);
    }
    await expect(page.locator('[data-current-winner]').first()).toBeEnabled();
  };
  await recordAWin();
  await sendNextKOFPlayer(true);
  await recordAWin();
  await sendNextKOFPlayer();
  await recordAWin();
  const values = () => page.locator('.board-current-extra-info-rows dd:visible').allTextContents();
  await expect(page.locator('.board-current-match')).toHaveAttribute('data-draw-phase', 'players');
  await expect.poll(values).toEqual([]);
  await expect.poll(values, { timeout: 10000, intervals: [100] }).toEqual(['地图甲']);
  await expect.poll(values, { timeout: 10000 }).toEqual(['地图甲', '昼间']);
  await expect(page.locator('[data-current-winner]').first()).toBeEnabled();
  const pendingMatchDetails = page.locator('[data-stage="round2"] .board-match.is-pending .board-extra-info-trigger');
  await expect(pendingMatchDetails).toBeVisible();
  await pendingMatchDetails.click();
  const details = page.locator('.board-extra-info-popover:popover-open');
  await expect(details).toContainText('地图演示');
  await expect(details).toContainText('比赛模式');
  await expect(details).toContainText('地图甲');
  await expect(details).toContainText('昼间');
});

test('管理员自动KOF复活抽签在原对战位连续滚动并落定', async ({ page }) => {
  await login(page);
  const roster = await players(page.request, '自动复活动画');
  const created = await createTournament(page.request, '自动复活抽签动画', roster, {
    round1_target: 1,
    round2_target: 5,
    rules: {
      round1_mode: 'random', kof_starter: 'random', no_repeat: true,
      random_draw_seconds: 4, ace_draw_seconds: 0,
      ace_most_wins: 0, ace_most_losses: 0, ace_specials: []
    }
  });
  const api = '/api/tournaments/' + created.id;
  const detail = () => json(page.request, 'GET', '/api/public/tournaments/' + created.id);
  await json(page.request, 'POST', api + '/setup-draw', {});
  let event = await detail();
  await json(page.request, 'POST', api + '/captains/confirm', {
    team_a_player_id: event.teams[0].players[0].id,
    team_b_player_id: event.teams[1].players[0].id
  });
  await json(page.request, 'POST', api + '/start', {});
  event = await detail();
  let match = event.matches.find(row => row.stage === 'round1' && row.status === 'pending');
  await json(page.request, 'POST', api + '/matches/' + match.id + '/result', { winner_team_id: event.teams[0].id });

  const lost = new Set();
  const result = async (winnerSide, assignFreshPlayer) => {
    event = await detail();
    match = event.matches.find(row => row.stage === 'round2' && row.status === 'pending');
    expect(match).toBeTruthy();
    const loserSide = winnerSide === 0 ? 1 : 0;
    const loserID = loserSide === 0 ? match.player_a_id : match.player_b_id;
    const body = { winner_team_id: event.teams[winnerSide].id };
    lost.add(loserID);
    if (assignFreshPlayer) {
      const loserTeam = event.teams[loserSide];
      body.next_player_id = loserTeam.players.find(player => !lost.has(player.id)).id;
    }
    await json(page.request, 'POST', api + '/matches/' + match.id + '/result', body);
  };
  await result(0, true);
  await result(1, true);
  await result(0, true);
  await result(1, true);

  await page.goto('/live/' + created.id);
  const before = await detail();
  const finalKOF = before.matches.find(row => row.stage === 'round2' && row.status === 'pending');
  expect(finalKOF).toBeTruthy();
  await page.locator(`[data-current-winner="${before.teams[0].id}"]`).click();
  await page.locator('[data-confirm-accept]').click();
  const current = page.locator('.board-current-match');
  await expect(current).toHaveAttribute('data-draw-phase', /^(players|reveal)$/);
  const final = await detail();
  const resultMatch = final.matches.find(row => row.stage === 'round2' && row.status === 'pending');
  expect(resultMatch).toBeTruthy();
  await expect(page.locator('.current-player .player-record-link').first()).toContainText(resultMatch.player_a_name);
  await expect(page.locator('.current-player .player-record-link').last()).toContainText(resultMatch.player_b_name);
  await expect(page.locator('[data-current-winner]').first()).toBeEnabled({ timeout: 10000 });
  await expect(current).not.toHaveAttribute('data-draw-phase', 'players');
});

test('赛前抽签揭晓KOF额外信息并显示对阵详情', async ({ page }) => {
  await login(page);
  for (const [name, option] of [['赛前地图类型', '地图甲'], ['赛前模式类型', '昼间']]) {
    const response = await page.request.post('/api/site-settings/extra-infos', { multipart: { name, options: option } });
    expect(response.ok(), await response.text()).toBeTruthy();
  }
  const definitions = (await json(page.request, 'GET', '/api/site-settings')).extra_infos.filter(info => ['赛前地图类型', '赛前模式类型'].includes(info.name));
  const roster = await players(page.request, '赛前地图');
  const assignments = Object.fromEntries(roster.map((player, index) => [player.id, index < 3 ? 'A' : 'B']));
  const created = await createTournament(page.request, '赛前地图抽签', roster, {
    fixed_assignments: assignments,
    round1_target: 1,
    rules: {
      round1_mode: 'random', kof_starter: 'random', random_draw_seconds: 0,
      extra_info: { round2: definitions.map(definition => ({ definition_id: definition.id, selected: [] })) }
    }
  });
  await page.goto(`/live/${created.id}/setting`);
  await expect(page.locator('[data-extra-info-config]')).toHaveCount(2);
  await page.locator('#start-setup-draw').click();
  await expect(page.locator('#board-content')).not.toHaveAttribute('data-setup-drawing', 'true', { timeout: 10000 });
  const kofMatch = page.locator('[data-stage="round2"] .board-match').first();
  await expect(kofMatch.locator('.board-extra-info-trigger')).toBeVisible();
  await kofMatch.locator('.board-extra-info-trigger').click();
  const detail = page.locator('.board-extra-info-popover:popover-open');
  await expect(detail).toContainText('赛前地图类型');
  await expect(detail).toContainText('赛前模式类型');
  await expect(detail).toContainText('地图甲');
  await expect(detail).toContainText('昼间');
  await page.keyboard.press('Escape');
  const savedSetupDetails = page.locator('[data-stage="round2"] .board-match .board-extra-info-trigger');
  await expect(savedSetupDetails).toBeVisible();
  await savedSetupDetails.click();
  const savedPopover = page.locator('.board-extra-info-popover:popover-open');
  await expect(savedPopover).toContainText('地图甲');
  await expect(savedPopover).toContainText('昼间');

  const publicBeforeStart = await json(page.request, 'GET', `/api/public/tournaments/${created.id}`);
  const [teamA, teamB] = publicBeforeStart.teams;
  await json(page.request, 'POST', `/api/tournaments/${created.id}/captains/confirm`, {
    team_a_player_id: teamA.players[0].id, team_b_player_id: teamB.players[0].id
  });
  await json(page.request, 'POST', `/api/tournaments/${created.id}/start`, {});
  let publicAfterStart = await json(page.request, 'GET', `/api/public/tournaments/${created.id}`);
  const firstRound = publicAfterStart.matches.find(match => match.stage === 'round1' && match.status === 'pending');
  expect(firstRound).toBeTruthy();
  await json(page.request, 'POST', `/api/tournaments/${created.id}/matches/${firstRound.id}/result`, { winner_team_id: teamA.id });
  publicAfterStart = await json(page.request, 'GET', `/api/public/tournaments/${created.id}`);
  const initialKOF = publicAfterStart.matches.find(match => match.stage === 'round2');
  expect(initialKOF.extra_info).toEqual({ '赛前地图类型': '地图甲', '赛前模式类型': '昼间' });
});

test('赛事管理按赛事名和开始日期筛选，后台页面不显示重复页脚', async ({ page }) => {
  await login(page, '/tournaments');
  const before = (await json(page.request, 'GET', '/api/tournaments')).tournaments.length;
  const roster = await players(page.request, '赛事筛选');
  await createTournament(page.request, '日期筛选甲', roster, { start_time: '2026-11-19T19' });
  await createTournament(page.request, '日期筛选乙', roster, { start_time: '2026-11-18T19' });
  await page.goto('/tournaments');
  await expect(page.locator('.site-footer')).toHaveCount(0);
  await page.locator('#tournament-filter [name=name]').fill('日期筛选甲');
  await page.locator('#tournament-filter [name=from]').fill('2026-11-19');
  await page.locator('#tournament-filter [name=to]').fill('2026-11-19');
  await page.locator('#tournament-filter button.primary').click();
  await expect(page.locator('.event-table tbody tr')).toHaveCount(1);
  await expect(page.locator('.event-table')).toContainText('日期筛选甲');
  expect(new URL(page.url()).searchParams.get('from')).toBe('2026-11-19');
  await page.locator('#tournament-filter [data-go="/tournaments"]').click();
  await expect(page.locator('.event-table tbody tr')).toHaveCount(before + 2);
  await page.goto('/site-settings');
  await expect(page.locator('.site-footer')).toHaveCount(0);
  await page.goto('/tournaments/new');
  await expect(page.locator('.site-footer')).toHaveCount(0);
  await page.goto('/tournaments/archive');
  await expect(page.locator('.site-footer')).toHaveCount(0);
});

test('登录校验、单设备会话替换与自动退出', async ({ page, browser }) => {
  await page.goto('/accounts');
  await expect(page).toHaveURL(/\/login\?return=/);
  await page.locator('[name=username]').fill('admin');
  await page.locator('[name=password]').fill('wrong-password');
  await page.locator('#login-form button.primary').click();
  await expect(page.locator('.form-error')).toBeVisible();
  await login(page);
  const other = await browser.newContext();
  try {
    const second = await other.newPage();
    await login(second);
    // 触发受保护的资料读取，旧设备应回到登录页。
    await page.locator('[data-go="/account"]').click();
    await expect(page).toHaveURL(/\/login/);
    await expect(second.locator('#account-search')).toBeVisible();
  } finally { await other.close(); }
});

test('账号创建、密码二次校验、头像和手机搜索', async ({ page }) => {
  await login(page, '/accounts/new');
  await page.locator('[name=username]').fill('E2EPlayer');
  await page.locator('[name=display_name]').fill('端到端选手');
  await page.locator('[name=avatar]').setInputFiles(image);
  await expect(page.locator('#avatar-preview img')).toBeVisible();
  await page.locator('[name=new_password]').fill('test1234');
  await page.locator('[name=new_password_confirm]').fill('different-password');
  await page.locator('#user-form button[type=submit]').click();
  await expect(page.locator('.form-error')).toContainText('两次');
  await page.locator('[name=new_password_confirm]').fill('test1234');
  await page.locator('#user-form button[type=submit]').click();
  await expect(page).toHaveURL(/\/accounts$/);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.locator('#account-search').fill('E2EPlayer');
  const row = page.locator('[data-account-row]:visible');
  await expect(row).toHaveCount(1);
  await expect(row.locator('.person strong')).toHaveText('端到端选手');
  await expect(row.locator('.person small')).toHaveText('E2EPlayer');
  const dimensions = await row.locator('.person strong').boundingBox();
  expect(dimensions.width).toBeGreaterThan(60);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
  await page.locator('.language-picker').selectOption('en');
  await expect(page.locator('#account-search')).toHaveValue('E2EPlayer');
  await expect(page.locator('[data-account-row]:visible')).toHaveCount(1);
  await page.locator('#account-search').fill('not-an-account');
  await expect(page.locator('#account-empty')).toContainText('No matching accounts');
  await page.locator('#account-search').fill('E2EPlayer');
  await page.locator('[data-account-row]:visible [data-go]').click();
  await expect(page.locator('[name=username]')).toHaveAttribute('readonly', '');
  await expect(page.locator('#avatar-preview img')).toBeVisible();
  await page.locator('[name=display_name]').fill('更新的选手');
  await page.locator('#user-form button[type=submit]').click();
  await expect(page).toHaveURL(/\/accounts$/);
  await page.locator('#account-search').fill('E2EPlayer');
  await expect(page.locator('[data-account-row]:visible .person strong')).toHaveText('更新的选手');
  await page.locator('[data-account-row]:visible [data-go]').click();
  await expect(page.locator('[name=display_name]')).toHaveValue('更新的选手');
  await expect(page.locator('#avatar-preview img')).toBeVisible();
});

test('网站设置子菜单、默认值、分类图片和背景上传', async ({ page }) => {
  await login(page, '/site-settings');
  const submenu = page.locator('.nav-submenu');
  await expect(submenu.getByRole('link')).toHaveText(['常用赛事名', '分类', '额外信息']);
  await page.locator('[name=random_draw_seconds]').fill('6');
  await page.locator('[name=ace_draw_seconds]').fill('7');
  await page.getByLabel('抽签动画').selectOption('gradient');
  await Promise.all([page.waitForResponse('/api/site-settings/tournament-defaults'), page.locator('#tournament-defaults-form button').click()]);
  await page.reload();
  await expect(page.getByLabel('抽签动画')).toHaveValue('gradient');
  await submenu.getByRole('link', { name: '常用赛事名', exact: true }).click();
  await expect(page).toHaveURL(/\/site-settings\/tournament-names$/);
  await expect(page.locator('#name-form')).toBeVisible();
  await expect(page.locator('#category-form, #extra-info-form, #tournament-defaults-form')).toHaveCount(0);
  await page.locator('#name-form [name=name]').fill('端到端常用赛事');
  await Promise.all([page.waitForResponse('/api/site-settings/tournament-names'), page.locator('#name-form button').click()]);
  const nameRow = page.locator('[data-name]').filter({ has: page.locator('input[value="端到端常用赛事"]') });
  await expect(nameRow).toHaveCount(1);
  await nameRow.locator('[name=name]').fill('更新的常用赛事');
  await Promise.all([page.waitForResponse(response => response.url().includes('/tournament-names/') && response.request().method() === 'PATCH'), nameRow.locator('button').first().click()]);
  await expect(page.locator('input[value="更新的常用赛事"]')).toBeVisible();
  await submenu.getByRole('link', { name: '分类', exact: true }).click();
  await expect(page).toHaveURL(/\/site-settings\/categories$/);
  await page.locator('#category-form [name=name]').fill('端到端分类');
  await page.locator('#category-form [name=icon]').setInputFiles(image);
  await Promise.all([page.waitForResponse('/api/site-settings/categories'), page.locator('#category-form button').click()]);
  const category = page.locator('[data-category]').filter({ has: page.locator('input[value="端到端分类"]') });
  await expect(category.locator('img')).toBeVisible();
  expect(await category.locator('img').evaluate(img => img.complete && img.naturalWidth > 0)).toBeTruthy();
  const extraInfoLink = submenu.getByRole('link', { name: '额外信息', exact: true });
  await extraInfoLink.click();
  await expect(page).toHaveURL(/\/site-settings\/extra-infos$/);
  await expect(extraInfoLink).toHaveAttribute('aria-current', 'page');
  await expect(page.getByText('可为每场对局设置额外信息，例如比赛地图。')).toBeVisible();
  await expect(page.locator('#category-form, #name-form, #tournament-defaults-form')).toHaveCount(0);
  await page.locator('#extra-info-form [name=name]').fill('子菜单测试地图');
  await page.locator('#extra-info-form [name=options]').fill('地图甲\n地图乙');
  await page.locator('#extra-info-form button').click();
  await expect(page.locator('input[value="子菜单测试地图"]')).toBeVisible();
  await page.reload();
  await expect(page.getByRole('heading', { name: '额外信息', exact: true })).toBeVisible();
  await expect(page.locator('input[value="子菜单测试地图"]')).toBeVisible();
  await page.goto('/tournaments/new');
  await expect(page.locator('[name=random_draw_seconds]')).toHaveValue('6');
  await expect(page.locator('[name=ace_draw_seconds]')).toHaveValue('7');
  await expect(page.locator('#tournament-names option')).toHaveAttribute('value', '更新的常用赛事');

  await page.goto('/backgrounds');
  await page.locator('#background-form [name=name]').fill('端到端背景');
  await page.locator('#background-form [name=image]').setInputFiles(image);
  await Promise.all([page.waitForResponse('/api/site-settings/backgrounds'), page.locator('#background-form button').click()]);
  const background = page.locator('[data-background]').filter({ has: page.locator('input[value="端到端背景"]') });
  await expect(background.locator('.background-preview')).toBeVisible();
  expect(await background.locator('img').getAttribute('src')).toContain('/thumbnail');
  const backgroundID = Number(await background.getAttribute('data-background'));
  const roster = await players(page.request, '背景选手');
  await createTournament(page.request, '背景绑定赛事', roster, { background_id: backgroundID });
  await page.reload();
  await expect(page.locator(`[data-delete-background="${backgroundID}"]`)).toBeDisabled();
  await page.goto('/site-settings/tournament-names');
  const updatedName = page.locator('[data-name]').filter({ has: page.locator('input[value="更新的常用赛事"]') });
  await updatedName.locator('[data-delete-name]').click();
  await expect(page.locator('#confirm-dialog')).toBeVisible();
  await page.keyboard.press('Enter');
  await expect(page.locator('#confirm-dialog')).toBeVisible();
  await page.locator('[data-confirm-accept]').click();
  await expect(page.locator('input[value="更新的常用赛事"]')).toHaveCount(0);
});

test('搜索添加选手、特殊对战和分组后的规则锁定', async ({ page }) => {
  await login(page, '/tournaments/new');
  const extraInfoResponse = await page.request.post('/api/site-settings/extra-infos', {
    multipart: { name: '创建页地图', options: '地图甲\n地图乙' }
  });
  expect(extraInfoResponse.ok(), await extraInfoResponse.text()).toBeTruthy();
  const extraInfoDefinition = (await json(page.request, 'GET', '/api/site-settings')).extra_infos.find(info => info.name === '创建页地图');
  await expect(page.locator('#event-basics .required-mark')).toHaveCount(3);
  await expect(page.locator('#event-basics [name=name]')).toHaveAttribute('required', '');
  await expect(page.locator('#event-basics [name=start_time]')).toHaveAttribute('required', '');
  await expect(page.locator('#event-basics [name=manager_id]')).toBeHidden();
  await expect(page.locator('#event-basics .search-select input')).toHaveAttribute('required', '');
  await expect(page.locator('#event-basics [name=name]')).toBeVisible();
  await expect(page.locator('.event-phase-heading, .editor-jumps')).toHaveCount(0);
  for (const selector of ['#event-round1 [name=round1_target]', '#event-round2 [name=round2_target]', '#event-ace [name=ace_most_wins]']) {
    await expect(page.locator(selector)).toHaveClass(/compact-number-input/);
    expect((await page.locator(selector).boundingBox()).width).toBeLessThan(90);
  }
  const roster = await players(page.request, '建赛选手');
  // 创建页重新读取目录后，用联想选择和逐人添加完成整份名单。
  await page.reload();
  await page.locator('#event-form [name=name]').fill('端到端创建赛事');
  await expect(page.locator('[data-extra-info-config]')).toHaveCount(0);
  for (const stage of ['round1', 'round2', 'ace']) {
    await expect(page.locator(`#event-${stage} [data-extra-info-select]`)).toBeVisible();
  }
  const round2ExtraInfo = page.locator('#event-round2 [data-extra-info-stage]');
  await round2ExtraInfo.locator('[data-extra-info-select]').selectOption(String(extraInfoDefinition.id));
  await round2ExtraInfo.locator('[data-add-extra-info]').click();
  const round2Map = round2ExtraInfo.locator('[data-extra-info-config]');
  await expect(round2Map).toHaveCount(1);
  await expect(round2Map).toContainText('创建页地图');
  await expect(round2Map.locator('[data-extra-option]:checked')).toHaveCount(0);
  const managerSearch = page.locator('.search-select').filter({ has: page.locator('[name=manager_id]') });
  await managerSearch.locator('input').fill('admin');
  await managerSearch.getByRole('option').first().click();
  for (const [index, user] of roster.entries()) {
    await page.locator('#player-search').fill(user.display_name);
    await page.locator(`[data-suggest-player="${user.id}"]`).click();
    if (index === 1) {
      await expect(page.locator('#add-player')).toBeEnabled();
      await page.getByRole('button', { name: '取消当前选择' }).click();
      await expect(page.locator('#player-search')).toHaveValue('');
      await expect(page.locator('#player-search')).toBeFocused();
      await expect(page.locator('#player-search')).toHaveAttribute('aria-expanded', 'false');
      await expect(page.locator('#add-player')).toBeDisabled();
      await expect(page.locator(`[data-roster-player="${roster[0].id}"]`)).toBeVisible();
      await expect(page.locator('[data-roster-player]')).toHaveCount(1);
      await page.locator('#player-search').fill(user.display_name);
      await page.locator(`[data-suggest-player="${user.id}"]`).click();
    }
    await page.locator('#add-player').click();
    await page.locator(`[name="pick_${user.id}"]`).selectOption(index < 3 ? 'A' : 'B');
  }
  await expect(page.locator('#roster-count')).toHaveText('已入选 6');
  const manualRevival = page.locator('#event-round2 [name=kof_manual_revival]');
  const uniqueRevival = page.locator('#event-round2 [name=kof_revival_no_repeat]');
  const kofTarget = page.locator('#event-round2 [name=round2_target]');
  await expect(manualRevival).not.toBeChecked();
  await kofTarget.fill('4');
  await expect(manualRevival).toBeVisible();
  await expect(uniqueRevival).toBeHidden();
  await kofTarget.fill('5');
  await expect(uniqueRevival).toBeEnabled();
  await expect(uniqueRevival).not.toBeChecked();
  await manualRevival.check();
  await expect(uniqueRevival).toBeChecked();
  await expect(uniqueRevival).toBeDisabled();
  await expect(page.locator('[name=kof_starter][value=random]')).toBeChecked();
  await page.locator('[name=round1_mode]').selectOption('random');
  await page.locator('[name=random_draw_seconds]').fill('0');
  await page.locator('[name=ace_draw_seconds]').fill('0');
  await page.locator('#add-special').click();
  await page.locator('[name=special_a_ids]').selectOption(String(roster[0].id));
  await page.locator('[name=special_b_ids]').selectOption([String(roster[3].id), String(roster[4].id)]);
  await page.locator('[name=special_probability]').fill('30');
  await expect(page.locator('#fallback-probability')).toContainText('30%');
  // 切换语言须保留名单、双方候选、概率和管理员。
  await page.locator('.language-picker').selectOption('en');
  await expect(page.locator('[data-roster-player]')).toHaveCount(6);
  await expect(page.locator('#event-round2 [data-extra-info-config]')).toContainText('创建页地图');
  await expect(page.locator('[name=special_b_ids]')).toHaveValues([String(roster[3].id), String(roster[4].id)]);
  await expect(manualRevival).toBeChecked();
  await expect(uniqueRevival).toBeDisabled();
  await page.locator('.language-picker').selectOption('zh');
  const createdResponse = page.waitForResponse(response => response.url().endsWith('/api/tournaments') && response.request().method() === 'POST');
  await page.locator('#event-form button[type=submit]').click();
  const created = await (await createdResponse).json();
  await expect(page).toHaveURL(new RegExp(`/live/${created.id}$`));
  await expect(page.locator('.board-rosters [data-player-record]')).toHaveCount(6);
  let createdTournament = await json(page.request, 'GET', `/api/public/tournaments/${created.id}`);
  expect(createdTournament.rules.extra_info.round2).toHaveLength(1);
  expect(createdTournament.rules.extra_info.round2[0].name).toBe('创建页地图');
  await page.goto(`/live/${created.id}/setting`);
  await expect(page.locator('[data-roster-player]')).toHaveCount(6);
  await expect(page.locator('[name=special_probability]')).toHaveValue('30');
  await json(page.request, 'POST', `/api/tournaments/${created.id}/setup-draw`, {});
  await page.goto(`/live/${created.id}/setting`);
  await expect(page.locator('select[name=round1_mode]')).toBeDisabled();
  await expect(manualRevival).toBeChecked();
  await expect(page.locator('#event-round2 [data-extra-info-config]')).toHaveCount(1);
  await expect(manualRevival).toBeDisabled();
  await expect(page.locator('[name=ace_most_wins]')).toBeEnabled();
  await expect(page.locator('[data-remove-player]')).toHaveCount(0);
  await page.locator('[name=random_draw_seconds]').fill('2');
  await page.locator('#event-form [data-submit-action="save-and-return"]').click();
  await expect(page).toHaveURL(new RegExp(`/live/${created.id}$`));
  await page.goto(`/live/${created.id}/setting`);
  await expect(page.locator('[name=random_draw_seconds]')).toHaveValue('2');
  await test.step('校验分队设置、直接抽取KOF首发及4对3名单等高', () => verifyFixedSetupDraw(page));
});

test('赛事基本信息仅超管可修改且分组抽签后锁定', async ({ page }) => {
  await login(page);
  const adminSession = (await page.context().cookies()).find(cookie => cookie.name === 'tb_session');
  const backgroundResponse = await page.request.post('/api/site-settings/backgrounds', {
    multipart: { name: '赛事中切换背景', image }
  });
  expect(backgroundResponse.ok(), await backgroundResponse.text()).toBeTruthy();
  const backgrounds = await json(page.request, 'GET', '/api/backgrounds');
  const backgroundID = backgrounds.backgrounds.find(item => item.name === '赛事中切换背景').id;
  const roster = await players(page.request, '基本信息权限选手');
  const { id } = await createTournament(page.request, '基本信息权限赛事', roster, { manager_id: roster[0].id });
  const setting = `/live/${id}/setting`;

  await page.context().clearCookies();
  await login(page, setting, roster[0].username);
  await expect(page.locator('.event-phase-heading, .editor-jumps')).toHaveCount(0);
  await expect(page.locator('#event-basics [name=name]')).toBeDisabled();
  await expect(page.locator('#event-basics [name=name]')).toHaveValue('基本信息权限赛事');
  const managerEdit = await page.request.patch(`/api/tournaments/${id}/settings`, { data: { name: '管理员改名' } });
  expect(managerEdit.status()).toBe(403);

  await page.context().clearCookies();
  await page.context().addCookies([adminSession]);
  await page.goto(setting);
  await expect(page.locator('#event-basics [name=name]')).toBeEnabled();
  await page.locator('#event-basics [name=description]').fill('超管在抽签前更新');
  await page.locator('#event-form [data-submit-action="save-and-return"]').click();
  await expect(page).toHaveURL(new RegExp(`/live/${id}$`));
  expect(await json(page.request, 'GET', `/api/public/tournaments/${id}`)).toMatchObject({ description: '超管在抽签前更新' });

  await page.goto(setting);
  await page.locator('#event-basics [name=description]').fill('返回时不保存');
  await page.locator('#back-board').click();
  await expect(page).toHaveURL(new RegExp(`/live/${id}$`));
  expect(await json(page.request, 'GET', `/api/public/tournaments/${id}`)).toMatchObject({ description: '超管在抽签前更新' });

  await page.goto(setting);
  await page.locator('#event-basics [name=description]').fill('超管保存后留在设置');
  await page.locator('#event-form [data-submit-action="save"]').click();
  await expect(page).toHaveURL(new RegExp(`/live/${id}/setting$`));
  await expect(page.locator('#event-basics [name=description]')).toHaveValue('超管保存后留在设置');
  expect(await json(page.request, 'GET', `/api/public/tournaments/${id}`)).toMatchObject({ description: '超管保存后留在设置' });

  await json(page.request, 'POST', `/api/tournaments/${id}/setup-draw`, {});
  const grouped = await json(page.request, 'GET', `/api/public/tournaments/${id}`);
  await json(page.request, 'POST', `/api/tournaments/${id}/captains/confirm`, {
    team_a_player_id: grouped.teams[0].players[0].id,
    team_b_player_id: grouped.teams[1].players[0].id
  });
  await json(page.request, 'POST', `/api/tournaments/${id}/start`, {});
  await page.goto(setting);
  await expect(page.locator('#event-basics [name=name]')).toBeDisabled();
  const background = page.locator('#event-basics #event-background select');
  await expect(background).toBeEnabled();
  await expect(page.locator('#event-phase-before-ace #event-background')).toHaveCount(0);
  await background.selectOption(String(backgroundID));
  await page.locator('#event-form [data-submit-action="save-and-return"]').click();
  await expect(page).toHaveURL(new RegExp(`/live/${id}$`));
  expect(await json(page.request, 'GET', `/api/public/tournaments/${id}`)).toMatchObject({ background_id: backgroundID });

  const lockedEdit = await page.request.patch(`/api/tournaments/${id}/settings`, { data: { name: '抽签后改名' } });
  expect(lockedEdit.status()).toBe(400);
  expect(await json(page.request, 'GET', `/api/public/tournaments/${id}`)).toMatchObject({ name: '基本信息权限赛事' });
});

test('赛事设置按四大区域分区并要求三次确认后永久删除', async ({ page }) => {
  await login(page);
  const roster = await players(page.request, '永久删除选手');
  const { id } = await createTournament(page.request, '三阶段删除赛事', roster);
  const live = `/live/${id}`;
  await page.goto(live + '/setting');

  for (const selector of ['#event-basics', '#event-phase-before-ace', '#event-phase-ace', '#event-phase-after-ace']) {
    await expect(page.locator(selector)).toBeVisible();
  }
  await expect(page.locator('.event-phase-heading, .editor-jumps')).toHaveCount(0);
  const regionGaps = await page.evaluate(() => {
    const regions = ['#event-basics', '#event-phase-before-ace', '#event-phase-ace', '#event-phase-after-ace']
      .map(selector => document.querySelector(selector).getBoundingClientRect());
    return regions.slice(0, -1).map((region, index) => regions[index + 1].top - region.bottom);
  });
  expect(regionGaps.every(gap => gap >= 32)).toBeTruthy();
  const deleteButton = page.locator('#delete-active-tournament');
  await expect(deleteButton).toBeVisible();
  await expect(page.locator('#event-phase-after-ace #delete-active-tournament')).toBeVisible();
  await expect(page.locator('.event-editor-actions #delete-active-tournament')).toHaveCount(0);
  const footerLabels = await page.locator('.event-editor-actions button').evaluateAll(buttons => buttons.map(button => button.textContent.trim()));
  expect(footerLabels).toEqual(['返回赛事', '保存并返回', '保存']);
  await page.setViewportSize({ width: 375, height: 812 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBeTruthy();
  const footerButtons = await page.locator('.event-editor-actions button:visible').evaluateAll(buttons => buttons.map(button => {
    const rect = button.getBoundingClientRect();
    return rect.left >= 0 && rect.right <= window.innerWidth;
  }));
  expect(footerButtons.every(Boolean)).toBeTruthy();
  await deleteButton.click();
  await expect(page.locator('#confirm-dialog')).toContainText('第 1 次确认');
  await page.locator('[data-confirm-accept]').click();
  await expect(page.locator('#confirm-dialog')).toContainText('第 2 次确认');
  await page.locator('[data-confirm-accept]').click();
  await expect(page.locator('#confirm-dialog')).toContainText('第 3 次确认');
  await page.locator('[data-confirm-cancel]').click();
  await expect(page).toHaveURL(new RegExp(`/live/${id}/setting$`));
  expect((await page.request.get(`/api/public/tournaments/${id}`)).status()).toBe(200);

  await page.locator('#delete-active-tournament').click();
  for (const [index, label] of ['第 1 次确认', '第 2 次确认', '第 3 次确认'].entries()) {
    await expect(page.locator('#confirm-dialog')).toContainText(label);
    const deletion = index === 2
      ? page.waitForResponse(response => response.url().endsWith(`/api/tournaments/${id}/permanent`) && response.request().method() === 'DELETE')
      : null;
    await page.locator('[data-confirm-accept]').click();
    if (deletion) await expect((await deletion).status()).toBe(200);
  }
  await expect(page).toHaveURL(/\/$/);
  expect((await page.request.get(`/api/public/tournaments/${id}`)).status()).toBe(404);
});

async function verifyFixedSetupDraw(page) {
  const roster = await players(page.request, '固定分队', 7);
  const { id } = await createTournament(page.request, '固定分队校验', roster, {
    fixed_assignments: Object.fromEntries(roster.map(user => [user.id, 'A'])),
    rules: { round1_mode: 'manual', kof_starter: 'random', random_draw_seconds: 1 }
  });
  const live = '/live/' + id;
  await page.goto(live + '/setting');
  const draw = page.locator('#start-setup-draw');
  const hint = page.locator('#setup-draw-hint');
  await expect(draw).toBeDisabled();
  await expect(hint).toHaveText('两队都必须有选手');
  for (const player of roster.slice(4)) {
    await page.locator(`[name="pick_${player.id}"]`).selectOption('R');
  }
  const target = page.locator('#event-round2 [name=round2_target]');
  await target.fill('0');
  await expect(draw).toBeDisabled();
  await target.fill('5');
  const probability = page.locator('[name=ace_most_wins]');
  await probability.fill('81');
  await expect(draw).toBeDisabled();
  await expect(hint).toContainText('概率总和');
  await probability.fill('20');
  await expect(draw).toBeEnabled();

  await page.evaluate(() => {
    const observer = new MutationObserver(() => {
      const board = document.querySelector('#board-content[data-setup-drawing]');
      if (!board) return;
      window.setupDrawRoster = ['a', 'b'].map(side =>
        board.querySelectorAll(`.board-roster-${side} [data-roster-player]`).length);
      observer.disconnect();
    });
    observer.observe(document.getElementById('app'), { childList: true, subtree: true });
  });
  const response = page.waitForResponse(res => res.url().endsWith(`/api/tournaments/${id}/setup-draw`) && res.request().method() === 'POST');
  await draw.click();
  expect((await (await response).json()).steps.map(step => step.kind)).toEqual(['round2']);
  await expect(page).toHaveURL(new RegExp(live + '$'));
  const animation = page.locator('#board-content[data-setup-drawing]');
  await expect.poll(() => page.evaluate(() => window.setupDrawRoster)).toEqual([4, 3]);
  await expect(animation).toHaveCount(0);
  await expect(page.locator('.board-round[data-stage=round2] .board-match')).toHaveCount(1);

  for (const [width, height] of [[1920, 1080], [1536, 864], [1366, 768], [1280, 720]]) {
    await page.setViewportSize({ width, height });
    const left = page.locator('.board-roster-a [data-roster-player]');
    const right = page.locator('.board-roster-b [data-roster-player]');
    const rows = list => list.evaluateAll(items => items.map(item => {
      const { y, height } = item.getBoundingClientRect();
      return { y, height };
    }));
    const a = await rows(left), b = await rows(right);
    expect(a).toHaveLength(4);
    expect(b).toHaveLength(3);
    expect(b).toEqual(a.slice(0, 3));
    expect(a[3].height).toBeCloseTo(a[0].height, 1);
    const bList = await page.locator('.board-roster-b .board-roster-list').boundingBox();
    expect(bList.y + bList.height - b[2].y - b[2].height).toBeGreaterThanOrEqual(a[3].height);
  }
}

test('无队长的选手可以报名KOF复活并直接上场', async ({ page, browser }) => {
  await login(page);
  const roster = await players(page.request, '复活');
  const { id } = await createTournament(page.request, '3 打 3 抢 5', roster, {
    round1_target: 1, round2_target: 5,
    rules: { round1_mode: 'random', kof_starter: 'random', no_repeat: true, kof_manual_revival: true, random_draw_seconds: 4 }
  });
  const path = '/api/tournaments/' + id, live = '/live/' + id;
  await json(page.request, 'POST', path + '/setup-draw');
  await json(page.request, 'POST', path + '/start');
  const detail = () => json(page.request, 'GET', '/api/public/tournaments/' + id);
  const finish = (match, next = 0) => json(page.request, 'POST', path + '/matches/' + match.id + '/result', {
    winner_team_id: match.team_a_id, next_player_id: next
  });
  const first = (await detail()).matches.find(match => match.stage === 'round1' && match.status === 'pending');
  await finish(first);
  const starter = (await detail()).matches.find(match => match.stage === 'round2' && match.status === 'pending');
  const returning = roster.find(player => player.id === starter.player_b_id);
  const [second, third] = roster.slice(3).filter(player => player.id !== returning.id);
  for (const next of [second.id, third.id, 0]) {
    const match = (await detail()).matches.find(match => match.stage === 'round2' && match.status === 'pending');
    await finish(match, next);
  }

  await page.goto(live);
  const context = await browser.newContext();
  try {
    const member = await context.newPage();
    await login(member, live, returning.username);
    const row = member.locator(`[data-roster-player="${returning.id}"]`);
    await expect(row).toHaveClass(/is-lost/);
    await row.locator('.roster-action-area').click();
    await member.locator('#roster-actions [data-send-kof]').click();
    const current = '.board-current-fighters .current-player-right';
    await expect(member.locator(current)).toContainText(returning.display_name);
    await expect(row).not.toHaveClass(/is-lost/);
    await expect(page.locator(current)).toContainText(returning.display_name);
    await expect(page.locator('#draw-roll')).toBeHidden();
    await expect(page.locator('[data-current-winner]').first()).toBeEnabled();
  } finally {
    await context.close();
  }
});

test('历史补录紧凑设置并按场录入结果', async ({ page }) => {
  await login(page, '/tournaments/archive');
  const roster = await players(page.request, '历史录入');
  await page.goto('/tournaments/archive');
  await page.locator('[name=name]').fill('历史补录交互验证');
  const managerSearch = page.locator('.search-select').filter({ has: page.locator('[name=manager_id]') });
  await expect(page.locator('#archive-form .required-mark')).toHaveCount(3);
  await expect(page.locator('#archive-form .field [name=manager_id]')).toBeHidden();
  await expect(managerSearch.locator('input[required]')).toBeVisible();
  await managerSearch.locator('input').fill('admin');
  await managerSearch.getByRole('option').first().click();
  await expect(page.locator('.archive-steps li[aria-current]')).toContainText('1');
  await page.locator('[name=round1_target]').fill('1');
  await page.locator('[name=round2_target]').fill('1');
  await expect(page.locator('[name=round1_target]')).toHaveClass(/compact-number-input/);
  await expect(page.locator('[name=round2_target]')).toHaveClass(/compact-number-input/);
  for (const [index, user] of roster.entries()) {
    await page.locator('#player-search').fill(user.display_name);
    await page.locator(`[data-suggest-player="${user.id}"]`).click();
    await page.locator('#add-player').click();
    await page.locator(`[name="pick_${user.id}"]`).selectOption(index < 3 ? 'A' : 'B');
  }
  await page.locator('#archive-form button[type=submit]').click();
  await expect(page.locator('.archive-steps li[aria-current]')).toContainText('2');
  await expect(page.locator('.archive-current-head')).toBeVisible();
  await expect(page.locator('.archive-current')).toBeVisible();
  await expect(page.locator('.archive-contender')).toHaveCount(2);
  await expect(page.locator('#archive-next')).toBeDisabled();
  await page.locator('.archive-contender button[value=a]').click();
  await expect(page.locator('#archive-next')).toBeEnabled();
  await page.locator('#archive-next').click();
  await expect(page.locator('.archive-steps li[aria-current]')).toContainText('3');
  await page.locator('.archive-contender button[value=a]').click();
  await page.locator('#archive-next').click();
  await expect(page.locator('.archive-review-stage')).toHaveCount(2);
  await page.locator('#archive-form button[type=submit]').click();
  await expect(page.locator('#confirm-dialog')).toBeVisible();
  await page.locator('[data-confirm-accept]').click();
  await expect(page).toHaveURL(/\/live\/[1-9]\d*$/);
});

test('直播点击确认和公开SSE更新、历史筛选与软删除恢复', async ({ page, browser }) => {
  await login(page);
  const roster = await players(page.request, '观赛选手');
  const created = await createTournament(page.request, '端到端直播', roster);
  await json(page.request, 'POST', `/api/tournaments/${created.id}/setup-draw`, {});
  const visitor = await browser.newContext();
  try {
    const publicPage = await visitor.newPage();
    const streams = [];
    publicPage.on('request', request => { if (request.url().endsWith('/stream')) streams.push(request.url()); });
    await publicPage.goto(`/live/${created.id}`);
    await expect(publicPage.locator('.broadcast-board')).toBeVisible();
    await expect(publicPage.locator('.sidebar, .topbar, #board-settings, [data-current-winner]')).toHaveCount(0);
    await expect(publicPage.locator('.board-rules')).toHaveAttribute('open', '');
    const tracks = await publicPage.locator('.board-columns').evaluate(element => getComputedStyle(element).gridTemplateColumns.split(' ').map(Number.parseFloat));
    expect(tracks).toHaveLength(3);
    expect(tracks[0] / tracks[1]).toBeCloseTo(1, 1);
    expect(tracks[2] / tracks[0]).toBeCloseTo(1.5, 1);
    const expandedBoardHeight = await publicPage.locator('.board-columns').evaluate(element => element.getBoundingClientRect().height);
    await publicPage.locator('.board-rules > summary').click();
    await expect(publicPage.locator('.board-rules')).not.toHaveAttribute('open');
    await expect.poll(() => publicPage.locator('.board-columns').evaluate(element => element.getBoundingClientRect().height)).toBeGreaterThan(expandedBoardHeight);
    const avatarSize = await publicPage.locator('.board-roster-player .board-avatar').first().evaluate(element => element.getBoundingClientRect().width);
    expect(avatarSize).toBeGreaterThan(30);
    await expect.poll(() => streams.length).toBe(1);
    await page.goto(`/live/${created.id}`);
    await page.locator('[data-current-winner]').first().click();
    await expect(page.locator('#confirm-dialog')).toBeVisible();
    await page.keyboard.press('Enter');
    await expect(page.locator('#confirm-dialog')).toBeVisible();
    await expect(publicPage.locator('[data-stage=round1] h2')).toHaveText('0 : 0');
    await page.locator('[data-confirm-accept]').click();
    await expect(publicPage.locator('[data-stage=round1] h2')).toHaveText('1 : 0');
    await expect(publicPage.locator('.board-rules')).not.toHaveAttribute('open');
    // 访客只收到结果，管理员的抽签动画不应在访客页面播放。
    await expect(publicPage.locator('.board-draw-roll:visible')).toHaveCount(0);

    const matches = [
      ...[0, 1, 2].map(i => ({ round: 1, a_id: roster[i].id, b_id: roster[i + 3].id, who_win: 'a' })),
      ...[0, 1, 2].map(i => ({ round: 2, a_id: roster[i].id, b_id: roster[3].id, who_win: 'b' })),
      { round: 3, a_id: roster[0].id, b_id: roster[3].id, who_win: 'a' }
    ];
    const history = await json(page.request, 'POST', '/api/tournaments/archive', { name: '端到端历史', start_time: '2026-10-01T19', manager_id: 1, player_ids: roster.map(user => user.id), fixed_assignments: assignments(roster), round1_target: 3, round2_target: 3, matches, ace_source: 'most_wins' });
    streams.length = 0;
    await publicPage.goto(`/live/${history.id}`);
    await expect(publicPage.locator('.board-score')).toContainText('2');
    await expect(publicPage.locator('.board-rules')).toContainText('后期录入');
    await publicPage.locator('.board-rosters [data-player-record]').first().click();
    await expect(publicPage.locator('#player-records')).toBeVisible();
    expect(streams).toHaveLength(0);
    await publicPage.goto('/');
    await publicPage.setViewportSize({ width: 390, height: 844 });
    await expect(publicPage.locator('.event-table')).toBeVisible();
    await expect(publicPage.locator('.event-table thead')).toContainText('赛事管理员');
    await expect(publicPage.locator(`[data-tournament-row="${history.id}"] .event-manager`)).toHaveText('超级管理员');
    await expect(publicPage.locator('.event-table tbody tr').first().locator('.event-date, .event-result')).toHaveCount(2);
    expect(await publicPage.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await publicPage.locator('#history-filter [name=name]').fill('端到端历史');
    const playerSelect = publicPage.locator('.search-select').filter({ has: publicPage.locator('[name=player_id]') });
    await playerSelect.locator('input').fill(roster[0].display_name);
    await playerSelect.getByRole('option').first().click();
    await expect(playerSelect.locator('select')).toHaveValue(String(roster[0].id));
    await publicPage.locator('#history-filter button.primary').click();
    await expect(publicPage.locator('.event-table tbody tr')).toHaveCount(1);
    await expect(publicPage.locator('.event-table')).toContainText('端到端历史');

    await page.goto('/tournaments');
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(page.locator('.event-table thead')).toContainText('赛事管理员');
    await expect(page.locator(`[data-tournament-row="${history.id}"] .event-manager`)).toHaveText('超级管理员');
    await expect(page.locator(`[data-tournament-row="${created.id}"] .event-status`)).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await expect(page.locator(`[data-tournament-row="${created.id}"] [data-tournament-action]`)).toHaveCount(0);
    await page.locator(`[data-tournament-id="${history.id}"]`).click();
    await page.locator('[data-confirm-accept]').click();
    await expect(page.locator(`[data-tournament-id="${history.id}"]`)).toHaveAttribute('data-tournament-action', 'restore');
    await publicPage.reload();
    await expect(publicPage.locator('.event-table')).toHaveCount(0);
    await publicPage.locator('#include-deleted').check();
    await expect(publicPage.locator('.event-table')).toContainText('端到端历史');
    await page.locator(`[data-tournament-id="${history.id}"]`).click();
    await expect(page.locator(`[data-tournament-id="${history.id}"]`)).toHaveAttribute('data-tournament-action', 'delete');
    await publicPage.locator('#include-deleted').uncheck();
    await expect(publicPage.locator('.event-table')).toContainText('端到端历史');
  } finally { await visitor.close(); }
});

test('纠错确认清除范围、验证批量合法性并允许修正最后一局选手', async ({ page }) => {
  await login(page);
  const roster = await players(page.request, '纠错选手');
  const created = await createTournament(page.request, '纠错赛程校验', roster);
  await json(page.request, 'POST', `/api/tournaments/${created.id}/setup-draw`, {});
  await json(page.request, 'POST', `/api/tournaments/${created.id}/start`, {});
  await page.goto(`/live/${created.id}`);
  const detailPath = `/api/public/tournaments/${created.id}`;
  let detail = await json(page.request, 'GET', detailPath);
  const teamA = detail.teams[0].id;
  const teamB = detail.teams[1].id;
  for (let score = 0; score < 3; score++) {
    await page.locator(`[data-current-winner="${teamA}"]`).click();
    await page.locator('[data-confirm-accept]').click();
  }
  await expect(page.locator('[data-stage="round1"] h2')).toHaveText('3 : 0');
  const kofID = (await json(page.request, 'GET', detailPath)).matches.find(match => match.stage === 'round2').id;
  await page.locator(`[data-current-winner="${teamA}"]`).click();
  await page.locator('[data-confirm-accept]').click();
  await page.goto(`/live/${created.id}/setting`);
  detail = await json(page.request, 'GET', detailPath);
  const firstRoundID = detail.matches.find(match => match.stage === 'round1' && match.seq === 1).id;
  await page.locator(`[data-correct-match="${firstRoundID}"]`).click();
  await page.locator(`[data-correct-match="${kofID}"]`).click();
  await expect(page.locator('[data-correction-scope]')).toContainText('整批修改会被拒绝');
  await page.locator('#event-form [data-submit-action="save-and-return"]').click();
  await expect(page.locator('#confirm-dialog')).toBeVisible();
  await page.locator('[data-confirm-accept]').click();
  await expect(page.locator('.form-error')).toContainText('第一轮尚未达到目标胜场');
  await expect.poll(async () => (await json(page.request, 'GET', detailPath)).matches.filter(match => match.status === 'finished').length).toBe(4);

  await page.locator(`[data-correct-match="${kofID}"]`).click();
  await page.locator('#event-form [data-submit-action="save-and-return"]').click();
  await page.locator('[data-confirm-accept]').click();
  await expect(page.locator('#confirm-dialog')).toContainText('全部');
  await page.locator('[data-confirm-cancel]').click();
  await expect.poll(async () => (await json(page.request, 'GET', detailPath)).matches.filter(match => match.status === 'finished').length).toBe(4);
  await page.locator(`[data-correct-match="${firstRoundID}"]`).click();

  detail = await json(page.request, 'GET', detailPath);
  const last = detail.matches.find(match => match.id === kofID);
  const replacement = detail.teams[1].players.find(player => player.id !== last.player_b_id);
  await page.locator('[name="correction_player_b_id"]').selectOption(String(replacement.id));
  await expect(page.locator('[name="correction_player_b_id"]')).toHaveValue(String(replacement.id));
  await page.locator('#event-form [data-submit-action="save-and-return"]').click();
  await expect(page.locator('#confirm-dialog')).toBeVisible();
  await expect(page.locator('#confirm-dialog')).toContainText(replacement.display_name);
  const correctionRequest = page.waitForRequest(request => request.method() === 'PATCH' && request.url().endsWith(`/api/tournaments/${created.id}/settings`));
  const correctionResponse = page.waitForResponse(response => response.request().method() === 'PATCH' && response.url().endsWith(`/api/tournaments/${created.id}/settings`));
  await page.locator('[data-confirm-accept]').click();
  const correctionPayload = await (await correctionRequest).postDataJSON();
  expect(correctionPayload.corrections[0].player_b_id).toBe(replacement.id);
  const savedCorrection = await correctionResponse;
  expect(savedCorrection.status(), await savedCorrection.text()).toBe(200);
  detail = await json(page.request, 'GET', detailPath);
  expect(detail.matches.find(match => match.id === kofID).player_b_id).toBe(replacement.id);
  expect(detail.matches.find(match => match.id === kofID).winner_team_id).toBe(teamA);
  expect(teamB).not.toBe(teamA);
});

test('观众实时接收大将抽签结果但不播放悬念动画', async ({ page, browser }) => {
  await login(page);
  await json(page.request, 'PATCH', '/api/site-settings/tournament-defaults', { draw_animation: 'gradient' });
  const roster = await players(page.request, '抽签观众');
  const created = await createTournament(page.request, '观众抽签动画', roster, {
    round1_target: 1,
    round2_target: 1,
    rules: { round1_mode: 'random', kof_starter: 'random', no_repeat: true, kof_revival_no_repeat: false, random_draw_seconds: 0, ace_draw_seconds: 3, ace_most_wins: 20, ace_most_losses: 20, ace_specials: [] }
  });
  await json(page.request, 'POST', `/api/tournaments/${created.id}/setup-draw`, {});
  await json(page.request, 'POST', `/api/tournaments/${created.id}/start`, {});
  let detail = await json(page.request, 'GET', `/api/public/tournaments/${created.id}`);
  const [teamA, teamB] = detail.teams.map(team => team.id);
  let match = detail.matches.find(item => item.stage === 'round1');
  await json(page.request, 'POST', `/api/tournaments/${created.id}/matches/${match.id}/result`, { winner_team_id: teamA });
  detail = await json(page.request, 'GET', `/api/public/tournaments/${created.id}`);
  match = detail.matches.find(item => item.stage === 'round2');
  await json(page.request, 'POST', `/api/tournaments/${created.id}/matches/${match.id}/result`, { winner_team_id: teamB });

  const visitor = await browser.newContext();
  try {
    const publicPage = await visitor.newPage();
    await publicPage.goto(`/live/${created.id}`);
    await expect(publicPage.locator('#draw-ace')).toHaveCount(0);
    await page.goto(`/live/${created.id}`);
    await expect(page.locator('#draw-ace')).toBeVisible();
    await expect(page.locator('.broadcast-board')).toHaveAttribute('data-draw-animation', 'gradient');
    await page.locator('#draw-ace').click();
    await expect(page.locator('#draw-roll')).toBeVisible();
    await expect(page.locator('.draw-rule-slot')).toBeVisible();
    await expect(publicPage.locator('[data-stage="ace"] .board-matches .board-match')).toHaveCount(1);
    await expect(publicPage.locator('#draw-roll:visible')).toHaveCount(0);
    await expect(publicPage.locator('#draw-ace')).toHaveCount(0);
  } finally {
    await visitor.close();
  }
});

test('首轮排阵公开后队长和管理员均无法重排', async ({ page }) => {
  await login(page);
  const roster = await players(page.request, '队长排阵锁定');
  const created = await createTournament(page.request, '队长排阵锁定', roster, {
    rules: { round1_mode: 'manual', kof_starter: 'random', no_repeat: true, kof_revival_no_repeat: false, random_draw_seconds: 0, ace_draw_seconds: 0, ace_most_wins: 20, ace_most_losses: 20, ace_specials: [] }
  });
  await json(page.request, 'POST', `/api/tournaments/${created.id}/setup-draw`, {});
  let detail = await json(page.request, 'GET', `/api/public/tournaments/${created.id}`);
  const teamA = detail.teams[0], teamB = detail.teams[1];
  await json(page.request, 'POST', `/api/tournaments/${created.id}/captains/confirm`, {
    team_a_player_id: teamA.players[0].id,
    team_b_player_id: teamB.players[0].id
  });
  await page.goto(`/live/${created.id}`);
  await expect(page.locator('#board-lineup')).toBeVisible();
  await page.goto(`/live/${created.id}/lineup`);
  await expect(page.locator('#round1-lineup-form')).toBeVisible();

  const lineupURL = `/api/tournaments/${created.id}/lineup/submit`;
  const firstA = teamA.players.slice(0, 2).map(player => player.id);
  const reorderedA = [firstA[1], firstA[0]];
  await json(page.request, 'POST', lineupURL, { team_id: teamA.id, player_ids: firstA });
  await page.goto(`/live/${created.id}/lineup`);
  await expect(page.locator('#round1-lineup-form')).toBeVisible();
  await json(page.request, 'POST', lineupURL, { team_id: teamA.id, player_ids: reorderedA });
  await json(page.request, 'POST', lineupURL, { team_id: teamB.id, player_ids: teamB.players.slice(0, 2).map(player => player.id) });

  await page.goto(`/live/${created.id}`);
  await expect(page.locator('#board-lineup')).toHaveCount(0);
  await page.goto(`/live/${created.id}/lineup`);
  await expect(page).toHaveURL(new RegExp(`/live/${created.id}$`));
  for (const [team, playerIDs] of [[teamA, firstA], [teamB, teamB.players.slice(0, 2).map(player => player.id)]]) {
    const response = await page.request.post(lineupURL, { data: { team_id: team.id, player_ids: playerIDs } });
    expect(response.status()).toBe(400);
  }
  const detailAfter = await json(page.request, 'GET', `/api/public/tournaments/${created.id}`);
  const savedA = detailAfter.lineups.filter(lineup => lineup.team_id === teamA.id).map(lineup => lineup.player_id);
  expect(savedA.slice(0, 2)).toEqual(reorderedA);
});
