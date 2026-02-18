const fs = require('fs');
const path = 'frontend/src/App.tsx';
let c = fs.readFileSync(path, 'utf8');

// Fix 1: Find and repair SVG path elements that lost their surrounding braces
// Pattern: } followed by newline followed by <path
// Should be } followed by newline and closing braces then <path
c = c.replace(/\}\s*\n\s*<path/g, '})}\n            <path');

// Fix 2: Fix orphaned JSX elements that should be inside map functions
// Look for <path, <circle etc that appear without proper function context
c = c.replace(/\n\s*<path d=\{/g, '\n            {points.length > 0 && <path d={');
c = c.replace(/\n\s*<circle /g, '\n            <circle ');

// Fix 3: Close unclosed expressions before SVG elements
// If we see } followed by <path without proper closing, add the closing
const lines = c.split('\n');
const result = [];
let inSvg = false;
let svgDepth = 0;

for (let i = 0; i < lines.length; i++) {
  const line = lines[i];
  const trimmed = line.trim();
  
  // Track SVG depth
  if (trimmed.startsWith('<svg')) { inSvg = true; svgDepth = 1; }
  if (trimmed.startsWith('</svg>')) { inSvg = false; svgDepth = 0; }
  if (inSvg) {
    svgDepth += (trimmed.match(/<\w[^/>]*>/g) || []).length;
    svgDepth -= (trimmed.match(/<\/\w+/g) || []).length;
  }
  
  // If we're in SVG and see a standalone <path without proper context, skip or fix
  if (inSvg && trimmed.startsWith('<path') && !result[result.length-1]?.includes('return') && 
      !result[result.length-1]?.includes('{') && !result[result.length-1]?.includes('}')) {
    // This path element might be orphaned - add closing braces from previous context
    // Actually let's just wrap it properly
    line = '            ' + line.trim();
  }
  
  result.push(line);
}

fs.writeFileSync(path, result.join('\n'), 'utf8');
console.log('Fixed SVG structure');
