// @ts-check
import assert from "assert";
import { test } from "node:test";
import { findDuplicateAdrNumbers, findAdrNumberGaps } from "./check-adr-numbers.js";

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

test("a contiguous sequence has no gaps", () => {
  const gaps = findAdrNumberGaps([
    "0000_template.md",
    "0001_plugins.md",
    "0002_credentials.md",
    "README.md",
  ]);
  assert.deepStrictEqual(gaps, []);
});

test("a single missing number is reported", () => {
  const gaps = findAdrNumberGaps(["0001_a.md", "0003_c.md"]);
  assert.deepStrictEqual(gaps, [2]);
});

test("several missing numbers are reported in order", () => {
  const gaps = findAdrNumberGaps([
    "0000_template.md",
    "0001_a.md",
    "0004_d.md",
    "0006_f.md",
  ]);
  assert.deepStrictEqual(gaps, [2, 3, 5]);
});

test("gaps are measured between the lowest and highest number, not from zero", () => {
  const gaps = findAdrNumberGaps(["0005_e.md", "0007_g.md"]);
  assert.deepStrictEqual(gaps, [6]);
});

test("no ADR files yields no gaps", () => {
  assert.deepStrictEqual(findAdrNumberGaps(["README.md"]), []);
});
