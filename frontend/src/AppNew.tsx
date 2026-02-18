import { useCallback, useEffect, useMemo, useState } from 'react'

interface Agent {
  id: string
  name: string
  status: string
  lastSeen?: string
}

interface LogEntry {
  ID: number
  Timestamp: string
  Type: string
  TargetVM: string
  Status: string
  Message: string
  agentName?: string
}

interface AuthUser { username: string; role: string }
interface AuthState { token: string; user: AuthUser }
type Page = 'overview' | 'logs'

const API = '/api'
const AUTH_KEY = 'nodax_auth'

function getAuthToken(): string | null {
  try {
    const a = JSON.parse(localStorage.getItem(AUTH_KEY) || '')
    return a.token || null
  } catch {
    return null
  }
}

function getAuthHeaders(): Record<string, string> {
  const t = getAuthToken()
  return t ? { Authorization: `Bearer ${t}` } : {}
}

async function authFetch(url: string, opts?: RequestInit): Promise<Response> {
  const h = opts?.headers instanceof Headers ? Object.fromEntries(opts.headers.entries()) : (opts?.headers || {})
  const headers = { ...getAuthHeaders(), ...h }
  const res = await fetch(url, { ...opts, headers })
  if (res.status === 401) {
    localStorage.removeItem(AUTH_KEY)
    window.location.reload()
  }
  return res
}

async function fetchJSON<T>(url: string, opts?: RequestInit): Promise<T> {
  const res = await authFetch(url, opts)
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
  return res.json()
}

function fmtDate(ts: string): string {
  try {
    const d = new Date(ts)
    if (Number.isNaN(d.getTime())) return ts || '—'
    return d.toLocaleString('ru-RU')
  } catch {
    return ts || '—'
  }
}

function normalizeLog(raw: any, idx: number): LogEntry {
  return {
    ID: Number(raw?.ID ?? raw?.id ?? idx + 1),
    Timestamp: String(raw?.Timestamp ?? raw?.timestamp ?? ''),
    Type: String(raw?.Type ?? raw?.type ?? ''),
    TargetVM: String(raw?.TargetVM ?? raw?.targetVM ?? raw?.vm ?? ''),
    Status: String(raw?.Status ?? raw?.status ?? ''),
    Message: String(raw?.Message ?? raw?.message ?? raw?.line ?? ''),
    agentName: String(raw?.agentName ?? raw?.agent ?? ''),
  }
}

