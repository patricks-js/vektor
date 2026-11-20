import js from '@eslint/js'
import tseslint from 'typescript-eslint'
import hooks from 'eslint-plugin-react-hooks'

export default tseslint.config(
  { ignores: ['dist', 'playwright-report', 'test-results', 'src/lib/api/openapi.d.ts'] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  { files: ['src/**/*.{ts,tsx}'], ...hooks.configs.flat.recommended },
)
