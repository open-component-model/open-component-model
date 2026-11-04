// @ts-check

/**
 * Check whether a comment is the "/perf" command. Only the first line is read.
 *
 * @param {string} body - The comment body.
 * @returns {boolean} true for "/perf", false for any other comment.
 * @throws when "/perf" has arguments: the suite is fixed so every run is comparable.
 */
export function isPerfCommand(body) {
  // trimEnd, not trim: the workflow only starts this job for comments that begin with "/perf".
  const [command, ...args] = body.split("\n")[0].trimEnd().split(/\s+/);
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
  // The merge base, not the base branch tip: the tip moves between runs, the fork point does not.
  const { data: cmp } = await github.rest.repos.compareCommitsWithBasehead({
    owner, repo, basehead: `${pr.base.sha}...${pr.head.sha}`,
  });
  core.setOutput("base_ref", pr.base.ref);
  core.setOutput("base_sha", cmp.merge_base_commit.sha);
  core.setOutput("pr_sha", pr.head.sha);
  await react("rocket");
}
