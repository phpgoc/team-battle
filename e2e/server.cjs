const { spawn, spawnSync } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');

const root = path.resolve(__dirname, '..');
const output = path.join(root, 'runtime', 'e2e');
fs.mkdirSync(output, { recursive: true });
// 每次运行新建空库，由程序按正式启动流程初始化，不复制或改写演示库。
const run = fs.mkdtempSync(path.join(output, 'run-'));
const binary = path.join(run, process.platform === 'win32' ? 'team-battle.exe' : 'team-battle');
const build = spawnSync(process.env.GO_BINARY || 'go', ['build', '-o', binary, '.'], {
  cwd: root,
  env: { ...process.env, CGO_ENABLED: '0' },
  stdio: 'inherit',
  windowsHide: true
});
if (build.error) throw build.error;
if (build.status !== 0) process.exit(build.status || 1);

console.log(`端到端测试数据库：${path.join(run, 'team-battle.sqlite')}`);
const server = spawn(binary, ['--db', path.join(run, 'team-battle.sqlite'), '--host', '127.0.0.1', '--port', process.env.E2E_PORT || '19577'], {
  cwd: root,
  stdio: 'inherit',
  windowsHide: true
});
server.on('error', error => { console.error(error); process.exitCode = 1; });
server.on('exit', code => { process.exitCode = code || 0; });
for (const signal of ['SIGINT', 'SIGTERM']) process.on(signal, () => server.kill());
