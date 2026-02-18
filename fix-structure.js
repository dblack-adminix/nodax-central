const fs = require('fs');
const path = 'frontend/src/App.tsx';
let c = fs.readFileSync(path, 'utf8');

// Find the broken area and fix it
// The issue is there's a global-logs page block inside central-settings
// We need to extract it and place it properly, or remove it entirely

// Pattern to find: the broken insertion where global-logs is inside central-settings
const csComment = '{/* ============ CENTRAL SETTINGS ============ */}';
const csIdx = c.indexOf(csComment);

if (csIdx > 0) {
  // Look for the global-logs block that follows immediately (wrong place)
  const glPattern = "{page === 'global-logs' && (";
  let glIdx = c.indexOf(glPattern, csIdx);
  
  // If global-logs is before the proper closing of central-settings, it's in the wrong place
  const nextSection = c.indexOf('{/* ============', csIdx + csComment.length);
  
  if (glIdx > 0 && glIdx < nextSection) {
    console.log('Found misplaced global-logs block at', glIdx);
    
    // Find where the global-logs block ends
    let depth = 1;
    let i = glIdx + glPattern.length;
    while (depth > 0 && i < c.length) {
      if (c[i] === '(') depth++;
      if (c[i] === ')') depth--;
      i++;
    }
    // Find the closing )}
    const glEnd = c.indexOf(')}', i);
    if (glEnd > 0) {
      const glBlock = c.substring(glIdx, glEnd + 2);
      console.log('Extracted global-logs block length:', glBlock.length);
      
      // Remove it from inside central-settings
      c = c.substring(0, glIdx) + c.substring(glEnd + 2);
      
      // Now find a proper place to insert it - after central-settings page
      const csPageStart = c.indexOf("{page === 'central-settings'");
      if (csPageStart > 0) {
        let csDepth = 1;
        let j = csPageStart + 25;
        while (csDepth > 0 && j < c.length) {
          if (c[j] === '(') csDepth++;
          if (c[j] === ')') csDepth--;
          j++;
        }
        const csPageEnd = c.indexOf(')}', j) + 2;
        console.log('central-settings page ends at', csPageEnd);
        
        // Insert global-logs after central-settings
        c = c.substring(0, csPageEnd) + '\n\n' + glBlock + c.substring(csPageEnd);
      }
    }
  }
}

// Also remove any orphaned closing tags from previous broken edits
// Find and remove orphaned </div> and </td> tags in wrong places
c = c.replace(/\n\s*<\/div>\s*\n\s*\)\}\s*\n\s*<\/div>\s*\n\s*\)\}/g, '\n        )}');

fs.writeFileSync(path, c, 'utf8');
console.log('Fixed structure');
