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

            // Modern syntax preferences.
            "no-var": "error",
            "prefer-const": ["error", {destructuring: "all"}],
            "prefer-arrow-callback": "error",
            "prefer-template": "error",
            "prefer-object-spread": "error",
            "prefer-numeric-literals": "error",
            "prefer-exponentiation-operator": "error",
            "object-shorthand": ["error", "always"],
            "dot-notation": "error",

            // Remove dead or redundant code.
            "no-useless-rename": "error",
            "no-useless-computed-key": "error",
            "no-extra-bind": "error",
            "no-lonely-if": "error",
            "no-else-return": "error",
            "no-unneeded-ternary": "error",
            "operator-assignment": ["error", "always"],
            "yoda": "error",
        }
    },
    {
        // TypeScript-only, auto-fixable defaults.
        files: ["**/*.{ts,mts,cts,tsx}"],
        rules: {
            "@typescript-eslint/array-type": ["error", {default: "array"}],
            "@typescript-eslint/consistent-type-imports": "error",
            "@typescript-eslint/prefer-as-const": "error",
            "@typescript-eslint/no-inferrable-types": "error",
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
