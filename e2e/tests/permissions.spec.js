const { test, expect, login, json, players, createTournament } = require('./helpers.cjs');

const adminRoutes = [
  '/tournaments', '/tournaments/new', '/tournaments/archive',
  '/accounts', '/accounts/new', '/accounts/1', '/site-settings', '/backgrounds'
];
const privateForms = '#event-form, #user-form, #account-form, #account-search, #name-form, #category-form, #background-form, #round1-lineup-form';

async function preparePermissions(browser, label) {
  // 数据准备使用独立会话；接受权限检查的访客页面从未登录过。
  const admin = await browser.newContext();
  try {
    await json(admin.request, 'POST', '/api/auth/login', { username: 'admin', password: 'test1234' });
    const roster = await players(admin.request, label);
    const tournament = await createTournament(admin.request, '页面权限赛事', roster, {
      manager_id: roster[0].id,
      rules: { round1_mode: 'manual', kof_starter: 'captain', random_draw_seconds: 0, ace_draw_seconds: 0 }
    });
    await json(admin.request, 'POST', `/api/tournaments/${tournament.id}/setup-draw`, {});
    await json(admin.request, 'POST', `/api/tournaments/${tournament.id}/captains/confirm`, {
      team_a_player_id: roster[2].id, team_b_player_id: roster[3].id
    });
    const other = await createTournament(admin.request, '其他管理员的赛事', roster, {
      rules: { round1_mode: 'manual', kof_starter: 'captain', random_draw_seconds: 0, ace_draw_seconds: 0 }
    });
    await json(admin.request, 'POST', `/api/tournaments/${other.id}/setup-draw`, {});
    return { roster, id: tournament.id, otherID: other.id };
  } finally { await admin.close(); }
}

async function expectForbidden(page, route) {
  const response = await page.goto(route);
  expect(response.status(), route).toBe(403);
  await expect(page).toHaveURL(new URL(route, page.url()).href);
  await expect(page.locator(privateForms)).toHaveCount(0);
  // 拦截必须发生在返回后台HTML之前，不能只隐藏表单。
  await expect(page.locator('script[src="/assets/app.js"], .sidebar')).toHaveCount(0);
}

async function expectLoginRedirect(page, route, target = route) {
  const response = await page.goto(route);
  await expect(page).toHaveURL(url => url.pathname === '/login' && url.searchParams.get('return') === target);
  // 验证服务端已经重定向，不能先返回后台HTML再靠前端跳走。
  const redirected = response.request().redirectedFrom();
  expect(redirected, route).not.toBeNull();
  expect(new URL(redirected.url()).pathname).toBe(new URL(target, page.url()).pathname);
  expect((await redirected.response()).status()).toBe(302);
  await expect(page.locator('#login-form')).toBeVisible();
  await expect(page.locator(privateForms)).toHaveCount(0);
}

test('未登录逐个访问后台路由均被拦截', async ({ page, browser }) => {
  const { id } = await preparePermissions(browser, '访客权限选手');
  for (const route of [...adminRoutes, '/account', '/accounts?search=admin']) {
    await test.step('访客访问 ' + route, async () => {
      await expectLoginRedirect(page, route);
    });
  }
  for (const route of [`/live/${id}/setting`, `/live/${id}/lineup`]) {
    await test.step('访客访问 ' + route, () => expectForbidden(page, route));
  }
  // 旧书签经过重定向后也不能绕过权限。
  for (const [bookmark, target] of [
    ['/workspace?page=accounts&edit=1', '/accounts/1'],
    ['/workspace?page=siteSettings', '/site-settings'],
    ['/workspace?page=createEvent', '/tournaments/new']
  ]) {
    await test.step('访客旧书签 ' + bookmark, async () => {
      await expectLoginRedirect(page, bookmark, target);
    });
  }
  await page.goto(`/live/${id}`);
  await expect(page.locator('.broadcast-board')).toBeVisible();
  await expect(page.locator('#board-settings, #board-lineup, [data-current-winner]')).toHaveCount(0);
  await page.goto('/');
  await expect(page.locator('#history-filter')).toBeVisible();
  await expect(page.locator('.sidebar')).toHaveCount(0);
});

test('选手和队长不能越权，赛事管理员仅能管理本赛事', async ({ page, browser }) => {
  const { roster, id, otherID } = await preparePermissions(browser, '身份权限选手');
  for (const identity of [
    { label: '普通选手', user: roster[1], settings: false, lineup: false },
    { label: '队长', user: roster[2], settings: false, lineup: true },
    { label: '赛事管理员', user: roster[0], settings: true, lineup: true }
  ]) {
    await test.step(identity.label + '的后台入口权限', async () => {
      await login(page, '/', identity.user.username);
      await expect(page.locator('.sidebar .nav a')).toHaveCount(2);
      for (const route of adminRoutes) await expectForbidden(page, route);
      await page.goto('/account');
      await expect(page.locator('#account-form')).toBeVisible();
    });
    await test.step(identity.label + '的赛事入口权限', async () => {
      const setting = `/live/${id}/setting`, lineup = `/live/${id}/lineup`;
      if (identity.settings) {
        await page.goto(setting);
        await expect(page.locator('#event-form')).toBeVisible();
      } else await expectForbidden(page, setting);
      if (identity.lineup) {
        await page.goto(lineup);
        await expect(page.locator('#round1-lineup-form')).toBeVisible();
      } else await expectForbidden(page, lineup);
      // 赛事管理和队长身份均绑定赛事，不能继承到另一个赛事。
      await expectForbidden(page, `/live/${otherID}/setting`);
      await expectForbidden(page, `/live/${otherID}/lineup`);
    });
  }
  await test.step('超管可以打开所有后台入口', async () => {
    await login(page);
    for (const route of adminRoutes) {
      const response = await page.goto(route);
      expect(response.status(), route).toBe(200);
      await expect(page.locator('h1').first()).toBeVisible();
    }
    await page.goto(`/live/${id}/setting`);
    await expect(page.locator('#event-form')).toBeVisible();
    await page.goto(`/live/${id}/lineup`);
    await expect(page.locator('#round1-lineup-form')).toBeVisible();
    await page.goto(`/live/${otherID}/setting`);
    await expect(page.locator('#event-form')).toBeVisible();
  });
});
