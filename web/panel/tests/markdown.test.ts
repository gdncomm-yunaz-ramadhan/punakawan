import { describe, expect, it } from "vitest";
import { buildAnchor, groupIntoSections, parseDocument, snippetForSection } from "../src/lib/review/markdown";

const sampleMarkdown = `# Punakawan Panel

## Security Model

<!-- pk:block:panel.security.network-boundary -->
## Network Boundary

Default binding: 127.0.0.1 only

<!-- pk:block:panel.security.loopback-default -->
The panel binds to loopback by default.

## Another Section

Unrelated content here.
`;

describe("parseDocument", () => {
  it("assigns heading_path as the currently active nesting, mirroring blockid.go", () => {
    const nodes = parseDocument(sampleMarkdown);

    // The marker right before "## Network Boundary" sees the stack as it
    // stood just before that heading was pushed - Security Model is still
    // on top, matching blockid_test.go's own
    // "panel.security.network-boundary" expectation of
    // [Punakawan Panel, Security Model].
    const networkBoundaryMarker = nodes.find(
      (n) => n.kind === "blockMarker" && n.blockId === "panel.security.network-boundary",
    );
    expect(networkBoundaryMarker).toBeTruthy();
    if (networkBoundaryMarker?.kind === "blockMarker") {
      expect(networkBoundaryMarker.headingPath).toEqual(["Punakawan Panel", "Security Model"]);
    }

    // The "Network Boundary" heading itself pops "Security Model" (an
    // equal-level sibling) off the stack before being pushed - so its own
    // parentPath no longer includes Security Model.
    const networkBoundaryHeading = nodes.find((n) => n.kind === "heading" && n.text === "Network Boundary");
    expect(networkBoundaryHeading).toBeTruthy();
    if (networkBoundaryHeading?.kind === "heading") {
      expect(networkBoundaryHeading.headingPath).toEqual(["Punakawan Panel"]);
    }

    const loopbackMarker = nodes.find((n) => n.kind === "blockMarker" && n.blockId === "panel.security.loopback-default");
    expect(loopbackMarker).toBeTruthy();
    if (loopbackMarker?.kind === "blockMarker") {
      // Sibling level-2 headings replace each other in the stack - by the
      // time this marker appears (after "Network Boundary"'s heading
      // line), the path holds Network Boundary, not Security Model too.
      expect(loopbackMarker.headingPath).toEqual(["Punakawan Panel", "Network Boundary"]);
    }
  });

  it("captures paragraph text under the correct heading", () => {
    const nodes = parseDocument(sampleMarkdown);
    const para = nodes.find(
      (n) => n.kind === "paragraph" && n.text === "The panel binds to loopback by default.",
    );
    expect(para).toBeTruthy();
    if (para?.kind === "paragraph") {
      expect(para.headingPath).toEqual(["Punakawan Panel", "Network Boundary"]);
    }
  });
});

describe("groupIntoSections", () => {
  it("groups nodes per heading with a full heading path and first paragraph text", () => {
    const sections = groupIntoSections(parseDocument(sampleMarkdown));
    const anotherSection = sections.find((s) => s.heading?.text === "Another Section");
    expect(anotherSection).toBeTruthy();
    expect(anotherSection?.headingPath).toEqual(["Punakawan Panel", "Another Section"]);
    expect(anotherSection?.firstParagraphText).toBe("Unrelated content here.");
  });
});

describe("snippetForSection", () => {
  it("derives a snippet from the section's own first paragraph", () => {
    const sections = groupIntoSections(parseDocument(sampleMarkdown));
    const networkBoundary = sections.find((s) => s.heading?.text === "Network Boundary");
    expect(networkBoundary).toBeTruthy();
    expect(snippetForSection(networkBoundary!)).toBe("Default binding: 127.0.0.1 only");
  });

  it("falls back to the heading text when there is no paragraph", () => {
    const sections = groupIntoSections(parseDocument("# Title\n\n## Empty Heading\n"));
    const empty = sections.find((s) => s.heading?.text === "Empty Heading");
    expect(empty).toBeTruthy();
    expect(snippetForSection(empty!)).toBe("Empty Heading");
  });

  it("truncates long text to maxLen", () => {
    const longText = "word ".repeat(50).trim();
    const sections = groupIntoSections(parseDocument(`# Title\n\n${longText}\n`));
    const snippet = snippetForSection(sections[0], 20);
    expect(snippet!.length).toBeLessThanOrEqual(20);
  });
});

describe("buildAnchor", () => {
  it("produces the exact server anchor shape from a heading path + selection", () => {
    const anchor = buildAnchor({
      baseRevisionHash: "sha256:abc123",
      headingPath: ["Punakawan Panel", "Network Boundary"],
      quotedText: "loopback by default",
    });

    expect(anchor).toEqual({
      kind: "markdown_block",
      base_revision_hash: "sha256:abc123",
      heading_path: ["Punakawan Panel", "Network Boundary"],
      quoted_text: "loopback by default",
    });
  });

  it("omits heading_path/quoted_text/block_id when not provided", () => {
    const anchor = buildAnchor({ baseRevisionHash: "sha256:abc123" });
    expect(anchor).toEqual({ kind: "markdown_block", base_revision_hash: "sha256:abc123" });
  });

  it("includes block_id when provided", () => {
    const anchor = buildAnchor({ baseRevisionHash: "sha256:abc123", blockId: "panel.security.network-boundary" });
    expect(anchor.block_id).toBe("panel.security.network-boundary");
  });
});
