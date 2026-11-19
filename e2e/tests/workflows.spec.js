const { test, expect, login, json, players, assignments, createTournament, image } = require('./helpers.cjs');

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

test('网站默认值、常用名称、分类图片和背景上传', async ({ page }) => {
  await login(page, '/site-settings');
  await page.locator('[name=random_draw_seconds]').fill('6');
  await page.locator('[name=ace_draw_seconds]').fill('7');
  await Promise.all([page.waitForResponse('/api/site-settings/tournament-defaults'), page.locator('#tournament-defaults-form button').click()]);
  await page.locator('#name-form [name=name]').fill('端到端常用赛事');
  await Promise.all([page.waitForResponse('/api/site-settings/tournament-names'), page.locator('#name-form button').click()]);
  const nameRow = page.locator('[data-name]').filter({ has: page.locator('input[value="端到端常用赛事"]') });
  await expect(nameRow).toHaveCount(1);
  await nameRow.locator('[name=name]').fill('更新的常用赛事');
  await Promise.all([page.waitForResponse(response => response.url().includes('/tournament-names/') && response.request().method() === 'PATCH'), nameRow.locator('button').first().click()]);
  await page.locator('#category-form [name=name]').fill('端到端分类');
  await page.locator('#category-form [name=icon]').setInputFiles(image);
  await Promise.all([page.waitForResponse('/api/site-settings/categories'), page.locator('#category-form button').click()]);
  const category = page.locator('[data-category]').filter({ has: page.locator('input[value="端到端分类"]') });
  await expect(category.locator('img')).toBeVisible();
  expect(await category.locator('img').evaluate(img => img.complete && img.naturalWidth > 0)).toBeTruthy();
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
  await page.goto('/site-settings');
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
  const roster = await players(page.request, '建赛选手');
  // 创建页重新读取目录后，用联想选择和逐人添加完成整份名单。
  await page.reload();
  await page.locator('#event-form [name=name]').fill('端到端创建赛事');
  const managerSearch = page.locator('.search-select').filter({ has: page.locator('[name=manager_id]') });
  await managerSearch.locator('input').fill('admin');
  await managerSearch.getByRole('option').first().click();
  for (const [index, user] of roster.entries()) {
    await page.locator('#player-search').fill(user.display_name);
    await page.locator(`[data-suggest-player="${user.id}"]`).click();
    await page.locator('#add-player').click();
    await page.locator(`[name="pick_${user.id}"]`).selectOption(index < 3 ? 'A' : 'B');
  }
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
  await expect(page.locator('[name=special_b_ids]')).toHaveValues([String(roster[3].id), String(roster[4].id)]);
  await page.locator('.language-picker').selectOption('zh');
  const createdResponse = page.waitForResponse(response => response.url().endsWith('/api/tournaments') && response.request().method() === 'POST');
  await page.locator('#event-form button[type=submit]').click();
  const created = await (await createdResponse).json();
  await expect(page).toHaveURL(new RegExp(`/live/${created.id}$`));
  await expect(page.locator('.board-rosters [data-player-record]')).toHaveCount(6);
  await page.goto(`/live/${created.id}/setting`);
  await expect(page.locator('[data-roster-player]')).toHaveCount(6);
  await expect(page.locator('[name=special_probability]')).toHaveValue('30');
  await json(page.request, 'POST', `/api/tournaments/${created.id}/setup-draw`, {});
  await page.goto(`/live/${created.id}/setting`);
  await expect(page.locator('select[name=round1_mode]')).toBeDisabled();
  await expect(page.locator('[name=ace_most_wins]')).toBeEnabled();
  await expect(page.locator('[data-remove-player]')).toHaveCount(0);
  await page.locator('[name=random_draw_seconds]').fill('2');
  await page.locator('#event-form button[type=submit]').click();
  await expect(page).toHaveURL(new RegExp(`/live/${created.id}$`));
  await page.goto(`/live/${created.id}/setting`);
  await expect(page.locator('[name=random_draw_seconds]')).toHaveValue('2');
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
    await expect.poll(() => streams.length).toBe(1);
    await page.goto(`/live/${created.id}`);
    await page.locator('[data-current-winner]').first().click();
    await expect(page.locator('#confirm-dialog')).toBeVisible();
    await page.keyboard.press('Enter');
    await expect(page.locator('#confirm-dialog')).toBeVisible();
    await expect(publicPage.locator('[data-stage=round1] h2')).toHaveText('0 : 0');
    await page.locator('[data-confirm-accept]').click();
    await expect(publicPage.locator('[data-stage=round1] h2')).toHaveText('1 : 0');
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
    await publicPage.locator('#history-filter [name=name]').fill('端到端历史');
    const playerSelect = publicPage.locator('.search-select').filter({ has: publicPage.locator('[name=player_id]') });
    await playerSelect.locator('input').fill(roster[0].display_name);
    // 无分类后缀时精确姓名会直接选中账号，菜单也会随之关闭。
    await expect(playerSelect.locator('select')).toHaveValue(String(roster[0].id));
    await publicPage.locator('#history-filter button.primary').click();
    await expect(publicPage.locator('.event-table tbody tr')).toHaveCount(1);
    await expect(publicPage.locator('.event-table')).toContainText('端到端历史');

    await page.goto('/tournaments');
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
