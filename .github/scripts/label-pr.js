// @ts-check

/** Allowed conventional commit types. Single source of truth shared by validate and label. */
export const ALLOWED_TYPES = ['feat', 'fix', 'chore', 'docs', 'test', 'perf'];

/**
 * Parses a PR title against the Conventional Commit format.
 *
 * @param {string} title - The PR title to parse.
 * @param {string} allowedTypes - Pipe-separated list of allowed commit types, e.g. "feat|fix|chore".
 * @returns {{ type?: string, scope?: string, breaking?: string, subject?: string } | null}
 *   Named groups if the title matches, or null if it does not.
 */
export function parsePRTitle(title, allowedTypes) {
  // Matches Conventional Commit titles plus two special cases: "Initial commit" and merge commits.
  // Named groups: type, scope, breaking, subject.
  // Example: feat(scope)!: add new feature
  //          ^^^^ ^^^^^    ^^^^^^^^^^^^^^^
  //          type scope    subject
  const regex = new RegExp(
    `^(((Initial commit)|(Merge [^\\r\\n]+(\\s)[^\\r\\n]+((\\s)((\\s)[^\\r\\n]+)+)*(\\s)?)|^((?<type>${allowedTypes})(\\((?<scope>[\\w\\-]+)\\))?(?<breaking>!?): (?<subject>[^\\r\\n]+((\\s)((\\s)[^\\r\\n]+)+)*))(\\s)?)$)`
  );
  return title.match(regex)?.groups ?? null;
}

/**
 * Validates that a PR title follows the Conventional Commit format.
 * Calls core.setFailed if the title is invalid.
 *
 * @param {{ core: any, context: any }} opts
 */
export function validatePRTitle({ core, context }) {
  const prTitle = context.payload.pull_request.title;
  console.log(`PR Title: ${prTitle}`);

  if (!parsePRTitle(prTitle, ALLOWED_TYPES.join('|'))) {
    core.setFailed(
      `PR title "${prTitle}" does not follow the Conventional Commit format. ` +
      `Expected: <type>[(<scope>)][!]: <subject> — see https://www.conventionalcommits.org/en/v1.0.0/`
    );
  } else {
    console.log('PR title is valid.');
  }
}

/**
 * Applies labels to a PR based on its Conventional Commit title.
 * Title format validation is handled separately by validate-pr-title.yaml.
 *
 * @param {{ github: any, context: any }} opts
 */
export async function labelPR({ github, context }) {
  const typeToLabel = JSON.parse(process.env.TYPE_TO_LABEL ?? '{}');
  const scopeToLabel = JSON.parse(process.env.SCOPE_TO_LABEL ?? '{}');
  console.log("Type-to-Label Mapping:", typeToLabel);
  console.log("Scope-to-Label Mapping:", scopeToLabel);

  const allowedTypes = Object.keys(typeToLabel).join('|');
  const prTitle = context.payload.pull_request.title;
  console.log(`PR Title: ${prTitle}`);

  const groups = parsePRTitle(prTitle, allowedTypes);
  console.log(`Match: ${groups != null}`);

  if (groups) {
    const { type, scope, breaking } = groups;
    const labels = /** @type {string[]} */ ([]);

    if (breaking) {
      console.log("Adding breaking change label");
      labels.push(process.env.BREAKING_CHANGE_LABEL ?? '');
    }

    if (type && typeToLabel[type]) {
      labels.push(typeToLabel[type]);
    } else {
      console.log(`No label found for type: ${type}`);
    }

    if (scope && scopeToLabel[scope]) {
      labels.push(scopeToLabel[scope]);
    } else if (scope) {
      console.log(`No label found for scope: ${scope}`);
    }

    if (labels.length > 0) {
      console.log(`Adding labels: ${labels}`);
      await github.rest.issues.addLabels({
        owner: context.repo.owner,
        repo: context.repo.repo,
        issue_number: context.payload.pull_request.number,
        labels: labels,
      });
    } else {
      console.log("No labels to add.");
    }
  } else {
    throw new Error(
      "Invalid PR title format. Make sure you named the PR after the specification at " +
      "https://www.conventionalcommits.org/en/v1.0.0/#specification."
    );
  }
}
