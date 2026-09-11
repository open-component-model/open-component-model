// @ts-check

/**
 * Build the Conventional Commit matching regex for the allowed types.
 *
 * The title is matched against several named groups:
 *   - type:     the change type (feat, fix, ...)
 *   - scope:    optional scope in parentheses
 *   - breaking: optional "!" marking a breaking change
 *   - subject:  the change subject
 * "Initial commit" and "Merge ..." titles are also accepted.
 *
 * @param {string[]} allowedTypes - Allowed Conventional Commit types.
 * @returns {RegExp}
 */
export function buildTitleRegex(allowedTypes) {
  const types = allowedTypes.join("|");
  return new RegExp(
    `^(((Initial commit)|(Merge [^\\r\\n]+(\\s)[^\\r\\n]+((\\s)((\\s)[^\\r\\n]+)+)*(\\s)?)|^((?<type>${types})(\\((?<scope>[\\w\\-]+)\\))?(?<breaking>!?): (?<subject>[^\\r\\n]+((\\s)((\\s)[^\\r\\n]+)+)*))(\\s)?)$)`,
  );
}

/**
 * Copy the entries of a plain object into a prototype-less object.
 *
 * Lookups on the result cannot resolve inherited members: a key such as
 * "constructor" or "toString" returns undefined instead of the corresponding
 * Object.prototype value. This makes every bracket lookup on the map safe.
 *
 * @param {Record<string,string[]>} source
 * @returns {Record<string,string[]>}
 */
export function nullProtoMap(source) {
  return Object.assign(Object.create(null), source);
}

/**
 * Derive the labels for a PR title following the Conventional Commit format.
 *
 * @param {string} prTitle - The pull request title.
 * @param {object} maps
 * @param {Record<string,string[]>} maps.typeToLabel - type -> labels.
 * @param {Record<string,string[]>} maps.scopeToLabel - scope -> labels.
 * @param {string} maps.breakingLabel - label added for breaking changes.
 * @returns {{ valid: boolean, labels: string[] }} valid is false when the title
 *   does not follow the Conventional Commit format; labels is then empty.
 */
export function deriveLabels(prTitle, { typeToLabel, scopeToLabel, breakingLabel }) {
  // Scope is free-form input from the PR title. On a plain object, a scope
  // equal to an Object.prototype member (e.g. "toString") would resolve to the
  // inherited function and spreading it below would throw; the prototype-less
  // maps make such lookups return undefined instead.
  const types = nullProtoMap(typeToLabel);
  const scopes = nullProtoMap(scopeToLabel);

  const regex = buildTitleRegex(Object.keys(types));
  const match = prTitle.match(regex);

  if (!match || !match.groups) {
    return { valid: false, labels: [] };
  }

  const { type, scope, breaking } = match.groups;
  const labels = [];

  if (breaking) {
    labels.push(breakingLabel);
  }
  if (type) {
    labels.push(...(types[type] ?? []));
  }
  if (scope) {
    labels.push(...(scopes[scope] ?? []));
  }

  // De-duplicate while preserving order: a type's labels and a scope's labels
  // can both contribute the same label (e.g. kind/chore).
  return { valid: true, labels: [...new Set(labels)] };
}

/**
 * github-script entry point: label the PR based on its title.
 *
 * Reads TYPE_TO_LABEL, SCOPE_TO_LABEL and BREAKING_CHANGE_LABEL from the
 * environment, derives the labels from the PR title, and applies them.
 * Fails the job when the title does not follow the Conventional Commit format.
 *
 * @param {object} deps
 * @param {import("@actions/github-script").AsyncFunctionArguments["github"]} deps.github
 * @param {import("@actions/github-script").AsyncFunctionArguments["context"]} deps.context
 * @param {import("@actions/github-script").AsyncFunctionArguments["core"]} deps.core
 */
export default async function labelFromTitle({ github, context, core }) {
  const typeToLabel = JSON.parse(process.env.TYPE_TO_LABEL);
  const scopeToLabel = JSON.parse(process.env.SCOPE_TO_LABEL);
  const breakingLabel = process.env.BREAKING_CHANGE_LABEL;

  const prTitle = context.payload.pull_request.title;
  core.info(`PR Title: ${prTitle}`);

  const { valid, labels } = deriveLabels(prTitle, {
    typeToLabel,
    scopeToLabel,
    breakingLabel,
  });

  if (!valid) {
    core.setFailed(
      "Invalid PR title format. Name the PR after the Conventional Commit " +
        "specification: https://www.conventionalcommits.org/en/v1.0.0/#specification",
    );
    return;
  }

  if (labels.length === 0) {
    core.info("No labels to add.");
    return;
  }

  core.info(`Adding labels: ${labels.join(", ")}`);
  await github.rest.issues.addLabels({
    owner: context.repo.owner,
    repo: context.repo.repo,
    issue_number: context.payload.pull_request.number,
    labels,
  });
}
