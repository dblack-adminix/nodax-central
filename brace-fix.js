const fs = require('fs');
const path = 'frontend/src/App.tsx';
let content = fs.readFileSync(path, 'utf8');

// Find and fix the most common issues:

// 1. Remove orphaned standalone )} lines
content = content.replace(/\n\s*\)\}\s*\n/g, '\n');

// 2. Fix )} followed immediately by HTML tags on next line without proper structure
// This pattern: )}\n<h2> means the )} is orphaned
content = content.replace(/\)\}\s*\n\s*(<[a-zA-Z])/g, '\n$1');

// 3. Remove lines that are just closing tags without opening context
const lines = content.split('\n');
const result = [];
let jsxDepth = 0;

for (let i = 0; i < lines.length; i++) {
  const line = lines[i];
  const trimmed = line.trim();
  
  // Track JSX depth
  const openTags = (trimmed.match(/<[a-zA-Z][^/>]*>/g) || []).length;
  const closeTags = (trimmed.match(/<\/[a-zA-Z]+>/g) || []).length;
  const selfClosing = (trimmed.match(/<[^>]+\/>/g) || []).length;
  jsxDepth += openTags - closeTags - selfClosing;
  
  // If we see a standalone )} and jsxDepth is 0, it might be orphaned
  if (trimmed === ')}' && jsxDepth === 0) {
    // Check if previous line ends with a function call or map
    const prev = result[result.length - 1] || '';
    if (!prev.includes('map') && !prev.includes('filter') && !prev.includes('=>')) {
      continue; // Skip this orphaned line
    }
  }
  
  result.push(line);
}

fs.writeFileSync(path, result.join('\n'), 'utf8');
console.log('Applied brace fixes');
