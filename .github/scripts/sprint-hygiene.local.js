#!/usr/bin/env node
// @ts-check

/**
 * Local runner for sprint-hygiene.js.
 *
 * Usage:
 *   node sprint-hygiene.local.js [--apply] [--limit <n>] [--project <number>] [--org <name>]
 *
 * Runs in dry-run mode by default. Pass --apply to actually update sprints.
 * Use --limit to cap how many items are updated (useful for testing).
 * Requires GH_TOKEN (or GITHUB_TOKEN) with `read:project` and `project` scopes.
 */

import { graphql } from "@octokit/graphql";
import updateExpiredSprints from "./sprint-hygiene.js";

const args = process.argv.slice(2);

function flag(name) {
  return args.includes(`--${name}`);
}

function option(name, fallback) {
  const idx = args.indexOf(`--${name}`);
  if (idx === -1 || !args[idx + 1] || args[idx + 1].startsWith("--")) {
    return fallback;
  }
  return args[idx + 1];
}

const token = process.env.GH_TOKEN || process.env.GITHUB_TOKEN;
if (!token) {
  console.error("Error: GH_TOKEN or GITHUB_TOKEN must be set");
  process.exit(1);
}

const org = option("org", "open-component-model");
const projectNumber = Number(option("project", "10"));
const dryRun = !flag("apply"); // dry-run by default, --apply to mutate

const limitArg = option("limit", undefined);
let limit;
if (limitArg !== undefined) {
  limit = parseInt(limitArg, 10);
  if (Number.isNaN(limit) || limit <= 0) {
    console.error(`Error: --limit must be a positive integer, got "${limitArg}"`);
    process.exit(1);
  }
}

if (dryRun) {
  console.log("Running in DRY-RUN mode (pass --apply to make changes)\n");
}
if (limit !== undefined) {
  console.log(`Limiting updates to ${limit} item(s)\n`);
}

const graphqlWithAuth = graphql.defaults({
  headers: { authorization: `token ${token}` },
});

const github = { graphql: graphqlWithAuth };

const core = {
  info: (msg) => console.log(msg),
  warning: (msg) => console.warn(`WARNING: ${msg}`),
  setFailed: (msg) => {
    console.error(`FAILED: ${msg}`);
    process.exitCode = 1;
  },
};

const context = { repo: { owner: org } };

await updateExpiredSprints({ github, core, context, projectNumber, dryRun, limit });
