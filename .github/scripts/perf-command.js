// @ts-check

/**
 * Check whether a comment is the "/perf" command. Only the first line is read.
 *
 * @param {string} body - The comment body.
 * @returns {boolean} true for "/perf", false for any other comment.
 * @throws when "/perf" has arguments: the suite is fixed so every run is comparable.
 */
export function isPerfCommand(body) {
  const [command, ...args] = body.split("\n")[0].trim().split(/\s+/);
  if (command !== "/perf") {
    return false;
  }
  if (args.length > 0) {
    throw new Error(`/perf takes no arguments, got: ${args.join(" ")}`);
  }
  return true;
}

/**
 * github-script entry point: turn a /perf comment into the two commits to benchmark.
 *
 * Sets the outputs run, base_ref, base_sha and pr_sha, and reacts to the comment:
 * 👀 when seen, 🚀 when launched, 😕 when invalid.
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

  let run;
  try {
    run = isPerfCommand(comment.body);
  } catch (err) {
    await react("confused");
    core.setFailed(String(err));
    return;
  }
  core.setOutput("run", String(run));
  if (!run) {
    return;
  }
  await react("eyes");

  const { data: pr } = await github.rest.pulls.get({ owner, repo, pull_number: issue.number });
  const { data: base } = await github.rest.repos.getBranch({ owner, repo, branch: pr.base.ref });
  core.setOutput("base_ref", pr.base.ref);
  core.setOutput("base_sha", base.commit.sha);
  core.setOutput("pr_sha", pr.head.sha);
  await react("rocket");
}
