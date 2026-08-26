import js from "@eslint/js";
import globals from "globals";
import tseslint from "typescript-eslint";
import stylistic from "@stylistic/eslint-plugin";
import {defineConfig} from "eslint/config";

export default defineConfig([
    {
        files: ["**/*.{js,mjs,cjs,ts,mts,cts,jsx,tsx}"],
        plugins: {js, "@stylistic": stylistic},
        extends: ["js/recommended"],
        languageOptions: {globals: {...globals.browser, ...globals.node}}
    },
    tseslint.configs.recommended,
    {
        rules: {
            "curly": "error",
            "@stylistic/indent": ["error", 4],
            "@stylistic/brace-style": ["error", "1tbs"],
            "@typescript-eslint/no-unused-vars": ["error", {argsIgnorePattern: "^_", varsIgnorePattern: "^_"}],
        }
    },
    {
        // Node CLI helpers in scripts/ are CommonJS - require() is idiomatic here.
        files: ["scripts/**/*.{js,cjs}"],
        rules: {
            "@typescript-eslint/no-require-imports": "off",
        }
    },
]);
