const { getDefaultConfig } = require('expo/metro-config')
const path = require('node:path')

const projectRoot = __dirname
const monorepoRoot = path.resolve(projectRoot, '../..')

const config = getDefaultConfig(projectRoot)

config.watchFolders = [monorepoRoot]
config.resolver.nodeModulesPaths = [
  path.resolve(projectRoot, 'node_modules'),
  path.resolve(monorepoRoot, 'node_modules'),
]
config.resolver.unstable_enableSymlinks = true
config.resolver.extraNodeModules = {
  '@zsm/core': path.resolve(monorepoRoot, 'packages/core'),
  '@zsm/design': path.resolve(monorepoRoot, 'packages/design'),
}

module.exports = config