function LoginPage({ onAuth }: { onAuth: (auth: AuthState) => void }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!username || !password) {
      setError('Введите логин и пароль')
      return
    }
    setLoading(true)
    setError('')
    try {
      const res = await fetch(`${API}/auth/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password })
      })
      if (!res.ok) {
        setError('Ошибка авторизации')
        setLoading(false)
        return
      }
      const d = await res.json()
      const auth: AuthState = { token: d.token, user: { username: d.username, role: d.role } }
      localStorage.setItem(AUTH_KEY, JSON.stringify(auth))
      onAuth(auth)
    } catch {
      setError('Ошибка подключения')
    }
    setLoading(false)
  }

  return (
    <div style={{ maxWidth: 360, margin: '80px auto', padding: 16 }}>
      <h2>NODAX Central</h2>
      <form onSubmit={submit}>
        <input value={username} onChange={e => setUsername(e.target.value)} placeholder="Логин" style={{ width: '100%', marginBottom: 8 }} />
        <input value={password} onChange={e => setPassword(e.target.value)} placeholder="Пароль" type="password" style={{ width: '100%', marginBottom: 8 }} />
        {error && <div style={{ color: 'crimson', marginBottom: 8 }}>{error}</div>}
        <button type="submit" disabled={loading}>{loading ? 'Вход...' : 'Войти'}</button>
      </form>
    </div>
  )
}

function MainApp({ auth, onLogout }: { auth: AuthState; onLogout: () => void }) {
  const [page, setPage] = useState<Page>('overview')
  const [agents, setAgents] = useState<Agent[]>([])
  const [selectedAgent, setSelectedAgent] = useState<string>('')
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [logsLoading, setLogsLoading] = useState(false)

  const fetchAgents = useCallback(async () => {
    try {
      const d = await fetchJSON<Agent[]>(`${API}/agents`)
      setAgents(d || [])
    } catch {
      setAgents([])
    }
  }, [])

  const fetchLogs = useCallback(async () => {
    setLogsLoading(true)
    try {
      let url = `${API}/grafana/logs?limit=1000`
      if (selectedAgent) url += `&agentId=${selectedAgent}`
      const d = await fetchJSON<{ items: any[]; count: number }>(url)
      const items = (d?.items || []).map((x, i) => normalizeLog(x, i))
      setLogs(items)
    } catch {
      setLogs([])
    }
    setLogsLoading(false)
  }, [selectedAgent])

  useEffect(() => {
    fetchAgents()
    const i = setInterval(fetchAgents, 10000)
    return () => clearInterval(i)
  }, [fetchAgents])

  useEffect(() => {
    fetchLogs()
  }, [fetchLogs])

  const selectedName = useMemo(() => {
    if (!selectedAgent) return 'Все хосты'
    return agents.find(a => a.id === selectedAgent)?.name || selectedAgent
  }, [agents, selectedAgent])

  return (
    <div style={{ padding: 16 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 12 }}>
        <h2>NODAX Central — Логи</h2>
        <div>
          <span style={{ marginRight: 12 }}>{auth.user.username}</span>
          <button onClick={onLogout}>Выйти</button>
        </div>
      </div>

      <div style={{ display: 'flex', gap: 8, marginBottom: 12 }}>
        <button onClick={() => setPage('overview')} disabled={page === 'overview'}>Главная</button>
        <button onClick={() => setPage('logs')} disabled={page === 'logs'}>Логи</button>
      </div>

      {page === 'overview' && (
        <div>
          <h3>Центр</h3>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, minmax(180px, 1fr))', gap: 12, marginTop: 8 }}>
            <div style={{ border: '1px solid #ddd', borderRadius: 8, padding: 12 }}>
              <div style={{ opacity: 0.7 }}>Хостов</div>
              <div style={{ fontSize: 24, fontWeight: 700 }}>{agents.length}</div>
            </div>
            <div style={{ border: '1px solid #ddd', borderRadius: 8, padding: 12 }}>
              <div style={{ opacity: 0.7 }}>Онлайн</div>
              <div style={{ fontSize: 24, fontWeight: 700 }}>{agents.filter(a => a.status === 'online').length}</div>
            </div>
            <div style={{ border: '1px solid #ddd', borderRadius: 8, padding: 12 }}>
              <div style={{ opacity: 0.7 }}>Логов загружено</div>
              <div style={{ fontSize: 24, fontWeight: 700 }}>{logs.length}</div>
            </div>
          </div>
        </div>
      )}

      {page === 'logs' && (
        <>
          <div style={{ display: 'flex', gap: 8, marginBottom: 12, alignItems: 'center' }}>
            <label>Хост:</label>
            <select value={selectedAgent} onChange={e => setSelectedAgent(e.target.value)}>
              <option value="">Все</option>
              {agents.map(a => (
                <option key={a.id} value={a.id}>{a.name} ({a.status})</option>
              ))}
            </select>
            <button onClick={fetchLogs} disabled={logsLoading}>{logsLoading ? 'Загрузка...' : 'Обновить'}</button>
            <span>Источник: /api/grafana/logs</span>
          </div>

          <div style={{ marginBottom: 8, opacity: 0.8 }}>
            Показано: {logs.length} записей • Фильтр: {selectedName}
          </div>

          <table style={{ width: '100%', borderCollapse: 'collapse' }}>
            <thead>
              <tr>
                <th style={{ textAlign: 'left', borderBottom: '1px solid #ccc' }}>Время</th>
                <th style={{ textAlign: 'left', borderBottom: '1px solid #ccc' }}>Хост</th>
                <th style={{ textAlign: 'left', borderBottom: '1px solid #ccc' }}>Тип</th>
                <th style={{ textAlign: 'left', borderBottom: '1px solid #ccc' }}>VM</th>
                <th style={{ textAlign: 'left', borderBottom: '1px solid #ccc' }}>Статус</th>
                <th style={{ textAlign: 'left', borderBottom: '1px solid #ccc' }}>Сообщение</th>
              </tr>
            </thead>
            <tbody>
              {logs.map((l, i) => (
                <tr key={`${l.ID}-${i}`}>
                  <td style={{ borderBottom: '1px solid #eee' }}>{fmtDate(l.Timestamp)}</td>
                  <td style={{ borderBottom: '1px solid #eee' }}>{l.agentName || '—'}</td>
                  <td style={{ borderBottom: '1px solid #eee' }}>{l.Type || '—'}</td>
                  <td style={{ borderBottom: '1px solid #eee' }}>{l.TargetVM || '—'}</td>
                  <td style={{ borderBottom: '1px solid #eee' }}>{l.Status || '—'}</td>
                  <td style={{ borderBottom: '1px solid #eee' }}>{l.Message || '—'}</td>
                </tr>
              ))}
              {logs.length === 0 && (
                <tr><td colSpan={6} style={{ padding: 12 }}>Нет записей</td></tr>
              )}
            </tbody>
          </table>
        </>
      )}
    </div>
  )
}

export default function AppNew() {
  const [auth, setAuth] = useState<AuthState | null>(() => {
    try {
      const a = JSON.parse(localStorage.getItem(AUTH_KEY) || '')
      return a.token ? a : null
    } catch {
      return null
    }
  })

  if (!auth) return <LoginPage onAuth={setAuth} />

  return <MainApp auth={auth} onLogout={() => { localStorage.removeItem(AUTH_KEY); setAuth(null) }} />
}
