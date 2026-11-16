// Animated "Try It Out" terminal for the landing page.
//
// The panel is server-rendered by layouts/home.html, which also carries the
// version and the exact install/inspect commands (version-aware, see
// _partials/main/cli-try-command.html) as data attributes. This module only
// replays them; it never decides which commands or version to show, so the
// animation always matches the copyable commands next to it.

const WIDTH = 26; // progress bar cells

function sleep(ms) {
    return new Promise((resolve) => setTimeout(resolve, ms));
}

function appendLine(term, cls) {
    const line = document.createElement("div");
    line.className = cls ? `cli-demo__line ${cls}` : "cli-demo__line";
    term.appendChild(line);
    return line;
}

function scrollToBottom(term) {
    term.scrollTop = term.scrollHeight;
}

// Type a shell command character by character behind a blinking cursor.
async function typeCommand(term, text, cancelled) {
    const line = appendLine(term, "cli-demo__line--cmd");
    line.innerHTML = "<span class=\"cli-demo__prompt\">$</span> ";
    const cursor = document.createElement("span");
    cursor.className = "cli-demo__cursor";
    line.appendChild(cursor);
    for (const char of text) {
        if (cancelled && cancelled()) {
            return;
        }
        cursor.insertAdjacentText("beforebegin", char);
        scrollToBottom(term);
        await sleep(char === " " ? 12 : 18 + Math.random() * 26);
    }
    await sleep(240);
    cursor.remove();
    await sleep(160);
}

// Print one output line. The html is composed here from fixed templates, never
// from user input, so it is safe to assign.
async function printLine(term, html, delay) {
    const line = appendLine(term, "cli-demo__line--in");
    line.innerHTML = html;
    scrollToBottom(term);
    await sleep(delay === undefined ? 90 : delay);
}

async function printBlank(term) {
    appendLine(term);
    await sleep(40);
}

// Animate a download-style progress bar that fills in place.
async function printProgress(term, label, cancelled) {
    const line = appendLine(term, "cli-demo__line--in");
    for (let percent = 0; percent <= 100; percent += 4 + Math.floor(Math.random() * 7)) {
        if (cancelled && cancelled()) {
            return;
        }
        const value = Math.min(percent, 100);
        const done = Math.round((value / 100) * WIDTH);
        const bar = `${"█".repeat(done)
        }<span class="cli-demo__bar-todo">${"░".repeat(WIDTH - done)}</span>`;
        line.innerHTML = `${label}  <span class="cli-demo__progress">${bar}</span> `
            + `<span class="cli-demo--cyan">${value}%</span>`;
        scrollToBottom(term);
        await sleep(55);
        if (value === 100) {
            break;
        }
    }
    await sleep(170);
}

function makeSetActive(panel) {
    return (step) => {
        for (const row of panel.querySelectorAll(".cli-demo__step")) {
            row.classList.toggle("is-active", Number(row.dataset.step) === step);
        }
    };
}

// Reflect playback state on the title-bar control: "Stop" while playing (so the
// reader can halt the animation, WCAG 2.2.2) and "Replay" once it is idle.
function setReplayState(panel, playing) {
    const button = panel.querySelector("[data-cli-replay]");
    if (!button) {
        return;
    }
    button.classList.toggle("is-playing", playing);
    const label = button.querySelector("[data-cli-replay-label]");
    if (label) {
        label.textContent = playing ? "Stop" : "Replay";
    }
    button.setAttribute("aria-label", playing ? "Stop the demo" : "Replay the demo");
}

function treeLines(version) {
    const platforms = ["linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64", "windows/amd64"];
    const rows = [
        "<span class=\"cli-demo--dim\"> COMPONENT         VERSION  PROVIDER      RESOURCE</span>",
        ` <span class="cli-demo--cyan">└─ ⬡</span> <span class="cli-demo--bold">ocm.software/cli</span>  <span class="cli-demo--bold">${version}</span>   ocm.software`
    ];
    platforms.forEach((platform, index) => {
        const branch = index === platforms.length - 1 ? "├─" : "├─";
        rows.push(`    <span class="cli-demo--dim">${branch} 📄</span> <span class="cli-demo--blue">cli</span>           <span class="cli-demo--dim">executable</span>   <span class="cli-demo--violet">${platform}</span>`);
    });
    rows.push(`    <span class="cli-demo--dim">└─ 📦</span> <span class="cli-demo--blue">ocmcli-image</span>  <span class="cli-demo--dim">ociImage</span>     <span class="cli-demo--cyan">ghcr.io/open-component-model/ocm.software/cli:${version}</span>`);
    return rows;
}

