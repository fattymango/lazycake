module.exports = {
  root: true,
  env: { browser: true, es2021: true },
  extends: [
    "eslint:recommended",
    "plugin:@typescript-eslint/recommended",
    "plugin:react-hooks/recommended",
  ],
  parser: "@typescript-eslint/parser",
  parserOptions: { ecmaVersion: "latest", sourceType: "module" },
  plugins: ["react-refresh"],
  rules: {
    "react-refresh/only-export-components": "warn",
    "@typescript-eslint/no-unused-vars": ["warn", { argsIgnorePattern: "^_" }],
  },
  overrides: [
    {
      // Context providers and their hooks (and cva variant helpers) live in the
      // same file by design here, so the fast-refresh export rule doesn't apply.
      files: ["src/lib/**/*.{ts,tsx}", "src/ui/**/*.{ts,tsx}"],
      rules: { "react-refresh/only-export-components": "off" },
    },
  ],
  ignorePatterns: ["dist", "*.config.js", "*.config.ts"],
};
