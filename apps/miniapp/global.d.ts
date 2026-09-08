/// <reference types="@tarojs/taro" />

declare module '*.scss'

// Taro 编译宏（类型声明，编译期被 CLI 内联替换）
declare function defineAppConfig(config: Record<string, unknown>): Record<string, unknown>
declare function definePageConfig(config: Record<string, unknown>): Record<string, unknown>

declare const API_BASE_URL: string
