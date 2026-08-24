#!/usr/bin/env node
/**
 * Sync + convert Open Delivery Gear (ODG) documentation into the OCM Hugo website.
 *
 * Source : an Open Delivery Gear checkout's `docs/website` directory (Sphinx + MyST).
 * Target : this website's `content/docs/<section>/odg/**` (Hugo + Thulite Doks).
 *
 * Both projects follow the Diátaxis framework, so ODG pages are placed under the
 * matching OCM section (getting-started / concepts / how-to / tutorials / reference)
 * inside an `odg/` subtree, sharing the theme, search and navigation natively.
 *
 * Usage:
 *   node scripts/sync-odg-docs.js --src <path-to-open-delivery-gear/docs/website> [--ref <git-ref>]
 *
 * The conversion is deterministic and idempotent: re-running with the same source
 * produces the same output.
 */

const fs = require("fs");
const path = require("path");

const BT = "`";
const FENCE = BT + BT + BT;

// ---------------------------------------------------------------------------
// Argument parsing
// ---------------------------------------------------------------------------
function parseArgs(argv) {
  const args = { ref: "main" };
  for (let i = 2; i < argv.length; i++) {
    const a = argv[i];
    if (a === "--src") args.src = argv[++i];
    else if (a === "--ref") args.ref = argv[++i];
    else if (a === "--content") args.content = argv[++i];
    else if (a === "--static") args.static = argv[++i];
  }
  return args;
}

// ---------------------------------------------------------------------------
// Structure mapping
// ---------------------------------------------------------------------------
// Which reference config pages belong to the "core" vs "extensions" sub-group
// is derived from the two toctrees in the source reference/{core,extensions}/index.md.
function parseToctreeMembers(indexFile) {
  const members = [];
  if (!fs.existsSync(indexFile)) return members;
  const txt = fs.readFileSync(indexFile, "utf8");
  const lines = txt.split("\n");
  let inTree = false;
  for (const line of lines) {
    if (/^```{toctree}/.test(line)) {
      inTree = true;
      continue;
    }
    if (inTree && /^```\s*$/.test(line)) {
      inTree = false;
      continue;
    }
    if (inTree) {
      const m = line.trim().match(/([\w./-]+)\.md$/);
      if (m) members.push(path.basename(m[1]) + ".md"); // e.g. 02-artefact-enumerator-config.md
    }
  }
  return members;
}

function stripNum(basename) {
  return basename.replace(/^\d+[-_]/, "");
}
function slug(basename) {
  return stripNum(basename).replace(/\.md$/, "");
}
function weightFromName(basename) {
  const m = basename.match(/^(\d+)/);
  return m ? (parseInt(m[1], 10) + 1) * 10 : 500;
}

