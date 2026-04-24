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

// -- GraphQL fragments ----------------------------------------------------

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
                repository { owner { login } name }
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
  const org = context.repo.owner;

  if (!projectNumber) {
    core.setFailed("projectNumber is required");
    return;
  }

  // 1. Fetch project configuration
  core.info("Fetching project configuration...");
  const configData = await github.graphql(PROJECT_CONFIG_QUERY, {
    org,
    number: projectNumber,
  });

  const project = configData.organization.projectV2;
  const projectId = project.id;
  const fields = project.fields.nodes;

  const statusField = fields.find((f) => f.name === "Status");
  const sprintField = fields.find((f) => f.name === "Sprint");

  if (!statusField || !sprintField) {
    core.setFailed("Could not find Status or Sprint fields on the project");
    return;
  }

  const needsRefinementOption = statusField.options.find((o) =>
    o.name.includes("Needs Refinement"),
  );
  if (!needsRefinementOption) {
    core.setFailed('Could not find a status option containing "Needs Refinement"');
    return;
  }

  core.info(`Project ID:   ${projectId}`);
  core.info(`Sprint field: ${sprintField.id}`);
  core.info(`Status match: ${needsRefinementOption.name}`);

  // 2. Determine the current sprint
  const today = new Date().toISOString().split("T")[0];
  const currentSprint = findCurrentSprint(
    sprintField.configuration.iterations,
    today,
  );

  if (!currentSprint) {
    core.setFailed("Could not determine current sprint");
    return;
  }
  core.info(`Current sprint: ${currentSprint.title} (${currentSprint.id})`);

  // 3. Fetch stale items using server-side filter
  //    The `query` parameter on projectV2.items supports the same filter syntax
  //    as the Projects UI, so we let GitHub do the heavy lifting.
  const itemsFilter = `sprint:<@current status:"${needsRefinementOption.name}"`;
  core.info(`Filter: ${itemsFilter}`);
  const staleItems = [];
  let cursor = null;

  do {
    const page = await github.graphql(STALE_ITEMS_QUERY, {
      projectId,
      cursor,
      filter: itemsFilter,
    });

    const { nodes, pageInfo, totalCount } = page.node.items;
    if (staleItems.length === 0) {
      core.info(`Matched items (server-side): ${totalCount}`);
    }
    staleItems.push(...nodes);

    cursor = pageInfo.hasNextPage ? pageInfo.endCursor : null;
  } while (cursor);

  core.info(
    `Found ${staleItems.length} issue(s) in "Needs Refinement" with expired sprints`,
  );

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

  // 4. Update each stale item to the current sprint and leave a comment
  let updated = 0;
  let failed = 0;

  for (const item of itemsToProcess) {
    const number = item.content?.number ?? "?";
    const title = item.content?.title ?? "unknown";
    const oldSprint = item.sprint?.title ?? "none";
    const repo = item.content?.repository;

    core.info(`Updating #${number} – ${title}`);
    core.info(`  Sprint: ${oldSprint} → ${currentSprint.title}`);

    try {
      await github.graphql(UPDATE_SPRINT_MUTATION, {
        projectId,
        itemId: item.id,
        fieldId: sprintField.id,
        iterationId: currentSprint.id,
      });

      // Leave a comment on the issue so the change is visible in the timeline
      if (repo && number !== "?") {
        try {
          await github.graphql(`
            mutation($body: String!, $subjectId: ID!) {
              addComment(input: { body: $body, subjectId: $subjectId }) {
                commentEdge { node { id } }
              }
            }
          `, {
            subjectId: item.content.id,
            body: `Sprint hygiene: automatically moved from **${oldSprint}** to **${currentSprint.title}** because this issue was still in "Needs Refinement" after the sprint ended.`,
          });
        } catch (commentErr) {
          core.warning(`  Comment failed: ${commentErr.message}`);
        }
      }

      core.info("  Done");
      updated++;
    } catch (err) {
      core.warning(`  Failed: ${err.message}`);
      failed++;
    }
  }

  core.info(`\nSummary: ${updated} updated, ${failed} failed out of ${itemsToProcess.length} total`);

  if (failed > 0) {
    core.setFailed(`${failed} item(s) failed to update`);
  }
}
