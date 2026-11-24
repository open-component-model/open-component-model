// @ts-check

import { readdirSync } from "node:fs";
import { pathToFileURL } from "node:url";

// ADR files follow "<number>_<title>.md", e.g. 0001_plugins.md.
const ADR_FILE_PATTERN = /^(\d+)_.+\.md$/;

/**
 * Find ADR numbers that are used by more than one file.
 *
 * @param {string[]} fileNames - Names of the files in the ADR directory.
 * @returns {{ number: number, files: string[] }[]} One entry per colliding
 *   number, with the names of the files that use it (sorted).
 */
export function findDuplicateAdrNumbers(fileNames) {
  const byNumber = new Map();
  for (const name of fileNames) {
    const match = ADR_FILE_PATTERN.exec(name);
    if (!match) {
      continue;
    }
    const number = parseInt(match[1], 10);
    const files = byNumber.get(number) ?? [];
    files.push(name);
    byNumber.set(number, files);
  }
  return [...byNumber.entries()]
    .filter(([, files]) => files.length > 1)
    .map(([number, files]) => ({ number, files: files.sort() }));
}

/**
 * Find ADR numbers that are missing from the otherwise contiguous sequence.
 *
 * Gaps are reported for every integer that is absent between the lowest and
 * the highest ADR number in use. The sequence is expected to run without
 * holes, so a missing number signals a renumbering mistake or a deletion.
 *
 * @param {string[]} fileNames - Names of the files in the ADR directory.
 * @returns {number[]} The missing ADR numbers, sorted ascending.
 */
export function findAdrNumberGaps(fileNames) {
  const numbers = new Set();
  for (const name of fileNames) {
    const match = ADR_FILE_PATTERN.exec(name);
    if (match) {
      numbers.add(parseInt(match[1], 10));
    }
  }
  if (numbers.size === 0) {
    return [];
  }
  const min = Math.min(...numbers);
  const max = Math.max(...numbers);
  const gaps = [];
  for (let number = min; number <= max; number++) {
    if (!numbers.has(number)) {
      gaps.push(number);
    }
  }
  return gaps;
}

/**
 * Scan a directory for ADR files and fail on duplicate or missing ADR numbers.
 *
 * @param {string} dir - Directory that contains the ADR files.
 */
export default function checkAdrNumbers(dir = "docs/adr") {
  const fileNames = readdirSync(dir);
  const duplicates = findDuplicateAdrNumbers(fileNames);
  const gaps = findAdrNumberGaps(fileNames);

  for (const { number, files } of duplicates) {
    console.error(`ADR number ${String(number).padStart(4, "0")} is used by multiple files:`);
    for (const file of files) {
      console.error(`  - ${file}`);
    }
  }
  if (duplicates.length > 0) {
    console.error("Rename the most recently merged ADR to the next free number.");
  }

  if (gaps.length > 0) {
    const list = gaps.map((number) => String(number).padStart(4, "0")).join(", ");
    console.error(`ADR numbers are not contiguous; missing: ${list}.`);
    console.error("Close the gap by renumbering later ADRs down, or add the missing ADR.");
  }

  if (duplicates.length === 0 && gaps.length === 0) {
    console.log(`All ADR numbers in ${dir} are unique and contiguous.`);
    return;
  }
  process.exitCode = 1;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  checkAdrNumbers(process.argv[2]);
}
