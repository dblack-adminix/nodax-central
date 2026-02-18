const fs = require('fs');
const path = 'frontend/src/App.tsx';
let c = fs.readFileSync(path, 'utf8');

// Find the broken SVG section and replace with minimal working version
// Look for the pattern: {points.map... followed by orphaned <path elements

const brokenSvgPattern = /\{points\.map\(\(p, i\) => \{[\s\S]*?\}\)\}\s*\{points\.length > 0 && <path d=\{areaPath\}[\s\S]*?<\/svg>/;

const workingSvg = `{points.map((p, i) => {
              const x = padding + i * stepX;
              return <text key={i} x={x} y={h - 2} textAnchor="middle" fontSize="6" fill="var(--text-muted)">{labels[i]}</text>;
            })}
            {points.length > 0 && <path d={areaPath} fill={\`url(#\${gradId})\`} />}
            {points.length > 0 && <path d={linePath} fill="none" stroke={color} strokeWidth="1" strokeLinejoin="round" strokeLinecap="round" />}
            {pts.map((p, i) => i === pts.length - 1 ? <circle key={i} cx={p.x} cy={p.y} r="2" fill={color} stroke="#fff" strokeWidth="0.8" /> : null)}
          </svg>`;

c = c.replace(brokenSvgPattern, workingSvg);

fs.writeFileSync(path, c, 'utf8');
console.log('SVG fixed');
