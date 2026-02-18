const fs = require('fs');
const path = 'frontend/src/App.tsx';
let c = fs.readFileSync(path, 'utf8');

// 1. Find and remove the badly inserted global-logs page block
// It's inside the central-settings block, we need to extract it and fix the structure
const csStart = c.indexOf("{page === 'central-settings'");
const csEnd = c.indexOf("{page === 'global-logs'");

console.log('csStart:', csStart, 'csEnd:', csEnd);

if (csEnd > csStart && csEnd < c.indexOf("</main>", csStart)) {
  // global-logs was inserted inside central-settings block, extract it
  const glBlockStart = csEnd;
  let depth = 1;
  let i = glBlockStart + 20;
  while (depth > 0 && i < c.length) {
    if (c[i] === '(') depth++;
    if (c[i] === ')') depth--;
    i++;
  }
  // Find the closing )} after the div
  const glBlockEnd = c.indexOf(')}', i) + 2;
  
  // Extract the global-logs block
  const glBlock = c.substring(glBlockStart, glBlockEnd);
  
  // Remove it from inside central-settings
  c = c.substring(0, glBlockStart) + c.substring(glBlockEnd);
  
  // 2. Now fix the central-settings structure - find its proper end
  const csProperStart = c.indexOf("{page === 'central-settings'");
  let csDepth = 1;
  let j = csProperStart + 25;
  while (csDepth > 0 && j < c.length) {
    if (c[j] === '(') csDepth++;
    if (c[j] === ')') csDepth--;
    j++;
  }
  const csProperEnd = c.indexOf(')}', j) + 2;
  
  // 3. Insert global-logs after central-settings
  c = c.substring(0, csProperEnd) + '\n\n' + glBlock + c.substring(csProperEnd);
}

// 4. Fix fetchLogs to use centralized endpoint
const oldFetch = /const fetchLogs = useCallback\(async \(\) => \{[\s\S]*?\}, \[selectedAgent\]\);/;
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

// 5. Update LogEntry interface to include agentName
const oldInterface = 'interface LogEntry { ID: number; Timestamp: string; Type: string; TargetVM: string; Status: string; Message: string; }';
const newInterface = 'interface LogEntry { ID: number; Timestamp: string; Type: string; TargetVM: string; Status: string; Message: string; agentName?: string; }';
c = c.replace(oldInterface, newInterface);

fs.writeFileSync(path, c, 'utf8');
console.log('Fixed!');
