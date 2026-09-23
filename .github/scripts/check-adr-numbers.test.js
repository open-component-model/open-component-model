// @ts-check
import assert from "assert";
import { test } from "node:test";
import { findDuplicateAdrNumbers } from "./check-adr-numbers.js";

test("unique numbers yield no duplicates", () => {
  const duplicates = findDuplicateAdrNumbers([
    "0000_template.md",
    "0001_plugins.md",
    "0002_credentials.md",
    "README.md",
  ]);
  assert.deepStrictEqual(duplicates, []);
});

test("a number used by two files is reported with both files sorted", () => {
  const duplicates = findDuplicateAdrNumbers([
    "0023_b.md",
    "0001_plugins.md",
    "0023_a.md",
  ]);
  assert.deepStrictEqual(duplicates, [{ number: 23, files: ["0023_a.md", "0023_b.md"] }]);
});

test("numbers with different zero padding collide", () => {
  const duplicates = findDuplicateAdrNumbers(["23_a.md", "0023_b.md"]);
  assert.deepStrictEqual(duplicates, [{ number: 23, files: ["0023_b.md", "23_a.md"] }]);
});

test("multiple colliding numbers are all reported", () => {
  const duplicates = findDuplicateAdrNumbers([
    "0023_b.md",
    "0024_b.md",
    "0023_a.md",
    "0024_a.md",
  ]);
  assert.deepStrictEqual(duplicates, [
    { number: 23, files: ["0023_a.md", "0023_b.md"] },
    { number: 24, files: ["0024_a.md", "0024_b.md"] },
  ]);
});
