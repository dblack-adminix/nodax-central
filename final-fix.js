const fs = require('fs');
const path = 'frontend/src/App.tsx';
let c = fs.readFileSync(path, 'utf8');

// Fix 1: Repair the broken SVG section around line 904
// The issue: {points.map... followed by orphaned JSX
// Find the pattern and fix it

const brokenSvg = /\{points\.map\(\(p, i\) => \{[\s\S]*?return <text[\s\S]*?text>\;\s*\}\)\}\s*\{points\.length > 0 && <path[\s\S]*?\}\)\}/;

const fixedSvg = `{points.map((p, i) => {
              const x = padding + i * stepX;
              return <text key={i} x={x} y={h - 2} textAnchor="middle" fontSize="6" fill="var(--text-muted)">{labels[i]}</text>;
            })}
            {points.length > 0 && <path d={areaPath} fill={\`url(#\${gradId})\`} />}
            {points.length > 0 && <path d={linePath} fill="none" stroke={color} strokeWidth="1" strokeLinejoin="round" strokeLinecap="round" />}
            {pts.map((p, i) => i === pts.length - 1 ? <circle key={i} cx={p.x} cy={p.y} r="2" fill={color} stroke="#fff" strokeWidth="0.8" /> : null)}`;

c = c.replace(brokenSvg, fixedSvg);

// Fix 2: Replace fetchLogs with centralized version
const oldFetch = /const fetchLogs = useCallback\(async \(\) => \{[\s\S]*?proxyUrl\(selectedAgent,[\s\S]*?\}, \[selectedAgent\]\);/;
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

// Fix 3: Update LogEntry interface
const oldInterface = /interface LogEntry \{[^}]+\}/;
const newInterface = `interface LogEntry { ID: number; Timestamp: string; Type: string; TargetVM: string; Status: string; Message: string; agentName?: string; }`;
c = c.replace(oldInterface, newInterface);

fs.writeFileSync(path, c, 'utf8');
console.log('Applied fixes');
