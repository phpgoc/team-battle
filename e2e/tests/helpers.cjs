const { test: base, expect } = require('@playwright/test');

// 用例结束时检查运行时异常，避免界面看似存在但交互脚本已经失败。
const test = base.extend({
  checkPageErrors: [async ({ page }, use) => {
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await use();
    expect(errors, '浏览器不应出现未处理异常').toEqual([]);
  }, { auto: true }]
});

async function login(page, target = '/accounts', username = 'admin', password = 'test1234') {
  await page.goto('/login?return=' + encodeURIComponent(target));
  await page.locator('[name=username]').fill(username);
  await page.locator('[name=password]').fill(password);
  await page.locator('#login-form button.primary').click();
  await expect(page).toHaveURL(new RegExp(target.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '$'));
  await expect(page.locator('h1').first()).toBeVisible();
}

async function json(request, method, url, data) {
  const response = await request.fetch(url, { method, data });
  expect(response.ok(), `${method} ${url}: ${await response.text()}`).toBeTruthy();
  return response.json();
}

let sequence = 0;
async function players(request, label, count = 6) {
  const { categories } = await json(request, 'GET', '/api/site-settings');
  let categoryID = categories[0]?.id;
  if (!categoryID) {
    const response = await request.post('/api/site-settings/categories', { multipart: { name: '赛事测试分类', icon: image } });
    expect(response.ok(), await response.text()).toBeTruthy();
    const settings = await json(request, 'GET', '/api/site-settings');
    categoryID = settings.categories.find(category => category.name === '赛事测试分类').id;
  }
  const users = [];
  for (let i = 0; i < count; i++) {
    const username = `${label}_${++sequence}`;
    const result = await json(request, 'POST', '/api/users', { username, display_name: username, password: 'test1234', role: 'user', category_id: categoryID });
    users.push({ id: result.id, username, display_name: username });
  }
  return users;
}

function assignments(users) {
  return Object.fromEntries(users.map((user, index) => [user.id, index < 3 ? 'A' : 'B']));
}

async function createTournament(request, name, users, extra = {}) {
  const result = await json(request, 'POST', '/api/tournaments', {
    name, start_time: '2026-11-19T19', manager_id: 1,
    player_ids: users.map(user => user.id), fixed_assignments: assignments(users),
    round1_target: 3, round2_target: 3,
    rules: { round1_mode: 'random', kof_starter: 'random', no_repeat: true, random_draw_seconds: 0, ace_draw_seconds: 0, ace_most_wins: 20, ace_most_losses: 20, ace_specials: [] },
    ...extra
  });
  return result;
}

// 小图片只用于验证文件上传，测试不依赖远程图片服务。
const image = { name: 'icon.png', mimeType: 'image/png', buffer: require('node:fs').readFileSync(require('node:path').join(__dirname, '../../internal/app/testdata/demo/terran.png')) };

module.exports = { test, expect, login, json, players, assignments, createTournament, image };
