/**
 * Block structure of a schema description. Descriptions are Go doc comment text,
 * from the OCM schema generator and from controller-gen for CRDs. This follows the
 * block rules of https://go.dev/doc/comment closely enough to render them well:
 * paragraphs, "# " headings, indented code blocks and indented lists. Inline syntax
 * such as doc links is left as plain text.
 */
export type DocBlock =
    | { kind: "paragraph"; text: string }
    | { kind: "heading"; text: string }
    | { kind: "code"; text: string }
    | { kind: "list"; ordered: boolean; items: string[][] };

/** Matches a list marker ("-", "*", "+", "•", "1." or "1)") followed by text. */
const LIST_MARKER = /^\s*(?:([-*+•])|(\d+)[.)])[ \t]+(?=\S)/;

const isBlank = (line: string) => line.trim() === "";
const isIndented = (line: string) => /^[ \t]/.test(line);
const leadingSpace = (line: string) => line.match(/^[ \t]*/)![0];

/** Removes the indentation shared by all non-blank lines of a code block. */
function unindent(lines: string[]): string[] {
    const indents = lines.filter((l) => !isBlank(l)).map(leadingSpace);
    let prefix = indents[0] ?? "";
    for (const indent of indents) {
        let i = 0;
        while (i < prefix.length && i < indent.length && prefix[i] === indent[i]) {
            i++;
        }
        prefix = prefix.slice(0, i);
    }
    return lines.map((l) => (isBlank(l) ? "" : l.slice(prefix.length)));
}

function parseList(lines: string[]): DocBlock {
    const ordered = !lines[0].match(LIST_MARKER)![1];
    const items: string[][] = [];
    let para: string[] = [];
    const flush = () => {
        if (para.length > 0 && items.length > 0) {
            items[items.length - 1].push(para.join(" "));
        }
        para = [];
    };

    for (const line of lines) {
        const marker = line.match(LIST_MARKER);
        if (marker && (marker[1] === undefined) === ordered) {
            flush();
            items.push([]);
            para.push(line.slice(marker[0].length).trim());
        } else if (isBlank(line)) {
            flush();
        } else {
            para.push(line.trim());
        }
    }
    flush();
    return { kind: "list", ordered, items };
}

export function parseDocText(text: string): DocBlock[] {
    const lines = text.split("\n").map((l) => (isBlank(l) ? "" : l));
    const blocks: DocBlock[] = [];
    let i = 0;

    while (i < lines.length) {
        if (lines[i] === "") {
            i++;
            continue;
        }

        const start = i;
        if (isIndented(lines[i])) {
            // An indented span runs until the next unindented line; it is a list if
            // it starts with a list marker and a code block otherwise.
            while (i < lines.length && (lines[i] === "" || isIndented(lines[i]))) {
                i++;
            }
            let end = i;
            while (lines[end - 1] === "") {
                end--;
            }
            const span = lines.slice(start, end);
            blocks.push(LIST_MARKER.test(span[0])
                ? parseList(span)
                : { kind: "code", text: unindent(span).join("\n") });
            i = end;
            continue;
        }

        // An unindented span runs until the next blank or indented line.
        while (i < lines.length && lines[i] !== "" && !isIndented(lines[i])) {
            i++;
        }
        const span = lines.slice(start, i);
        const heading = span.length === 1 && span[0].match(/^#[ \t]+(\S.*)$/);
        blocks.push(heading
            ? { kind: "heading", text: heading[1].trim() }
            : { kind: "paragraph", text: span.join(" ") });
    }

    return blocks;
}
