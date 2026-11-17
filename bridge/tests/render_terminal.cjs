#!/usr/bin/env node
// Render synthetic ANSI evidence with modern emoji/grapheme cell widths.
// This is a development tool, never a product or package dependency.
const fs = require('node:fs');
const path = require('node:path');
const { createRequire } = require('node:module');
const { pathToFileURL } = require('node:url');

async function main() {
  const args = process.argv.slice(2);
  const option = name => {
    const i = args.indexOf(name);
    return i >= 0 ? args[i + 1] : undefined;
  };
  const input = option('--input');
  const output = option('--output');
  const modules = option('--modules');
  if (!input || !output || !modules) {
    throw new Error('Usage: render_terminal.cjs --input ANSI_DIR --output DIR --modules NODE_MODULES [--check-divider] [--legacy-unicode] [--executable PATH]');
  }
  const load = createRequire(path.join(path.resolve(modules), '__visual_require.cjs'));
  const { chromium } = load('playwright');
  const xterm = load.resolve('@xterm/xterm');
  const addon = load.resolve('@xterm/addon-unicode-graphemes');
  const css = path.join(path.dirname(xterm), '../css/xterm.css');
  const legacy = args.includes('--legacy-unicode');
  fs.mkdirSync(output, { recursive: true });
  const executable = option('--executable');
  const browser = await chromium.launch({ ...(executable ? { executablePath: executable } : { channel: 'chrome' }), headless: true });
  const results = [];
  let failed = false;
  try {
    for (const name of fs.readdirSync(input).filter(n => n.endsWith('.ansi')).sort()) {
      const match = name.match(/-(60|80|100|160)\.ansi$/);
      if (!match) continue;
      const cols = Number(match[1]);
      const rows = Number(option("--rows")) || { 60: 20, 80: 24, 100: 30, 160: 45 }[cols];
      const data = fs.readFileSync(path.join(input, name), 'utf8').replace(/\r\n/g, '\n').replace(/\n/g, '\r\n');
      // A directory or session name must never terminate the script element.
      const encoded = JSON.stringify(data).replace(/</g, '\\u003c');
      const html = `<!doctype html><html><head><link rel="stylesheet" href="${pathToFileURL(css)}"></head>
<body style="margin:0;background:#132235;padding:24px"><div id="terminal"></div>
<script src="${pathToFileURL(xterm)}"></script><script src="${pathToFileURL(addon)}"></script>
<script>
const term = new Terminal({allowProposedApi:true,cols:${cols},rows:${rows},fontSize:15,fontFamily:'Menlo, monospace',lineHeight:1.0,theme:{background:'#132235',foreground:'#D9E5EF'},cursorBlink:false});
${legacy ? '' : 'term.loadAddon(new UnicodeGraphemesAddon.UnicodeGraphemesAddon());'}
term.open(document.getElementById('terminal'));
window.terminalReady = new Promise(resolve => term.write(${encoded}, resolve));
</script></body></html>`;
      const htmlPath = path.resolve(output, name.replace(/\.ansi$/, '.html'));
      fs.writeFileSync(htmlPath, html, { mode: 0o600 });
      const page = await browser.newPage({ viewport: { width: Math.ceil(cols * 9.5 + 48), height: rows * 20 + 48 } });
      try {
        await page.goto(pathToFileURL(htmlPath).href);
        await page.evaluate(() => window.terminalReady);
        const geometry = await page.evaluate(() => {
          const divider = [];
          for (let y = 0; y < term.rows; y++) {
            const line = term.buffer.active.getLine(y);
            const cells = [];
            for (let x = 0; x < term.cols; x++) {
              if (line.getCell(x).getChars() === '│') cells.push(x);
            }
            // This check targets the single-divider Harbor session fixture.
            if (cells.length === 1) divider.push({ row: y, column: cells[0] });
          }
          return { unicode: term.unicode.activeVersion, divider };
        });
        const columns = [...new Set(geometry.divider.map(r => r.column))];
        const aligned = columns.length <= 1;
        results.push({ file: name, ...geometry, columns, aligned });
        await page.screenshot({ path: path.join(output, name.replace(/\.ansi$/, '.png')), fullPage: true });
        if (args.includes('--check-divider') && !aligned) failed = true;
      } finally {
        await page.close();
      }
    }
  } finally {
    await browser.close();
  }
  fs.writeFileSync(path.join(output, 'geometry.json'), JSON.stringify(results, null, 2), { mode: 0o600 });
  console.log(`${failed ? 'FAIL' : 'PASS'} rendered ${results.length} terminal fixtures; Unicode ${legacy ? '6 (diagnostic)' : '15-graphemes'}`);
  if (!results.length || failed) process.exitCode = 1;
}
main().catch(error => { console.error(error.message); process.exitCode = 1; });
