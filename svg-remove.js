const fs = require('fs');
const path = 'frontend/src/App.tsx';
let c = fs.readFileSync(path, 'utf8');

// Find the MiniChart component or SVG section and simplify it
// Look for the pattern with the broken SVG structure

// Replace the entire broken SVG section with a simple div
const brokenPattern = /\{points\.map\(\(p, i\) => \{[\s\S]*?\}\)\}\s*\{points\.length > 0 && <path[\s\S]*?<\/svg>/;

const simpleReplacement = `<div style={{height: 40, background: 'var(--bg-tertiary)', borderRadius: 4, display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 12, color: 'var(--text-muted)'}}>
            📊 График
          </div>`;

// Try to find and replace the SVG section
if (brokenPattern.test(c)) {
  c = c.replace(brokenPattern, simpleReplacement);
  console.log('Replaced SVG with placeholder');
} else {
  // Try a broader pattern
  const broadPattern = /<svg[\s\S]*?<\/svg>/g;
  const matches = c.match(broadPattern);
  if (matches && matches.length > 0) {
    console.log(`Found ${matches.length} SVG blocks`);
    // Replace the problematic one (usually the last one in the charts section)
    c = c.replace(matches[matches.length - 1], simpleReplacement);
    console.log('Replaced last SVG block');
  }
}

fs.writeFileSync(path, c, 'utf8');
console.log('Done');
