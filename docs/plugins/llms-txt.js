const fs = require("fs");
const path = require("path");

function cleanBody(body, dir) {
  if (dir) {
    for (const m of body.matchAll(/^import\s+(\w+)\s+from\s+["'](\.\/[\w./-]+\.md)["']/gm)) {
      const file = path.join(dir, m[2]);
      if (!fs.existsSync(file)) continue;
      const partial = readBody(file).replace(/^\{\/\*[\s\S]*?\*\/\}\n/, "").trim();
      body = body.replace(new RegExp(`<${m[1]}\\s*/>`, "g"), () => `\n${partial}\n`);
    }
  }
  body = body
    .replace(/<TabItem\b[^>]*\b(?:label|value)="([^"]+)"[^>]*>/g, "\n### $1\n")
    .replace(/<\/?Tabs\b[^>]*>/g, "")
    .replace(/<\/?ExpandableTable\b[^>]*>/g, "")
    .replace(/<\/TabItem>/g, "");
  const lines = [];
  let inMdx = false;
  let fence = null;
  for (const l of body.split("\n")) {
    const t = l.trim();
    if (!fence && t === "```mdx-code-block") { inMdx = true; continue; }
    if (inMdx && t === "```") { inMdx = false; continue; }
    const marker = t.match(/^(```|~~~)/);
    if (!inMdx && marker && (!fence || marker[1] === fence)) {
      fence = fence ? null : marker[1];
    }
    if (!fence && /^import\s.+\sfrom\s["']/.test(l)) continue;
    lines.push(l);
  }
  return lines.join("\n").replace(/\n{3,}/g, "\n\n");
}

function readBody(file) {
  const raw = fs.readFileSync(file, "utf8");
  const fm = raw.match(/^---\n[\s\S]*?\n---\n/);
  return fm ? raw.slice(fm[0].length) : raw;
}

function sidebarSections(sidebars) {
  const all = Object.values(sidebars);
  if (all.length !== 1) {
    throw new Error(`[llms-txt] expected one sidebar, found ${all.length}; update the plugin`);
  }
  const sections = [];
  let current = { label: null, ids: [] };
  for (const item of all[0]) {
    if (item.type === "doc") {
      current.ids.push(item.id);
    } else if (item.type === "category") {
      if (current.ids.length) sections.push(current);
      const ids = item.items.filter((i) => i.type === "doc").map((i) => i.id);
      sections.push({ label: item.label, ids });
      current = { label: null, ids: [] };
    }
  }
  if (current.ids.length) sections.push(current);
  return sections;
}

module.exports = function llmsTxtPlugin() {
  return {
    name: "llms-txt",
    async postBuild({ siteConfig, siteDir, outDir, plugins }) {
      const docsPlugin = plugins.find((p) => p.name === "docusaurus-plugin-content-docs");
      const version = docsPlugin.content.loadedVersions[0];
      const docs = new Map(version.docs.map((d) => [d.id, d]));
      const site = `${siteConfig.url}${siteConfig.baseUrl}`;

      const sections = sidebarSections(version.sidebars);
      const listed = new Set(sections.flatMap((s) => s.ids));
      const unlisted = [...docs.keys()].filter((id) => !listed.has(id));
      if (unlisted.length) sections.push({ label: "Other pages", ids: unlisted.sort() });

      const index = [];
      const full = [];
      for (const section of sections) {
        if (section.label) index.push(`\n- ${section.label}\n`);
        for (const id of section.ids) {
          const doc = docs.get(id);
          if (!doc) {
            console.warn(`[llms-txt] sidebar lists "${id}" but no such doc was loaded; skipping`);
            continue;
          }
          const route = doc.permalink.slice(siteConfig.baseUrl.length);
          const url = `${siteConfig.url}${doc.permalink}`;
          const note = doc.description ? `: ${doc.description}` : "";
          index.push(`  - [${doc.title}](${url})${note}`);
          const file = path.join(siteDir, doc.source.replace(/^@site\//, ""));
          const body = cleanBody(readBody(file), path.dirname(file)).trim();
          full.push(`# ${doc.title}\n\nSource: ${url}\n\n${body}\n`);
          const clean = body.startsWith("# ")
            ? `${body}\n`
            : `# ${doc.title}\n\n${doc.description ? doc.description + "\n\n" : ""}${body}\n`;
          const target = path.join(outDir, `${route === "" ? "index" : route}.md`);
          fs.mkdirSync(path.dirname(target), { recursive: true });
          fs.writeFileSync(target, clean);
          if (route === "") fs.writeFileSync(path.join(outDir, `${id}.md`), clean);
        }
      }

      const header =
        "Append `.md` to any docs page URL below for its raw Markdown " +
        `(e.g. \`${site}installation.md\`; the front page is \`${site}index.md\`), ` +
        `or fetch all current pages at \`${site}llms-full.txt\`.\n\n` +
        `# ${siteConfig.title} Docs\n\n> ${siteConfig.title}: ${siteConfig.tagline}, ` +
        "written in Go.\n";

      fs.writeFileSync(path.join(outDir, "llms.txt"), header + "\n" + index.join("\n") + "\n");
      fs.writeFileSync(path.join(outDir, "llms-full.txt"), full.join("\n---\n\n"));
      console.log("[llms-txt] wrote llms.txt, llms-full.txt and raw .md routes");
    },
  };
};

module.exports.cleanBody = cleanBody;
