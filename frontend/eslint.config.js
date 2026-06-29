import js from '@eslint/js';
import globals from 'globals';
import reactHooks from 'eslint-plugin-react-hooks';
import reactRefresh from 'eslint-plugin-react-refresh';
import tseslint from 'typescript-eslint';
import eslintConfigPrettier from 'eslint-config-prettier';
import { defineConfig, globalIgnores } from 'eslint/config';

export default defineConfig([
  globalIgnores(['dist']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommended,
      reactHooks.configs.flat.recommended,
      reactRefresh.configs.vite,
    ],
    languageOptions: {
      globals: globals.browser,
    },
    rules: {
      // 允许下划线前缀表示“有意未使用”的参数 / 变量 / 捕获错误（mock 签名占位、stub 形参等）
      '@typescript-eslint/no-unused-vars': [
        'error',
        {
          argsIgnorePattern: '^_',
          varsIgnorePattern: '^_',
          caughtErrorsIgnorePattern: '^_',
        },
      ],
      // react-hooks v7 新增的实验性规则，会误报本项目中“将外部数据同步进 state”的合法 effect
      // （数据拉取、随搜索变化重置页码等，正是 React 文档认可的 effect 用途）。
      // 迁移到 react-query 等数据层方案超出当前范围，集中关闭此规则；
      // 依赖项正确性仍由 react-hooks/exhaustive-deps 把关。
      'react-hooks/set-state-in-effect': 'off',
    },
  },
  {
    // 测试引导文件需动态向 globalThis 注入 JSDOM 全局，依赖 @ts-nocheck 屏蔽类型噪声
    files: ['src/test/**/*.ts'],
    rules: {
      '@typescript-eslint/ban-ts-comment': 'off',
    },
  },
  // eslint-config-prettier 置于数组最后：关闭 ESLint 中与 Prettier 冲突的格式类规则，
  // 由 Prettier 统一负责代码格式，避免二者就缩进 / 引号 / 行宽等互相打架。
  eslintConfigPrettier,
]);