// Build the full plan: source-file -> { outPath, relref, weight, isIndex }
function buildPlan(srcRoot, contentRoot) {
  const contentsDir = path.join(srcRoot, "contents");
  const coreMembers = parseToctreeMembers(
    path.join(contentsDir, "reference/core/index.md"),
  );
  const extMembers = parseToctreeMembers(
    path.join(contentsDir, "reference/extensions/index.md"),
  );

  const plan = new Map(); // srcRel (posix, relative to srcRoot) -> entry
  const labelToRelref = new Map(); // maybe filled later
  const docPathToRelref = new Map(); // "contents/..." (no ext) -> relref

  const docsOut = "docs";

  function register(srcRel, section, sub, name, weight, isIndex) {
    const outRel = isIndex
      ? path.posix.join(docsOut, section, ...(sub ? [sub] : []), "_index.md")
      : path.posix.join(docsOut, section, ...(sub ? [sub] : []), name + ".md");
    const outPath = path.join(contentRoot, outRel);
    // relref target: site-root-relative content path.
    let relref;
    if (isIndex) {
      relref = "/" + path.posix.join(docsOut, section, ...(sub ? [sub] : []));
    } else {
      relref = "/" + outRel;
    }
    const entry = {
      srcRel,
      outRel,
      outPath,
      relref,
      weight,
      isIndex,
      section,
      sub,
      name,
    };
    plan.set(srcRel, entry);
    return entry;
  }

  const sectionMap = {
    "getting-started": "getting-started",
    concepts: "concepts",
    "how-to": "how-to",
    tutorial: "tutorials",
  };

  function walk(dir) {
    for (const de of fs.readdirSync(dir, { withFileTypes: true })) {
      const full = path.join(dir, de.name);
      if (de.isDirectory()) {
        walk(full);
        continue;
      }
      if (!de.name.endsWith(".md")) continue;
      const srcRel = path.posix.join(
        "contents",
        path.relative(contentsDir, full).split(path.sep).join("/"),
      );
      const parts = srcRel.split("/"); // contents / <section> / [sub/] file
      const section = parts[1];
      const base = parts[parts.length - 1];

      if (section === "reference") {
        // reference/core/index.md , reference/extensions/index.md
        if (parts.length === 4 && base === "index.md") {
          const sub = parts[2]; // core | extensions
          register(
            srcRel,
            "reference",
            path.posix.join("odg", sub),
            "_index",
            sub === "core" ? 20 : 30,
            true,
          );
          continue;
        }
        // flat reference/NN-*.md -> core / extensions / standalone
        let sub = "odg";
        if (coreMembers.includes(base)) sub = "odg/core";
        else if (extMembers.includes(base)) sub = "odg/extensions";
        register(
          srcRel,
          "reference",
          sub,
          slug(base),
          weightFromName(base),
          false,
        );
        continue;
      }

      const outSection = sectionMap[section];
      if (!outSection) continue; // ignore anything unexpected
      register(
        srcRel,
        outSection,
        "odg",
        slug(base),
        weightFromName(base),
        false,
      );
    }
  }
  walk(contentsDir);

  // doc-path map (keyed by normalized "contents/..." without extension)
  for (const e of plan.values()) {
    const key = e.srcRel.replace(/\.md$/, "");
    docPathToRelref.set(key, e);
    // reference index dirs are also referenced as contents/reference/core/index
  }

  return { plan, labelToRelref, docPathToRelref, coreMembers, extMembers };
}

// ---------------------------------------------------------------------------
// Pass 1: collect MyST labels -> owning page relref
// ---------------------------------------------------------------------------
function collectLabels(entry, ctx) {
  const txt = fs.readFileSync(path.join(ctx.srcRoot, entry.srcRel), "utf8");
  const re = /^\(([\w.-]+)\)=\s*$/;
  for (const line of txt.split("\n")) {
    const m = line.match(re);
    if (m) ctx.labelToRelref.set(m[1], entry);
  }
}

// ---------------------------------------------------------------------------
// Inline role conversion ({doc}, {ref})
// ---------------------------------------------------------------------------
function prettifySlug(s) {
  return s
    .replace(/^\d+[-_]/, "")
    .replace(/[-_]/g, " ")
    .replace(/\b\w/g, (c) => c.toUpperCase());
}

function resolveDocTarget(target, entry, ctx) {
  // target like "/contents/tutorial/00-contributing-extension" or "03-issue-replicator"
  let t = target.trim();
  t = t.replace(/^<|>$/g, "");
  t = t.replace(/\.md$/, "");
  let key;
  if (t.startsWith("/")) {
    key = t.slice(1);
  } else if (t.startsWith("contents/")) {
    key = t;
  } else {
    // relative to current source file's directory
    const dir = path.posix.dirname(entry.srcRel);
    key = path.posix.normalize(path.posix.join(dir, t));
  }
  // reference/core/index style
  const hit = ctx.docPathToRelref.get(key);
  return hit || null;
}