async function run(panel, term, config, state) {
    // Each invocation claims a run id. If a later invocation (e.g. Replay)
    // starts, this one sees a changed id and stops, so two transcripts never
    // interleave in the same terminal.
    const runId = (state.runId || 0) + 1;
    state.runId = runId;
    setReplayState(panel, true);
    const cancelled = () => state.runId !== runId;
    const setActive = makeSetActive(panel);
    term.innerHTML = "";
    setActive(1);

    // Step 1: install the CLI.
    await typeCommand(term, config.install, cancelled);
    if (cancelled()) {
        return;
    }
    await printLine(term, "<span class=\"cli-demo--dim\">==&gt;</span> Detecting platform  <span class=\"cli-demo--cyan\">darwin/arm64</span>", 240);
    if (cancelled()) {
        return;
    }
    await printLine(term, `<span class="cli-demo--dim">==&gt;</span> Resolving release  <span class="cli-demo--bold cli-demo--cyan">ocm.software/cli ${config.version}</span>`, 300);
    if (cancelled()) {
        return;
    }
    await printProgress(term, `<span class="cli-demo--dim">==&gt;</span> Downloading ocm ${config.version}`, cancelled);
    if (cancelled()) {
        return;
    }
    await printLine(term, "<span class=\"cli-demo--dim\">==&gt;</span> Verifying GitHub artifact attestation", 180);
    if (cancelled()) {
        return;
    }
    await printLine(term, "    <span class=\"cli-demo--green\">✓</span> provenance verified  <span class=\"cli-demo--dim\">(sigstore, keyless)</span>", 300);
    if (cancelled()) {
        return;
    }
    await printLine(term, "<span class=\"cli-demo--dim\">==&gt;</span> Installing to <span class=\"cli-demo--cyan\">~/.local/bin/ocm</span>", 240);
    if (cancelled()) {
        return;
    }
    await printLine(term, `<span class="cli-demo--green">✔</span> ocm ${config.version} installed and ready to go!`, 340);
    if (cancelled()) {
        return;
    }
    await printBlank(term);

    if (config.path) {
        await typeCommand(term, config.path, cancelled);
        if (cancelled()) {
            return;
        }
    }
    await typeCommand(term, "ocm version", cancelled);
    if (cancelled()) {
        return;
    }
    await printLine(term, `<span class="cli-demo--bold">${config.version}</span>`, 480);
    if (cancelled()) {
        return;
    }
    await printBlank(term);

    // Step 2: inspect the public CLI component.
    setActive(2);
    await typeCommand(term, config.inspect, cancelled);
    if (cancelled()) {
        return;
    }
    await printLine(term, "<span class=\"cli-demo--dim\">==&gt;</span> Fetching component descriptor  <span class=\"cli-demo--dim\">(no credentials needed)</span>", 420);
    if (cancelled()) {
        return;
    }
    await printBlank(term);
    for (const row of treeLines(config.version)) {
        await printLine(term, row, 140);
        if (cancelled()) {
            return;
        }
    }
    await printBlank(term);
    await printLine(term, "<span class=\"cli-demo--green\">✓</span> 1 component, 6 resources  <span class=\"cli-demo--dim\">— signed &amp; transportable anywhere</span>", 200);
    if (cancelled()) {
        return;
    }

    const done = appendLine(term, "cli-demo__line--cmd");
    done.innerHTML = "<span class=\"cli-demo__prompt\">$</span> <span class=\"cli-demo__cursor\"></span>";
    scrollToBottom(term);

    await sleep(5000);
    if (cancelled()) {
        return;
    }
    setActive(3); // invite the reader onward when the demo finishes
    setReplayState(panel, false);
}

function wireCopyButtons(panel) {
    for (const button of panel.querySelectorAll("[data-copy]")) {
        button.addEventListener("click", async () => {
            const text = button.getAttribute("data-copy");
            const flash = (cls) => {
                button.classList.add(cls);
                setTimeout(() => button.classList.remove(cls), 1400);
            };
            try {
                if (!navigator.clipboard || !navigator.clipboard.writeText) {
                    throw new Error("Clipboard API unavailable");
                }
                await navigator.clipboard.writeText(text);
                flash("is-copied");
            } catch {
                // Permission denied or no Clipboard API: tell the user to copy manually.
                flash("is-copy-failed");
            }
        });
    }
}

function initPanel(panel) {
    const term = panel.querySelector("[data-cli-term]");
    if (!term) {
        return;
    }
    const config = {
        version: panel.dataset.version || "latest",
        install: panel.dataset.install || "",
        path: panel.dataset.path || "",
        inspect: panel.dataset.inspect || ""
    };
    const state = { runId: 0 };

    wireCopyButtons(panel);

    const replay = panel.querySelector("[data-cli-replay]");
    if (replay) {
        replay.addEventListener("click", () => {
            if (replay.classList.contains("is-playing")) {
                // Stop: bump the run id to cancel the active invocation and remove
                // any blinking cursor, leaving the current output in place.
                state.runId = (state.runId || 0) + 1;
                for (const cursor of term.querySelectorAll(".cli-demo__cursor")) {
                    cursor.remove();
                }
                setReplayState(panel, false);
            } else {
                // run() bumps the run id, which cancels any invocation still playing.
                run(panel, term, config, state);
            }
        });
    }

    const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    if (reduceMotion) {
        return; // leave the terminal empty; the copyable commands stand on their own
    }

    let started = false;
    const observer = new IntersectionObserver((entries) => {
        for (const entry of entries) {
            if (entry.isIntersecting && !started) {
                started = true;
                run(panel, term, config, state);
            }
        }
    }, { threshold: 0.35 });
    observer.observe(term);
}

for (const panel of document.querySelectorAll("[data-cli-demo]")) {
    initPanel(panel);
}
