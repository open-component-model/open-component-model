---
title: "Issue Replicator"
description: "Configuration reference for the Issue Replicator extension."
weight: 7
toc: true
---

The Issue Replicator extension manages the GitHub issue lifecycle for findings. It creates, updates, and closes GitHub issues to track security and compliance findings discovered by other ODG extensions.

## Configuration Example

```yaml
issue_replicator:
  delivery_dashboard_url: https://delivery-dashboard.example.com
  interval: 3600                     # 1 hour
  mappings:
    - prefix: 'acme.org/product-a'
      github_repository: github.com/acme/product-a-issues
      github_issue_labels_to_preserve: ["do-not-remove-.*", "priority-.*"]
      number_included_closed_issues: 100
      milestones:
        title:
          prefix: "sprint-"
          sprint:
            value_type: name
    - prefix: ''                     # catch-all for all other components
      github_repository: github.com/acme/security-findings
      github_issue_labels_to_preserve: []
      number_included_closed_issues: 50
```

## Top-Level Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `delivery_dashboard_url` | string | — | Publicly accessible URL to the delivery dashboard (included in GitHub issues). |
| `interval` | int (seconds) | `3600` | Maximum time before GitHub issues are updated. |
| `mappings` | list | `[]` | Per-prefix component mappings. See mapping fields below. |

## Mapping Fields

Each entry in the `mappings` list supports the following fields:

| Option | Type | Required | Description |
|--------|------|----------|-------------|
| `prefix` | string | yes | Component name prefix. Use `''` (empty string) to match all components. |
| `github_repository` | string | yes | GitHub repository where issues should be created (e.g., `github.com/org/repo`). |
| `github_issue_labels_to_preserve` | list | `[]` | Regex patterns for labels that should not be removed on issue updates. |
| `number_included_closed_issues` | int | `100` | Number of closed issues to consider when deciding whether to create or reopen. Use `-1` for unlimited. |
| `milestones` | object | — | Configuration for mapping ODG sprints to GitHub milestones. |

## Milestone Configuration

The `milestones` object configures how ODG sprints are mapped to GitHub milestones:

| Option | Type | Description |
|--------|------|-------------|
| `milestones.title.prefix` | string | Prefix for GitHub milestone names (e.g., `"sprint-"`). |
| `milestones.title.sprint.value_type` | string | How to format the sprint: `name` (use sprint name) or `date` (use sprint date). |

## Configuration Details

### `interval`

The time (in seconds) between regular GitHub issue updates. Default is 3600 seconds (1 hour).

**Note:** Issues are also updated immediately in response to certain events:
- Initial scan completion
- Rescoring actions
- Finding status changes

This interval ensures issues are kept synchronized even without these triggering events.

### `mappings`

Allows routing findings from different components to different GitHub repositories
and applying different issue management policies.

#### Prefix Matching

The `prefix` field uses simple string prefix matching (not regex):
- `prefix: 'acme.org'` matches `acme.org/product` and `acme.org/another-product`
- `prefix: ''` (empty string) matches all components (use as a catch-all)

Multiple mappings are evaluated in order, and the first matching prefix is used.

#### GitHub Repository

Specifies the target GitHub repository for issue creation. Format: `github.com/org-name/repo-name`

**Example:**
```yaml
github_repository: github.com/my-org/security-findings
```

#### Preserved Labels

The `github_issue_labels_to_preserve` field contains regex patterns for labels that
should not be removed when ODG updates an issue. This is useful for:
- Manual labels added by team members
- Integration labels from other tools
- Priority or severity overrides

**Example:**
```yaml
github_issue_labels_to_preserve:
  - "do-not-remove-.*"
  - "priority-.*"
  - "team-assignment"
```

#### Closed Issue History

The `number_included_closed_issues` setting controls how far back the extension
looks when deciding whether to create a new issue or reopen an existing closed one.

- **Lower values** (e.g., `50`): Faster API operations, but may create duplicate issues if the original was closed long ago
- **Higher values** (e.g., `200`): Better deduplication, but more GitHub API requests
- **`-1`**: No limit (search all closed issues)

**Recommended:** Start with `100` and adjust based on your repository's issue volume.
