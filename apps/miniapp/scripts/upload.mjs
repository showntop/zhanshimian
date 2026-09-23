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
  // Taro(Vite) 产物已是转译后的 CJS；但 esbuild 无法下转 generator，
  // 产物里会残留 function*（见 dist/vendors.js）。微信侧任何二次编译
  // （es6 / enhance 增强编译）都会把它转成 regenerator 并注入
  // require('@babel/runtime/helpers/regeneratorValues')，而项目未配 npm
  // 构建、包内无 @babel/runtime 文件 → 运行时 "module not defined"。
  // 因此 es6 与 enhance 必须同时显式关掉，防止继承 project.config.json。
  setting: { es6: false, enhance: false, minify: true, minifyWXSS: true, minifyWXML: true },
  onProgressUpdate: (info) => console.log('[upload]', info),
});
console.log('[upload] ✅ 成功:', version, JSON.stringify(result.subPackageInfo || ''));
