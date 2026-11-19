const { defineConfig } = require('@playwright/test');

const port = Number(process.env.E2E_PORT || 19577);
if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error('E2E_PORT 必须是有效端口');
const baseURL = `http://127.0.0.1:${port}`;

module.exports = defineConfig({
  testDir: './tests',
  fullyParallel: false,
  // 登录会替换已有会话，使用一个工作进程保持用例相互独立。
  workers: 1,
  forbidOnly: !!process.env.CI,
  timeout: 45000,
  expect: { timeout: 10000 },
  reporter: [['list'], ['html', { outputFolder: '../runtime/e2e/report', open: 'never' }]],
  outputDir: '../runtime/e2e/results',
  use: {
    baseURL,
    viewport: { width: 1536, height: 864 },
    locale: 'zh-CN',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure'
  },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }],
  webServer: {
    command: 'node server.cjs',
    cwd: __dirname,
    url: baseURL + '/login',
    env: { E2E_PORT: String(port) },
    timeout: 120000,
    reuseExistingServer: false
  }
});
