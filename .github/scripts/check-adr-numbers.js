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
 * Scan a directory for ADR files and fail on duplicate ADR numbers.
 *
 * @param {string} dir - Directory that contains the ADR files.
 */
export default function checkAdrNumbers(dir = "docs/adr") {
  const duplicates = findDuplicateAdrNumbers(readdirSync(dir));
  if (duplicates.length === 0) {
    console.log(`All ADR numbers in ${dir} are unique.`);
    return;
  }
  for (const { number, files } of duplicates) {
    console.error(`ADR number ${String(number).padStart(4, "0")} is used by multiple files:`);
    for (const file of files) {
      console.error(`  - ${file}`);
    }
  }
  console.error("Rename the most recently merged ADR to the next free number.");
  process.exitCode = 1;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  checkAdrNumbers(process.argv[2]);
}
