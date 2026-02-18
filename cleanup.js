const fs = require('fs');
const path = 'frontend/src/App.tsx';
let c = fs.readFileSync(path, 'utf8');

// Fix 1: Remove the broken table fragments that got inserted into useEffect
const broken = '</td></tr></tbody></table></div></div></div>';
const broken2 = "{logsLoading ? 'Загрузка...' : 'Нет записей'}</td></tr>";
const broken3 = '</tbody></table></div></div>)}';
const broken4 = "{filteredLogs.length === 0 && <tr><td colSpan={6} className=\"empty-state\">";
const broken5 = '<td className="col-mono">{fmtDate(l.Timestamp)}</td>';

// Find and remove lines that contain these fragments outside of proper JSX
let lines = c.split('\n');
lines = lines.filter((line, idx) => {
    // Remove lines with table fragments that are outside the render context
    if (idx >= 600 && idx <= 610) {
        if (line.includes('</td>') && !line.includes('<td')) return false;
        if (line.includes('log-type type-info')) return false;
    }
    return true;
});
c = lines.join('\n');

// Fix 2: Ensure fetchLogs uses centralized endpoint
const oldFetch = /const fetchLogs = useCallback\(async \(\) => \{[\s\S]*?proxyUrl\(selectedAgent, ['"]\/api\/v1\/logs[\s\S]*?\}, \[selectedAgent\]\);/;
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

// Fix 3: Remove global-logs useEffect if present
const glEffect = /useEffect\(\(\) => \{ if \(page === 'global-logs'\) \{ fetchLogs\(\); \} \}, \[page, fetchLogs\]\);/;
c = c.replace(glEffect, '');

// Fix 4: Remove broken global-logs nav button
const glNav = /<button className=\{`nav-item \$\{page === 'global-logs'[\s\S]*?<\/button>/;
c = c.replace(glNav, '');

// Fix 5: Remove global-logs page JSX
const glPage = /\{page === 'global-logs' && \([\s\S]*?\)\}\s*/;
c = c.replace(glPage, '');

fs.writeFileSync(path, c, 'utf8');
console.log('Cleaned up broken code');