function convertInlineRoles(text, entry, ctx) {
  // rewrite static image paths: ![alt](/_static/x.svg) or (_static/x.svg) -> /docs/odg/x.svg
  text = text.replace(/(!\[[^\]]*\]\()\/?_static\//g, "$1/docs/odg/");

  // {doc}`text <path>` or {doc}`/path`
  text = text.replace(/\{doc\}`([^`]+)`/g, (_full, inner) => {
    let label = null,
      target = inner;
    const m = inner.match(/^(.*?)<([^>]+)>\s*$/);
    if (m) {
      label = m[1].trim();
      target = m[2].trim();
    }
    const hit = resolveDocTarget(target, entry, ctx);
    if (!hit) {
      ctx.warnings.push(`[${entry.srcRel}] unresolved {doc} target: ${target}`);
      return label || prettifySlug(target.split("/").pop());
    }
    const linkText =
      label ||
      prettifySlug(hit.name === "_index" ? hit.sub.split("/").pop() : hit.name);
    return `[${linkText}]({{< relref "${hit.relref}${hit.isIndex ? "" : ""}" >}})`;
  });

  // {ref}`text <label>` or {ref}`label`
  text = text.replace(/\{ref\}`([^`]+)`/g, (_full, inner) => {
    let label = null,
      target = inner;
    const m = inner.match(/^(.*?)<([^>]+)>\s*$/);
    if (m) {
      label = m[1].trim();
      target = m[2].trim();
    }
    target = target.trim();
    const owner = ctx.labelToRelref.get(target);
    if (!owner) {
      ctx.warnings.push(`[${entry.srcRel}] unresolved {ref} label: ${target}`);
      return label || prettifySlug(target);
    }
    const linkText = label || prettifySlug(target);
    return `[${linkText}]({{< relref "${owner.relref}${owner.isIndex ? "" : ""}" >}}#${target})`;
  });

  // Raw relative markdown links to other source docs: [text](../reference/18-x.md)
  // or [text](02-artefact-enumerator.md[#anchor]) — resolve via the doc-path map.
  text = text.replace(/\]\(([^)]+?\.md)(#[^)]*)?\)/g, (full, mdPath, frag) => {
    if (/^https?:|^{{</.test(mdPath)) return full; // external or already a shortcode
    const hit = resolveDocTarget(mdPath, entry, ctx);
    if (!hit) {
      ctx.warnings.push(
        `[${entry.srcRel}] unresolved relative link: ${mdPath}`,
      );
      return full;
    }
    return `]({{< relref "${hit.relref}" >}}${frag || ""})`;
  });

  return text;
}

// ---------------------------------------------------------------------------
// Admonition mapping
// ---------------------------------------------------------------------------
const CALLOUT_TYPE = {
  note: "note",
  seealso: "note",
  tip: "info",
  hint: "info",
  important: "warning",
  attention: "warning",
  caution: "warning",
  warning: "warning",
  danger: "danger",
  error: "danger",
};

// ---------------------------------------------------------------------------
// Block conversion (recursive descent over lines)
// ---------------------------------------------------------------------------
function convertBlocks(md, entry, ctx) {
  const lines = md.split("\n");
  const cur = { i: 0 };
  const out = [];
  processInto(lines, cur, out, entry, ctx, null);
  return out.join("\n");
}

function directiveCloser(indent, ch, len) {
  // closer is `indent` + ch repeated (>=1); we accept exactly the run of ch of any length
  const chEsc = ch === "`" ? "`" : ":";
  return new RegExp(
    "^" +
      indent.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") +
      "(" +
      chEsc +
      "{" +
      len +
      ",})\\s*$",
  );
}

function processInto(lines, cur, out, entry, ctx, stopCloser) {
  while (cur.i < lines.length) {
    const line = lines[cur.i];

    if (stopCloser && stopCloser.test(line)) {
      return;
    }

    // plain code fence (no directive) -> pass literally until closer
    const plainFence = line.match(/^(\s*)(`{3,}|~{3,})([^\s{].*)?$/);
    if (plainFence && !/\{[a-z-]+\}/.test(line)) {
      const indent = plainFence[1];
      const marker = plainFence[2][0];
      const len = plainFence[2].length;
      out.push(line);
      cur.i++;
      const closer = new RegExp(
        "^" +
          indent.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") +
          marker +
          "{" +
          len +
          ",}\\s*$",
      );
      while (cur.i < lines.length) {
        out.push(lines[cur.i]);
        if (closer.test(lines[cur.i])) {
          cur.i++;
          break;
        }
        cur.i++;
      }
      continue;
    }

    // directive opener: fence or colon, with {name}
    const dir = line.match(/^(\s*)(`{3,}|:{3,})\{([a-z-]+)\}(.*)$/);
    if (dir) {
      const indent = dir[1];
      const ch = dir[2][0];
      const len = dir[2].length;
      const name = dir[3];
      const arg = dir[4].trim();
      cur.i++;
      const closer = directiveCloser(indent, ch, len);
      // capture raw body
      const body = [];
      while (cur.i < lines.length && !closer.test(lines[cur.i])) {
        body.push(lines[cur.i]);
        cur.i++;
      }
      if (cur.i < lines.length) cur.i++; // consume closer
      emitDirective(name, arg, body, indent, out, entry, ctx);
      continue;
    }

    // MyST label anchor: (label)=
    const lab = line.match(/^\(([\w.-]+)\)=\s*$/);
    if (lab) {
      const label = lab[1];
      // find next non-blank line
      let j = cur.i + 1;
      while (j < lines.length && lines[j].trim() === "") j++;
      if (
        j < lines.length &&
        /^#{1,6}\s+/.test(lines[j]) &&
        !/\{#/.test(lines[j])
      ) {
        // attach to heading; drop label line + intermediate blanks handled by loop
        cur.i++;
        // push blanks
        while (cur.i < j) {
          out.push(lines[cur.i]);
          cur.i++;
        }
        out.push(lines[cur.i].replace(/\s*$/, "") + ` {#${label}}`);
        cur.i++;
      } else {
        out.push(`<a id="${label}"></a>`);
        cur.i++;
      }
      continue;
    }

    // regular line -> inline role conversion
    out.push(convertInlineRoles(line, entry, ctx));
    cur.i++;
  }
}

function emitDirective(name, arg, body, indent, out, entry, ctx) {
  if (name === "toctree") return; // navigation handled by Doks

  if (name === "mermaid") {
    ctx.hasMermaid = true;
    out.push(indent + FENCE + "mermaid");
    for (const b of body) out.push(b);
    out.push(indent + FENCE);
    return;
  }

  if (name === "code-block") {
    const lang = arg.split(/\s+/)[0] || "";
    let caption = null;
    const code = [];
    for (const b of body) {
      const opt = b.match(
        /^\s*:(caption|emphasize-lines|linenos|lineno-start|name|force|dedent):\s*(.*)$/,
      );
      if (opt) {
        if (opt[1] === "caption") caption = opt[2].trim();
        continue;
      }
      code.push(b);
    }
    // drop leading blank lines in code
    while (code.length && code[0].trim() === "") code.shift();
    if (caption) out.push(indent + `**${caption.replace(/`/g, "")}**`, "");
    out.push(indent + FENCE + lang);
    for (const c of code) out.push(c);
    out.push(indent + FENCE);
    return;
  }

  if (name === "figure") {
    const src = arg
      .trim()
      .replace(/^\/?_static\//, "/docs/odg/")
      .replace(/^\//, "/");
    let alt = "";
    const caption = [];
    for (const b of body) {
      const opt = b.match(
        /^\s*:(alt|width|height|figwidth|align|figclass|class|name|scale|target|loading):\s*(.*)$/,
      );
      if (opt) {
        if (opt[1] === "alt") alt = opt[2].trim();
        continue;
      }
      caption.push(b);
    }
    while (caption.length && caption[0].trim() === "") caption.shift();
    while (caption.length && caption[caption.length - 1].trim() === "")
      caption.pop();
    const capText = caption.join(" ").trim();
    if (!alt) alt = capText || "figure";
    out.push(indent + `![${alt.replace(/[[\]]/g, "")}](${src})`);
    if (capText)
      out.push("", indent + `*${convertInlineRoles(capText, entry, ctx)}*`);
    return;
  }

  if (name === "eval-rst") {
    // handle ".. note::" style admonitions inside
    let k = 0;
    while (k < body.length) {
      const m = body[k].match(/^\s*\.\.\s+([a-z-]+)::\s*(.*)$/);
      if (m && CALLOUT_TYPE[m[1]]) {
        const type = CALLOUT_TYPE[m[1]];
        const inner = [];
        if (m[2].trim()) inner.push(m[2].trim());
        k++;
        while (
          k < body.length &&
          (body[k].trim() === "" || /^\s+/.test(body[k]))
        ) {
          inner.push(body[k].replace(/^\s{1,3}/, ""));
          k++;
        }
        emitCallout(type, null, inner, indent, out);
      } else {
        k++;
      }
    }
    return;
  }

  if (name === "dropdown") {
    out.push(indent + "<details>");
    out.push(indent + `<summary>${arg || "Details"}</summary>`);
    out.push(indent + '<div markdown="1">', "");
    const inner = [];
    processInto(stripCommonIndent(body), { i: 0 }, inner, entry, ctx, null);
    for (const l of inner) out.push(l);
    out.push("", indent + "</div>", indent + "</details>");
    return;
  }

  if (CALLOUT_TYPE[name]) {
    const inner = [];
    processInto(stripCommonIndent(body), { i: 0 }, inner, entry, ctx, null);
    emitCallout(CALLOUT_TYPE[name], arg || null, inner, indent, out);
    return;
  }

  // unknown directive: keep content, warn
  ctx.warnings.push(
    `[${entry.srcRel}] unhandled directive {${name}} - inlined body`,
  );
  const inner = [];
  processInto(stripCommonIndent(body), { i: 0 }, inner, entry, ctx, null);
  for (const l of inner) out.push(indent + l);
}

function emitCallout(type, title, innerLines, indent, out) {
  const open = title
    ? `{{< callout context="${type}" title="${title.replace(/"/g, '\\"')}" >}}`
    : `{{< callout "${type}" >}}`;
  out.push(indent + open);
  // trim blank edges
  const b = innerLines.slice();
  while (b.length && b[0].trim() === "") b.shift();
  while (b.length && b[b.length - 1].trim() === "") b.pop();
  for (const l of b) out.push(indent + l);
  out.push(indent + "{{< /callout >}}");
}

function stripCommonIndent(lines) {
  let min = Infinity;
  for (const l of lines) {
    if (l.trim() === "") continue;
    const m = l.match(/^(\s*)/);
    min = Math.min(min, m[1].length);
  }
  if (!isFinite(min) || min === 0) return lines.slice();
  return lines.map((l) => (l.trim() === "" ? l : l.slice(min)));
}

// ---------------------------------------------------------------------------
// Frontmatter + H1 handling
// ---------------------------------------------------------------------------
function extractTitleAndBody(md) {
  const lines = md.split("\n");
  let title = null;
  let idx = 0;
  // skip leading blank / existing frontmatter
  while (idx < lines.length && lines[idx].trim() === "") idx++;
  for (let i = idx; i < lines.length; i++) {
    const m = lines[i].match(/^#\s+(.*)$/);
    if (m) {
      title = m[1].trim();
      lines.splice(0, i + 1);
      break;
    }
    if (lines[i].trim() !== "" && !/^#/.test(lines[i])) break;
  }
  return { title, body: lines.join("\n") };
}

function firstParagraph(body) {
  const lines = body.split("\n");
  const buf = [];
  let started = false;
  for (const l of lines) {
    const t = l.trim();
    if (started) {
      if (t === "") break;
      buf.push(t);
    } else {
      if (t === "" || t === "---") continue;
      if (
        /^[#>`:]/.test(t) ||
        /^\{\{</.test(t) ||
        /^[*_]{2}.*[*_]{2}$/.test(t) ||
        /^!\[/.test(t) ||
        /^<[a-z]/i.test(t)
      ) {
        // skip taglines / headings / callouts / images
        if (/^[*_]{2}.*[*_]{2}$/.test(t)) continue;
        continue;
      }
      started = true;
      buf.push(t);
    }
  }
  let p = buf.join(" ");
  p = p
    .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1") // links -> text
    .replace(/\{\{<[^>]*>\}\}/g, "")
    .replace(/[*_`]/g, "")
    .replace(/\s+/g, " ")
    .trim();
  if (p.length > 160) p = p.slice(0, 157).replace(/\s+\S*$/, "") + "…";
  return p;
}

function yamlStr(s) {
  return JSON.stringify(s == null ? "" : s);
}

function makeFrontmatter(fields) {
  const out = ["---"];
  for (const [k, v] of fields) {
    if (v === undefined || v === null) continue;
    if (typeof v === "boolean" || typeof v === "number") out.push(`${k}: ${v}`);
    else out.push(`${k}: ${yamlStr(v)}`);
  }
  out.push("---", "");
  return out.join("\n");
}

// ---------------------------------------------------------------------------
// Section landing pages (odg subtrees)
// ---------------------------------------------------------------------------
const SECTION_INDEX = {
  "getting-started": {
    title: "Open Delivery Gear",
    icon: "🚀",
    description:
      "Start here to understand ODG fundamentals and how to run and extend it.",
    body: "Onboarding for **Open Delivery Gear (ODG)** — a guided learning journey from OCM fundamentals all the way to running and extending ODG.",
  },
  concepts: {
    title: "Open Delivery Gear",
    icon: "🏗️",
    description:
      "Deep-dive into ODG architecture, data models, and how extensions work.",
    body: "Understanding-oriented explanations of **Open Delivery Gear (ODG)**: its Kubernetes-native architecture, the OCM-correlated data model, and the extension mechanisms that power automated compliance scanning.",
  },
  "how-to": {
    title: "Open Delivery Gear",
    icon: "🛠️",
    description:
      "Problem-oriented, goal-focused instructions for common ODG tasks and workflows.",
    body: "Task-oriented guides for **Open Delivery Gear (ODG)** — deploying locally, using the API, retrieving findings, managing SBOMs and SLAs, and preparing components for scanning.",
  },
  tutorials: {
    title: "Open Delivery Gear",
    icon: "🎓",
    description: "Learning-oriented, guided lessons to learn ODG by doing.",
    body: "Guided, learning-oriented lessons for **Open Delivery Gear (ODG)** — build your own extension and set up a full ODG environment from scratch.",
  },
  reference: {
    title: "Open Delivery Gear",
    icon: "📖",
    description:
      "Information-oriented technical specifications, API and configuration references for ODG.",
    body: "Technical reference for **Open Delivery Gear (ODG)**: the artefact-metadata query API, core and extension configuration, and the OCM label index.",
  },
};

function writeSectionIndex(section, contentRoot) {
  const meta = SECTION_INDEX[section];
  if (!meta) return;
  const dir = path.join(contentRoot, "docs", section, "odg");
  fs.mkdirSync(dir, { recursive: true });
  const fm = makeFrontmatter([
    ["title", meta.title],
    ["linkTitle", "Open Delivery Gear"],
    ["description", meta.description],
    ["icon", meta.icon],
    ["weight", 900],
    ["toc", false],
    ["sidebar", undefined],
  ]);
  // sidebar collapsed needs nested yaml
  const fmWithSidebar = fm.replace(
    "---\n\n",
    "sidebar:\n  collapsed: true\n---\n\n",
  );
  fs.writeFileSync(
    path.join(dir, "_index.md"),
    fmWithSidebar + meta.body + "\n",
  );
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------
function main() {
  const args = parseArgs(process.argv);
  const websiteRoot = path.resolve(__dirname, "..");
  const contentRoot = args.content
    ? path.resolve(args.content)
    : path.join(websiteRoot, "content");
  if (!args.src) {
    console.error("ERROR: --src <open-delivery-gear/docs/website> is required");
    process.exit(1);
  }
  const srcRoot = path.resolve(args.src);
  if (!fs.existsSync(path.join(srcRoot, "contents"))) {
    console.error(
      `ERROR: ${srcRoot} does not look like docs/website (no contents/)`,
    );
    process.exit(1);
  }

  const built = buildPlan(srcRoot, contentRoot);
  const ctx = {
    srcRoot,
    contentRoot,
    labelToRelref: built.labelToRelref,
    docPathToRelref: built.docPathToRelref,
    warnings: [],
  };

  // Pass 1: labels
  for (const entry of built.plan.values()) collectLabels(entry, ctx);

  // Clean previous odg output for idempotency
  for (const section of Object.keys(SECTION_INDEX)) {
    const d = path.join(contentRoot, "docs", section, "odg");
    if (fs.existsSync(d)) fs.rmSync(d, { recursive: true, force: true });
  }

  // Pass 2: convert files
  let count = 0;
  for (const entry of built.plan.values()) {
    ctx.hasMermaid = false;
    let raw;
    const abs = path.join(srcRoot, entry.srcRel);
    // resolve symlinks (e.g. how-to/01-local-setup.md -> local-setup/README.md)
    raw = fs.readFileSync(abs, "utf8");

    const { title, body } = extractTitleAndBody(raw);
    const converted =
      convertBlocks(body, entry, ctx)
        .replace(/\n{3,}/g, "\n\n")
        .trim() + "\n";

    const fmTitle = title || prettifySlug(entry.name);
    const desc = firstParagraph(converted) || fmTitle;

    const fields = [
      ["title", fmTitle],
      ["description", desc],
      ["weight", entry.weight],
      ["toc", true],
    ];
    if (ctx.hasMermaid) fields.push(["hasMermaid", true]);
    if (entry.isIndex) {
      fields.push(["sidebar", undefined]);
    }
    let fm = makeFrontmatter(fields);
    if (entry.isIndex)
      fm = fm.replace("---\n\n", "sidebar:\n  collapsed: true\n---\n\n");

    fs.mkdirSync(path.dirname(entry.outPath), { recursive: true });
    fs.writeFileSync(entry.outPath, fm + converted);
    count++;
  }

  // Section landing pages
  for (const section of Object.keys(SECTION_INDEX))
    writeSectionIndex(section, contentRoot);

  // Copy static assets into the Hugo assets pipeline (the Doks/Thulite image
  // render hook resolves markdown images as *resources* via resources.Get, which
  // reads from assets/, not static/).
  const staticSrc = path.join(srcRoot, "_static");
  const staticDst = path.join(websiteRoot, "assets", "docs", "odg");
  if (fs.existsSync(staticSrc)) {
    fs.mkdirSync(staticDst, { recursive: true });
    for (const f of fs.readdirSync(staticSrc)) {
      fs.copyFileSync(path.join(staticSrc, f), path.join(staticDst, f));
    }
  }

  console.log(
    `ODG sync complete: ${count} pages + ${Object.keys(SECTION_INDEX).length} section indexes (ref ${args.ref}).`,
  );
  console.log(
    `  content -> ${path.relative(websiteRoot, path.join(contentRoot, "docs"))}/{${Object.keys(SECTION_INDEX).join(",")}}/odg`,
  );
  console.log(`  static  -> ${path.relative(websiteRoot, staticDst)}`);
  if (ctx.warnings.length) {
    console.log(`\n${ctx.warnings.length} warning(s):`);
    for (const w of ctx.warnings.slice(0, 40)) console.log("  - " + w);
  }
}

if (require.main === module) main();

module.exports = {
  parseToctreeMembers,
  stripNum,
  slug,
  weightFromName,
  buildPlan,
  prettifySlug,
};
