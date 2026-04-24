// @ts-check

/**
 * Automated sprint hygiene for GitHub Projects (v2).
 *
 * Finds issues in "Needs Refinement" status whose sprint has expired
 * and moves them to the current (or next upcoming) sprint.
 */

// -- Pure helpers (exported for testing) ----------------------------------

/**
 * Pick the current sprint from a list of active iterations.
 * Falls back to the next upcoming sprint if none contains `today`.
 *
 * @param {Array<{id: string, title: string, startDate: string, duration: number}>} iterations
 * @param {string} today - ISO date string (YYYY-MM-DD)
 * @returns {{id: string, title: string, startDate: string, duration: number} | null}
 */
export function findCurrentSprint(iterations, today) {
  // Find the most recently started iteration that started on or before today.
  const started = iterations
    .filter((i) => i.startDate <= today)
    .sort((a, b) => a.startDate.localeCompare(b.startDate));

  if (started.length > 0) {
    return started[started.length - 1];
  }

  // Nothing started yet — pick the nearest upcoming sprint.
  const upcoming = iterations
    .filter((i) => i.startDate > today)
    .sort((a, b) => a.startDate.localeCompare(b.startDate));

  return upcoming.length > 0 ? upcoming[0] : null;
}

// -- GraphQL queries ------------------------------------------------------

const PROJECT_CONFIG_QUERY = `
  query($org: String!, $number: Int!) {
    organization(login: $org) {
      projectV2(number: $number) {
        id
        fields(first: 30) {
          nodes {
            ... on ProjectV2SingleSelectField {
              id
              name
              options { name }
            }
            ... on ProjectV2IterationField {
              id
              name
              configuration {
                iterations { id title startDate duration }
              }
            }
          }
        }
      }
    }
  }
`;

const STALE_ITEMS_QUERY = `
  query($projectId: ID!, $cursor: String, $filter: String!) {
    node(id: $projectId) {
      ... on ProjectV2 {
        items(first: 100, after: $cursor, query: $filter) {
          totalCount
          pageInfo { hasNextPage endCursor }
          nodes {
            id
            content {
              ... on Issue {
                id
                title
                number
                url
              }
            }
            sprint: fieldValueByName(name: "Sprint") {
              ... on ProjectV2ItemFieldIterationValue {
                title
                iterationId
              }
            }
          }
        }
      }
    }
  }
`;

const ADD_COMMENT_MUTATION = `
  mutation($body: String!, $subjectId: ID!) {
    addComment(input: { body: $body, subjectId: $subjectId }) {
      commentEdge { node { id } }
    }
  }
`;

const UPDATE_SPRINT_MUTATION = `
  mutation($projectId: ID!, $itemId: ID!, $fieldId: ID!, $iterationId: String!) {
    updateProjectV2ItemFieldValue(input: {
      projectId: $projectId
      itemId: $itemId
      fieldId: $fieldId
      value: { iterationId: $iterationId }
    }) {
      projectV2Item { id }
    }
  }
`;

// -- API helpers ----------------------------------------------------------

/**
 * Resolve the "Needs Refinement" status option from the project's Status field.
 * Throws if no match is found or if the name contains characters that would
 * break the Projects v2 query filter syntax.
 */
function resolveNeedsRefinementStatus(statusField) {
  const matches = statusField.options.filter((o) =>
    o.name.includes("Needs Refinement"),
  );

  if (matches.length === 0) {
    throw new Error('Could not find a status option containing "Needs Refinement"');
  }

  const statusName = matches[0].name;

  if (statusName.includes('"')) {
    throw new Error(`Status name contains a double quote which is unsupported in the filter: ${statusName}`);
  }

  return statusName;
}

/**
 * Fetch project configuration and resolve the fields we need.
 *
 * @returns {{ projectId: string, sprintField: object, statusName: string }}
 */
async function fetchProjectConfig(github, core, { org, projectNumber }) {
  core.info("Fetching project configuration...");
  const data = await github.graphql(PROJECT_CONFIG_QUERY, {
    org,
    number: projectNumber,
  });

  const project = data.organization.projectV2;
  const fields = project.fields.nodes;

  const statusField = fields.find((f) => f.name === "Status");
  const sprintField = fields.find((f) => f.name === "Sprint");

  if (!statusField || !sprintField) {
    throw new Error("Could not find Status or Sprint fields on the project");
  }

  const statusName = resolveNeedsRefinementStatus(statusField);

  core.info(`Project ID:   ${project.id}`);
  core.info(`Sprint field: ${sprintField.id}`);
  core.info(`Status match: ${statusName}`);

  return { projectId: project.id, sprintField, statusName };
}

/**
 * Fetch all stale items matching the server-side filter (paginated).
 */
