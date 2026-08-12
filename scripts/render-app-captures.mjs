#!/usr/bin/env node

import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const repositoryRoot = resolve(scriptDir, "..");
const captureRoot = join(repositoryRoot, "tui", "internal", "app", "testdata", "captures");
const sizes = [
  [160, 45],
  [120, 30],
  [90, 30],
  [70, 30],
];
const names = ["app-sessions", "app-session-dialog", "app-editor"];
const ansiColors = [
  "#000000", "#cd3131", "#0dbc79", "#e5e510",
  "#2472c8", "#bc3fbc", "#11a8cd", "#e5e5e5",
  "#666666", "#f14c4c", "#23d18b", "#f5f543",
  "#3b8eea", "#d670d6", "#29b8db", "#ffffff",
];

function color256(index) {
  if (index < 16) return ansiColors[index];
  if (index < 232) {
    const value = index - 16;
    const component = (part) => part === 0 ? 0 : 55 + part * 40;
    const red = component(Math.floor(value / 36));
    const green = component(Math.floor((value % 36) / 6));
    const blue = component(value % 6);
    return `rgb(${red},${green},${blue})`;
  }
  const gray = 8 + (index - 232) * 10;
  return `rgb(${gray},${gray},${gray})`;
}

function escapeHTML(value) {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;");
}

function ansiToHTML(input) {
  const state = {
    foreground: "",
    background: "",
    bold: false,
    dim: false,
    italic: false,
    underline: false,
  };
  const reset = () => {
    state.foreground = "";
    state.background = "";
    state.bold = false;
    state.dim = false;
    state.italic = false;
    state.underline = false;
  };
  const styled = (text) => {
    if (!text) return "";
    const rules = [];
    if (state.foreground) rules.push(`color:${state.foreground}`);
    if (state.background) rules.push(`background:${state.background}`);
    if (state.bold) rules.push("font-weight:700");
    if (state.dim) rules.push("opacity:.65");
    if (state.italic) rules.push("font-style:italic");
    if (state.underline) rules.push("text-decoration:underline");
    const escaped = escapeHTML(text);
    return rules.length === 0 ? escaped : `<span style="${rules.join(";")}">${escaped}</span>`;
  };

  let output = "";
  let offset = 0;
  const expression = /\x1b\[([\d;]*)m/g;
  for (let match = expression.exec(input); match; match = expression.exec(input)) {
    output += styled(input.slice(offset, match.index));
    const codes = (match[1] || "0").split(";").map(Number);
    for (let index = 0; index < codes.length; index += 1) {
      const code = codes[index];
      if (code === 0) reset();
      else if (code === 1) state.bold = true;
      else if (code === 2) state.dim = true;
      else if (code === 3) state.italic = true;
      else if (code === 4) state.underline = true;
      else if (code === 22) {
        state.bold = false;
        state.dim = false;
      } else if (code === 23) state.italic = false;
      else if (code === 24) state.underline = false;
      else if (code === 39) state.foreground = "";
      else if (code === 49) state.background = "";
      else if (code >= 30 && code <= 37) state.foreground = ansiColors[code - 30];
      else if (code >= 90 && code <= 97) state.foreground = ansiColors[code - 90 + 8];
      else if (code >= 40 && code <= 47) state.background = ansiColors[code - 40];
      else if (code >= 100 && code <= 107) state.background = ansiColors[code - 100 + 8];
      else if ((code === 38 || code === 48) && codes[index + 1] === 5) {
        const color = color256(codes[index + 2]);
        if (code === 38) state.foreground = color;
        else state.background = color;
        index += 2;
      } else if ((code === 38 || code === 48) && codes[index + 1] === 2) {
        const color = `rgb(${codes[index + 2]},${codes[index + 3]},${codes[index + 4]})`;
        if (code === 38) state.foreground = color;
        else state.background = color;
        index += 4;
      }
    }
    offset = match.index + match[0].length;
  }
  return output + styled(input.slice(offset));
}

const temporaryDirectory = mkdtempSync(join(tmpdir(), "agent-harbor-captures-"));
try {
  for (const [columns, rows] of sizes) {
    for (const name of names) {
      const source = join(captureRoot, `${columns}x${rows}`, `${name}.ansi`);
      const destination = join(captureRoot, `${columns}x${rows}`, `${name}.png`);
      const terminalWidth = Math.ceil(columns * 7.23 + 24);
      const terminalHeight = rows * 15 + 24;
      const html = `<!doctype html>
<meta charset="utf-8">
<style>
  * { box-sizing: border-box; }
  html, body { margin: 0; width: ${terminalWidth}px; height: ${terminalHeight}px; overflow: hidden; background: #000; }
  pre { margin: 0; padding: 12px; width: ${terminalWidth}px; height: ${terminalHeight}px; overflow: hidden;
    background: #000; color: #e5e5e5; font: 12px/15px Menlo, Monaco, monospace; letter-spacing: 0; white-space: pre; }
</style>
<pre>${ansiToHTML(readFileSync(source, "utf8"))}</pre>`;
      const page = join(temporaryDirectory, `${columns}x${rows}-${name}.html`);
      writeFileSync(page, html);
      execFileSync("playwright", [
        "screenshot",
        "--browser", "chromium",
        "--color-scheme", "dark",
        "--viewport-size", `${terminalWidth},${terminalHeight}`,
        pathToFileURL(page).href,
        destination,
      ], { stdio: "inherit" });
      console.log(`rendered ${destination}`);
    }
  }
} finally {
  rmSync(temporaryDirectory, { recursive: true, force: true });
}
