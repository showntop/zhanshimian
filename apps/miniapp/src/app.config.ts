export default defineAppConfig({
  window: {
    navigationStyle: 'custom',
    backgroundColor: '#F8F5F0',
    backgroundTextStyle: 'dark',
    navigationBarTextStyle: 'black',
  },
  // 主包 11 页：tab 3 + 主闭环（建档→分析→报告→方案→清单→反馈）+ 场景 Brief。
  // tab 页必须主包；主闭环是产品唯一不可延迟路径，首跑零分包下载等待。
  pages: [
    'pages/home/index',
    'pages/plans/index',
    'pages/profile/index',
    'pages/scene/index',
    'pages/capture/index',
    'pages/analysis/index',
    'pages/report/index',
    'pages/plan/index',
    'pages/checklist/index',
    'pages/feedback/index',
  ],
  // 分包：低频探需工具 + 复访闭环（preloadRule 兜底首屏后预拉）。
  subPackages: [
    {
      root: 'packages/tools',
      pages: [
        'pages/hair/index',
        'pages/outfit/index',
        'pages/purchase/index',
        'pages/lab/index',
      ],
    },
    {
      root: 'packages/life',
      pages: [
        'pages/today/index',
        'pages/wardrobe/index',
        'pages/advisor/index',
        'pages/share/index',
      ],
    },
  ],
  preloadRule: {
    'pages/home/index': { network: 'all', packages: ['packages/tools'] },
    'pages/plans/index': { network: 'wifi', packages: ['packages/life'] },
  },
  tabBar: {
    color: '#656B64',
    selectedColor: '#587344',
    backgroundColor: '#F8F5F0',
    borderStyle: 'black',
    list: [
      { pagePath: 'pages/home/index', text: '首页', iconPath: 'assets/tabbar/home.png', selectedIconPath: 'assets/tabbar/home-active.png' },
      { pagePath: 'pages/plans/index', text: '方案', iconPath: 'assets/tabbar/plans.png', selectedIconPath: 'assets/tabbar/plans-active.png' },
      { pagePath: 'pages/profile/index', text: '我的', iconPath: 'assets/tabbar/user.png', selectedIconPath: 'assets/tabbar/user-active.png' },
    ],
  },
  lazyCodeLoading: 'requiredComponents',
})
