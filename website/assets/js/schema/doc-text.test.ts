import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { parseDocText } from "./doc-text.ts";

describe("parseDocText", () => {
    it("returns no blocks for empty text", () => {
        assert.deepEqual(parseDocText(""), []);
        assert.deepEqual(parseDocText(" \n\n "), []);
    });

    it("splits paragraphs on blank lines and joins hard-wrapped lines", () => {
        assert.deepEqual(parseDocText("First line\nwrapped.\n\nSecond."), [
            { kind: "paragraph", text: "First line wrapped." },
            { kind: "paragraph", text: "Second." }
        ]);
    });

    it("keeps indented code verbatim, including inner blank lines", () => {
        const text = "Example:\n\n\ttype: a\n\tlist:\n\t  - b\n\n\tother: c\n\nAfter.";
        assert.deepEqual(parseDocText(text), [
            { kind: "paragraph", text: "Example:" },
            { kind: "code", text: "type: a\nlist:\n  - b\n\nother: c" },
            { kind: "paragraph", text: "After." }
        ]);
    });

    it("parses lists directly following a paragraph", () => {
        assert.deepEqual(parseDocText("Supported:\n  - first\n  - second"), [
            { kind: "paragraph", text: "Supported:" },
            { kind: "list", ordered: false, items: [["first"], ["second"]] }
        ]);
    });

    it("folds wrapped list items onto one line", () => {
        const text = "  - helm: uploaded to\n    <url>/repository.\n  - generic: as is";
        assert.deepEqual(parseDocText(text), [
            { kind: "list", ordered: false, items: [["helm: uploaded to <url>/repository."], ["generic: as is"]] }
        ]);
    });

    it("keeps blank-separated items in one list and splits item paragraphs", () => {
        const text = " 1. One.\n\n    More on one.\n\n 2. Two.";
        assert.deepEqual(parseDocText(text), [
            { kind: "list", ordered: true, items: [["One.", "More on one."], ["Two."]] }
        ]);
    });

    it("treats unindented list markers as paragraph text", () => {
        assert.deepEqual(parseDocText("Text.\n- Not a list."), [
            { kind: "paragraph", text: "Text. - Not a list." }
        ]);
    });

    it("parses headings", () => {
        assert.deepEqual(parseDocText("Intro.\n\n# Usage\n\nBody."), [
            { kind: "paragraph", text: "Intro." },
            { kind: "heading", text: "Usage" },
            { kind: "paragraph", text: "Body." }
        ]);
    });

    it("removes the common indentation of code blocks", () => {
        assert.deepEqual(parseDocText("Code:\n\n    a:\n      b\n\n    c"), [
            { kind: "paragraph", text: "Code:" },
            { kind: "code", text: "a:\n  b\n\nc" }
        ]);
    });
});
