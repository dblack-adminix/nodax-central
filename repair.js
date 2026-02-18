const fs = require('fs');
const path = 'frontend/src/App.tsx';
let content = fs.readFileSync(path, 'utf8');

// Fix 1: Remove all orphaned table fragments that appear outside of proper JSX
// These are lines with <td> or </td> that aren't inside a <tr>
let lines = content.split('\n');
let result = [];
let inTableRow = false;
let inMapFunction = false;
let braceDepth = 0;

for (let i = 0; i < lines.length; i++) {
  const line = lines[i];
  const trimmed = line.trim();
  
  // Track if we're inside a map function
  if (trimmed.includes('pagedLogs.map') || trimmed.includes('.map(')) {
    inMapFunction = true;
  }
  if (inMapFunction) {
    braceDepth += (trimmed.match(/\{/g) || []).length;
    braceDepth -= (trimmed.match(/\}/g) || []).length;
    if (braceDepth <= 0 && trimmed.includes(')')) {
      inMapFunction = false;
      braceDepth = 0;
    }
  }
  
  // Check if this is a table row line
  if (trimmed.startsWith('<tr')) inTableRow = true;
  if (trimmed.startsWith('</tr>')) inTableRow = false;
  
  // Skip lines with table elements that are NOT inside table rows AND not in map functions
  const hasTableElement = trimmed.includes('<td') || trimmed.includes('</td>') || 
                          trimmed.includes('<tbody') || trimmed.includes('</tbody>') ||
                          trimmed.includes('<table') || trimmed.includes('</table>');
  
  if (hasTableElement && !inTableRow && !inMapFunction && !trimmed.startsWith('<table') && !trimmed.startsWith('</table>')) {
    // Check if this line is properly part of JSX or orphaned
    // If it starts with <td or </td at the beginning of line (after whitespace), it's likely orphaned
    if (trimmed.startsWith('<td') || trimmed.startsWith('</td>')) {
      continue; // Skip this line
    }
  }
  
  result.push(line);
}

content = result.join('\n');

// Fix 2: Remove orphaned )} lines that don't close anything
content = content.replace(/\n\s*\)\}\s*\n/g, '\n');

// Fix 3: Fix the specific issue around line 1000 - check for broken regex/template literals
// Look for patterns like {agents.map(agent => ( followed by broken content
content = content.replace(/\{agents\.map\(agent => \(\)\)}/g, '{agents.map(agent => (');

fs.writeFileSync(path, content, 'utf8');
console.log('Repaired file structure');