async function fetchStaleItems(github, core, { projectId, statusName }) {
  const filter = `sprint:<@current status:"${statusName}"`;
  core.info(`Filter: ${filter}`);

  const items = [];
  let cursor = null;

  do {
    const page = await github.graphql(STALE_ITEMS_QUERY, {
      projectId,
      cursor,
      filter,
    });

    const { nodes, pageInfo, totalCount } = page.node.items;
    if (items.length === 0) {
      core.info(`Matched items (server-side): ${totalCount}`);
    }
    items.push(...nodes);

    cursor = pageInfo.hasNextPage ? pageInfo.endCursor : null;
  } while (cursor);

  return items;
}

/**
 * Update a single item's sprint and leave a comment on the issue.
 */
async function updateItem(github, core, { item, projectId, sprintField, currentSprint }) {
  const number = item.content?.number ?? "?";
  const title = item.content?.title ?? "unknown";
  const oldSprint = item.sprint?.title ?? "none";

  core.info(`Updating #${number} – ${title}`);
  core.info(`  Sprint: ${oldSprint} → ${currentSprint.title}`);

  await github.graphql(UPDATE_SPRINT_MUTATION, {
    projectId,
    itemId: item.id,
    fieldId: sprintField.id,
    iterationId: currentSprint.id,
  });

  if (item.content?.id) {
    try {
      await github.graphql(ADD_COMMENT_MUTATION, {
        subjectId: item.content.id,
        body: `Sprint hygiene: automatically moved from **${oldSprint}** to **${currentSprint.title}** because this issue was still in "Needs Refinement" after the sprint ended.`,
      });
    } catch (commentErr) {
      core.warning(`  Comment failed: ${commentErr.message}`);
    }
  }

  core.info("  Done");
}

// -- Main entry point -----------------------------------------------------

/**
 * @param {object} args
 * @param {import('@actions/github-script').AsyncFunctionArguments["github"]} args.github
 * @param {import('@actions/github-script').AsyncFunctionArguments["core"]} args.core
 * @param {import('@actions/github-script').AsyncFunctionArguments["context"]} args.context
 * @param {number} args.projectNumber - GitHub Projects (v2) project number
 * @param {boolean} [args.dryRun] - If true, log what would be updated without making changes
 * @param {number} [args.limit] - Maximum number of items to update (undefined = all)
 */
export default async function updateExpiredSprints({ github, core, context, projectNumber, dryRun = false, limit }) {
  if (!projectNumber) {
    core.setFailed("projectNumber is required");
    return;
  }

  const { projectId, sprintField, statusName } = await fetchProjectConfig(
    github, core, { org: context.repo.owner, projectNumber },
  );

  const today = new Date().toISOString().split("T")[0];
  const currentSprint = findCurrentSprint(sprintField.configuration.iterations, today);

  if (!currentSprint) {
    core.setFailed("Could not determine current sprint");
    return;
  }
  core.info(`Current sprint: ${currentSprint.title} (${currentSprint.id})`);

  const staleItems = await fetchStaleItems(github, core, { projectId, statusName });

  core.info(`Found ${staleItems.length} issue(s) in "Needs Refinement" with expired sprints`);

  if (staleItems.length === 0) {
    core.info("Nothing to update.");
    return;
  }

  const itemsToProcess = limit !== undefined ? staleItems.slice(0, limit) : staleItems;

  if (dryRun) {
    core.info("\n--- DRY RUN — no changes will be made ---");
    for (const item of itemsToProcess) {
      const number = item.content?.number ?? "?";
      const title = item.content?.title ?? "unknown";
      const oldSprint = item.sprint?.title ?? "none";
      core.info(`  #${number} – ${title}  (${oldSprint} → ${currentSprint.title})`);
    }
    return;
  }

  const updatedItems = [];
  const failedItems = [];

  for (const item of itemsToProcess) {
    try {
      await updateItem(github, core, { item, projectId, sprintField, currentSprint });
      updatedItems.push(item);
    } catch (err) {
      const number = item.content?.number ?? "?";
      core.warning(`  #${number} failed: ${err.message}`);
      failedItems.push(item);
    }
  }

  core.info(`\nSummary: ${updatedItems.length} updated, ${failedItems.length} failed out of ${itemsToProcess.length} total`);

  if (updatedItems.length > 0) {
    core.info("\nUpdated issues:");
    for (const item of updatedItems) {
      core.info(`  - ${item.content?.url ?? `#${item.content?.number}`}`);
    }
  }

  if (failedItems.length > 0) {
    core.info("\nFailed issues:");
    for (const item of failedItems) {
      core.info(`  - ${item.content?.url ?? `#${item.content?.number}`}`);
    }
    core.setFailed(`${failedItems.length} item(s) failed to update`);
  }
}
