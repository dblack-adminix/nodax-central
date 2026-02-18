const fs = require('fs');
const path = 'frontend/src/App.tsx';
let c = fs.readFileSync(path, 'utf8');

// Remove ALL global-logs related code that I added
// 1. Remove global-logs page type
const pageTypeMatch = c.match(/type Page = '([^']+)'/);
if (pageTypeMatch) {
  const originalTypes = "'overview' | 'host' | 'statistics' | 'central-settings' | 's3-browser' | 'smb-browser' | 'webdav-browser'";
  c = c.replace(/type Page = [^;]+;/, `type Page = ${originalTypes};`);
}

// 2. Remove global-logs nav button
const glNavPattern = /<button className=\{`nav-item \$\{page === 'global-logs'[\s\S]*?<\/button>\s*/;
c = c.replace(glNavPattern, '');

// 3. Remove global-logs page JSX block completely
const glPagePattern = /\{page === 'global-logs' && \([\s\S]*?\)\}\s*/;
while (glPagePattern.test(c)) {
  c = c.replace(glPagePattern, '');
}

// 4. Remove useEffect for global-logs
const glEffectPattern = /useEffect\(\(\) => \{ if \(page === 'global-logs'\) \{ fetchLogs\(\); \} \}, \[page, fetchLogs\]\);\s*/;
c = c.replace(glEffectPattern, '');

// 5. Fix the broken SVG section - find and repair it
// Look for the pattern where map function is broken
const svgBrokenPattern = /\{points\.map\(p => \(\)\)\}/;
if (svgBrokenPattern.test(c)) {
  // Replace with correct map structure
  c = c.replace(svgBrokenPattern, `{points.map((p, i) => {`);
}

// 6. Remove ALL orphaned closing braces )} that appear without context
const lines = c.split('\n');
const result = [];
let braceDepth = 0;

for (let i = 0; i < lines.length; i++) {
  const line = lines[i];
  const trimmed = line.trim();
  
  // Track brace depth in JSX expressions
  if (trimmed.includes('${')) braceDepth += (trimmed.match(/\$\{/g) || []).length;
  if (trimmed.includes('}')) braceDepth -= (trimmed.match(/\}/g) || []).length;
  
  // Skip lines that are just )} with negative or imbalanced brace depth
  if (trimmed === ')}' && braceDepth <= 0) {
    continue;
  }
  
  result.push(line);
}

c = result.join('\n');

// 7. Finally, apply the correct fetchLogs fix
const oldFetch = /const fetchLogs = useCallback\(async \(\) => \{[\s\S]*?proxyUrl[\s\S]*?\}, \[selectedAgent\]\);/;
const newFetch = `const fetchLogs = useCallback(async () => {
    setLogsLoading(true);
    try {
      let url = \`\${API}/grafana/logs?limit=1000\`;
      if (selectedAgent) url += \`&agentId=\${selectedAgent}\`;
      const d = await fetchJSON<{items: LogEntry[], count: number}>(url);
      setLogs(d?.items || []);
    } catch {} finally { setLogsLoading(false); }
  }, [selectedAgent]);`;

c = c.replace(oldFetch, newFetch);

fs.writeFileSync(path, c, 'utf8');
console.log('RESTORED');
