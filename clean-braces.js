const fs = require('fs');
const path = 'frontend/src/App.tsx';
let lines = fs.readFileSync(path, 'utf8').split('\n');

// Find and remove orphaned closing braces that appear at wrong places
// Pattern: lines that are just whitespace followed by )} (closing a non-existent block)
const toRemove = [];
for (let i = 0; i < lines.length; i++) {
  const line = lines[i];
  const trimmed = line.trim();
  
  // Remove lines that are just )} with optional whitespace
  if (trimmed === ')}') {
    // Check if previous line suggests this is a table/map closure (valid) or orphaned
    const prevLine = i > 0 ? lines[i-1].trim() : '';
    const nextLine = i < lines.length-1 ? lines[i+1].trim() : '';
    
    // If surrounded by pagination/table code, it might be valid
    // If it's in the middle of nowhere, it's orphaned
    if (!prevLine.includes('map') && !prevLine.includes(')') && !nextLine.includes('{')) {
      toRemove.push(i);
    }
  }
  
  // Also remove lines with just )} followed by more content on same line that's broken
  if (trimmed.startsWith(')}') && trimmed.length > 2 && !trimmed.startsWith(')})') && !trimmed.startsWith(')};')) {
    // Check if this looks like an orphaned fragment
    if (trimmed.includes('</td>') || trimmed.includes('<td')) {
      toRemove.push(i);
    }
  }
}

console.log('Removing lines:', toRemove.length);
// Remove in reverse order to preserve indices
toRemove.reverse().forEach(idx => {
  lines.splice(idx, 1);
});

fs.writeFileSync(path, lines.join('\n'), 'utf8');
console.log('Cleaned orphaned braces');
