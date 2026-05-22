---
title: ocm skill pull
description: Pull AI skills from an OCM skill catalogue component.
suppressTitle: true
toc: true
sidebar:
  collapsed: true
---

## ocm skill pull

Pull AI skills from an OCM skill catalogue component

### Synopsis

Pull one or all AI skills from an OCM component version that packages skills as resources with type ai.skill/v1.

When --skill is given, only that resource is downloaded. Without --skill, all ai.skill/v1 resources are downloaded.

By default skills are installed for Claude Code (--target claude):
  ~/.claude/skills/<skill-name>/SKILL.md

Use --target codex to install for OpenAI Codex CLI instead:
  ~/.agents/skills/<skill-name>/SKILL.md

Use --target all to install for both agents simultaneously.

```
ocm skill pull <component-ref> [--skill <name>] [--output <path>] [flags]
```

### Examples

```
  # Pull a single skill into Claude Code (default)
  ocm skill pull ./catalogue//jakob.io/ai-skill-catalogue:1.0.0 --skill ocm-guide

  # Pull all skills into OpenAI Codex CLI
  ocm skill pull ./catalogue//jakob.io/ai-skill-catalogue:1.0.0 --target codex

  # Pull all skills into both Claude Code and Codex
  ocm skill pull ./catalogue//jakob.io/ai-skill-catalogue:1.0.0 --target all

  # Pull a skill to a custom path
  ocm skill pull ./catalogue//jakob.io/ai-skill-catalogue:1.0.0 --skill ocm-guide --output /tmp/ocm-guide.md
```

### Options

```
  -h, --help            help for pull
      --output string   output path for the skill file (only valid with --skill)
      --skill string    name of skill resource to pull (pulls all ai.skill/v1 resources when omitted)
      --target string   coding agent to install skills for: "claude" (~/.claude/skills/), "codex" (~/.agents/skills/), or "all" (default "claude")
```

### Options inherited from parent commands

```
      --config string                      supply configuration by a given configuration file.
                                           By default (without specifying custom locations with this flag), the file will be read from one of the well known locations:
                                           1. The path specified in the OCM_CONFIG environment variable
                                           2. The XDG_CONFIG_HOME directory (if set), or the default XDG home ($HOME/.config), or the user's home directory
                                           - $XDG_CONFIG_HOME/ocm/config
                                           - $XDG_CONFIG_HOME/.ocmconfig
                                           - $HOME/.config/ocm/config
                                           - $HOME/.config/.ocmconfig
                                           - $HOME/.ocm/config
                                           - $HOME/.ocmconfig
                                           3. The current working directory:
                                           - $PWD/ocm/config
                                           - $PWD/.ocmconfig
                                           4. The directory of the current executable:
                                           - $EXE_DIR/ocm/config
                                           - $EXE_DIR/.ocmconfig
                                           If multiple configuration files are found, they will be merged in the order they are discovered.
                                           Using the option, this configuration file be used instead of the lookup above.
      --logformat enum                     set the log output format that is used to print individual logs
                                              json: Output logs in JSON format, suitable for machine processing
                                              text: Output logs in human-readable text format, suitable for console output
                                           (must be one of [json text]) (default text)
      --loglevel enum                      sets the logging level
                                              debug: Show all logs including detailed debugging information
                                              info:  Show informational messages and above
                                              warn:  Show warnings and errors only (default)
                                              error: Show errors only
                                           (must be one of [debug error info warn]) (default info)
      --logoutput enum                     set the log output destination
                                              stdout: Write logs to standard output
                                              stderr: Write logs to standard error, useful for separating logs from normal output
                                           (must be one of [stderr stdout]) (default stderr)
      --plugin-directory string            default directory path for ocm plugins. (default "$HOME/.config/ocm/plugins")
      --plugin-shutdown-timeout duration   Timeout for plugin shutdown. If a plugin does not shut down within this time, it is forcefully killed (default 10s)
      --temp-folder string                 Specify a custom temporary folder path for filesystem operations.
      --working-directory string           Specify a custom working directory path to load resources from.
```

### SEE ALSO

* [ocm skill]({{< relref "ocm_skill.md" >}})	 - Manage AI skills distributed via OCM component catalogues

