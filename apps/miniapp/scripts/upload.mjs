// 上传小程序到微信后台（上传后仍需管理员在 mp 后台「版本管理」设为体验版）
// 用法: node scripts/upload.mjs [版本号] [备注]
const ci = (await import('miniprogram-ci')).default;
const { readFileSync } = await import('node:fs');
const { fileURLToPath } = await import('node:url');
const path = await import('node:path');

const root = path.dirname(fileURLToPath(import.meta.url)) + '/..';
const pkg = JSON.parse(readFileSync(path.join(root, 'package.json'), 'utf8'));
const version = process.argv[2] || pkg.version;
const desc = process.argv[3] || `CI 上传 ${new Date().toISOString()}`;

const project = new ci.Project({
  appid: 'wx911e0fbcba0b24d0',
  type: 'miniProgram',
  projectPath: root,
  privateKeyPath: path.join(root, 'private.wx911e0fbcba0b24d0.key'),
  ignores: ['node_modules/**/*'],
});

const result = await ci.upload({
  project,
  version,
  desc,
  setting: { es6: true, minify: true, minifyWXSS: true, minifyWXML: true },
  onProgressUpdate: (info) => console.log('[upload]', info),
});
console.log('[upload] ✅ 成功:', version, JSON.stringify(result.subPackageInfo || ''));
