// @ts-check

// Comment flags and the benchmark flag each one sets.
/** @type {Record<string, string>} */
const FLAGS = {
  "--scenarios": "scenarios",
  "--scenario": "scenarios",
  "--objects": "objects",
  "--repeats": "repeats",
};

/**
 * Parse a "/perf [--scenarios=...] [--objects=...] [--repeats=...]" comment.
 *
 * @param {string} body - The comment body. Only the first line is read.
 * @returns {Record<string,string> | null} The given flags, or null when the comment is not a /perf command.
 */
export function parsePerfCommand(body) {
  const [command, ...args] = body.split("\n")[0].trim().split(/\s+/);
  if (command !== "/perf") {
    return null;
  }

  /** @type {Record<string, string>} */
  const flags = {};
  for (const arg of args) {
    const [name, value] = arg.split("=", 2);
    if (!Object.hasOwn(FLAGS, name) || !value) {
      throw new Error(`invalid argument: ${arg}`);
    }
    flags[FLAGS[name]] = value;
  }
  return flags;
}

/**
 * github-script entry point: turn a /perf comment into the benchmark job's inputs.
 *
 * Sets the outputs run, ref, scenarios, objects and repeats, and reacts to the
 * comment: 👀 when seen, 🚀 when launched, 😕 when invalid.
 *
 * @param {object} deps
 * @param {import("@actions/github-script").AsyncFunctionArguments["github"]} deps.github
 * @param {import("@actions/github-script").AsyncFunctionArguments["context"]} deps.context
 * @param {import("@actions/github-script").AsyncFunctionArguments["core"]} deps.core
 */
export default async function perfCommand({ github, context, core }) {
  const { owner, repo } = context.repo;
  const { comment, issue } = /** @type {{ comment: { id: number, body: string }, issue: { number: number } }} */ (context.payload);
  /** @param {"eyes" | "rocket" | "confused"} content */
  const react = (content) =>
    github.rest.reactions.createForIssueComment({ owner, repo, comment_id: comment.id, content });

  let flags;
  try {
    flags = parsePerfCommand(comment.body);
  } catch (err) {
    await react("confused");
    core.setFailed(String(err));
    return;
  }
  if (!flags) {
    core.setOutput("run", "false");
    return;
  }
  await react("eyes");

  const { data: pr } = await github.rest.pulls.get({ owner, repo, pull_number: issue.number });
  core.setOutput("run", "true");
  core.setOutput("ref", pr.head.sha);
  for (const name of ["scenarios", "objects", "repeats"]) {
    core.setOutput(name, flags[name] ?? "");
  }
  await react("rocket");
}
