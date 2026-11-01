import tseslint from "typescript-eslint";

export default tseslint.config(
  { ignores: ["dist/**", "node_modules/**"] },
  ...tseslint.configs.recommended,
  {
    files: ["src/**/*.ts", "test/**/*.ts"],
    rules: {
      eqeqeq: "error",
      "no-console": "error",
      "@typescript-eslint/no-explicit-any": "error",
    },
  },
);
