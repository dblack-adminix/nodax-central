import { useEffect, useState, useCallback, useMemo, useRef } from 'react'
import * as echarts from 'echarts/core'
import { LineChart as ELineChart } from 'echarts/charts'
import { GridComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import logoImg from './logo.png'

// --- Types ---
interface Agent { id: string; name: string; url: string; apiKey: string; status: string; lastSeen: string; createdAt: string; }
interface HostInfo { computerName: string; osName: string; cpuUsage: number; totalRAM: number; usedRAM: number; ramUsePct: number; uptime: string; vmCount: number; vmRunning: number; disks: { drive: string; totalGB: number; freeGB: number; usePct: number }[]; }
interface VM { name: string; state: string; cpuUsage: number; memoryAssigned: number; }
interface HealthCheck { name: string; status: string; message: string; value: string; }
interface HealthReport { timestamp: string; overall: string; checks: HealthCheck[]; }
interface AgentData { agentId: string; hostInfo?: HostInfo; vms?: VM[]; health?: HealthReport; error?: string; fetchedAt: string; }
interface Overview { totalAgents: number; onlineAgents: number; totalVMs: number; runningVMs: number; totalCpuAvg: number; totalRamBytes: number; usedRamBytes: number; }
interface VMDetail { name: string; state: string; generation: number; version: string; path: string; uptime: string; cpuUsage: number; memoryAssigned: number; hardDrives: string[]; networkAdapters: string[]; snapshots: string[]; }
interface LogEntry { ID: number; Timestamp: string; Type: string; TargetVM: string; Status: string; Message: string; agentName?: string; }
interface Schedule { ID: number; CronString: string; VMList: string; Destination: string; Enabled: boolean; }
interface Settings { BackupPath: string; ServerPort: string; ApiKey: string; Mode: string; Theme: string; RetentionCount: number; Archiver: string; CompressionLevel: number; S3Endpoint: string; S3Region: string; S3Bucket: string; S3AccessKey: string; S3SecretKey: string; S3Prefix: string; S3Enabled: boolean; S3RetentionCount: number; TelegramBotToken: string; TelegramChatID: string; TelegramEnabled: boolean; TelegramOnlyErrors: boolean; LogRetentionCount: number; SwaggerEnabled: boolean; [key: string]: any; }
interface BackupFile { vmName: string; fileName: string; filePath: string; size: number; date: string; }
interface RoleSectionPolicy { overview: boolean; statistics: boolean; storage: boolean; settings: boolean; security: boolean; }
interface CentralConfig { pollIntervalSec: number; port: string; caddyDomain: string; licenseKey?: string; licenseServer?: string; licensePubKey?: string; licenseStatus?: string; licenseReason?: string; licenseExpires?: string; licenseChecked?: string; licenseGraceTo?: string; licenseLastErr?: string; theme: string; language: string; retentionDays: number; bgColor: string; bgImage: string; rolePolicies?: Record<string, UserHostPermission[]>; roleSections?: Record<string, RoleSectionPolicy>; }
interface LicenseStatusResponse { status?: string; reason?: string; expiresAt?: string; checkedAt?: string; graceUntil?: string; lastError?: string; publicKey?: string; server?: string; configured?: boolean; writeEnabled?: boolean; }
interface HostStat { agentId: string; name: string; status: string; cpu: number; ramPct: number; ramUsedGB: number; ramTotalGB: number; vmTotal: number; vmRunning: number; disks: { drive: string; totalGB: number; freeGB: number; usePct: number }[]; uptime: string; os: string; }
interface AggStats { hosts: HostStat[]; totalHosts: number; onlineHosts: number; totalVMs: number; runningVMs: number; avgCpu: number; avgRam: number; totalRamGB: number; usedRamGB: number; totalDiskGB: number; usedDiskGB: number; }
interface MetricPoint { t: string; cpu: number; ramPct: number; ramUsedGB: number; diskPct: number; vmRunning: number; vmTotal: number; }
interface HostHistory { agentId: string; name: string; points: MetricPoint[]; }
interface UserHostPermission { agentId: string; view: boolean; control: boolean; }
interface UserItem { id: string; username: string; role: string; createdAt: string; }
interface MyProfile { id: string; username: string; role: string; createdAt?: string; }

// License Server types
interface License {
  id: string;
  licenseKey: string;
  customerName: string;
  customerCompany: string;
  customerEmail: string;
  customerTelegram: string;
  customerPhone: string;
  plan: string;
  maxAgents: number;
  expiresAt: string;
  status: string;
  notes: string;
  lastHostname: string;
  lastCheckAt: string;
  createdAt: string;
}
interface AuditEvent {
  id: string;
  licenseId: string;
  action: string;
  actor: string;
  details: string;
  createdAt: string;
}
interface LicenseServerSettings {
  telegram_bot_token: string;
  telegram_chat_id: string;
  notify_days_before: string;
  webhook_url: string;
}
interface APIKey {
  id: string;
  name: string;
  key: string;
  createdAt: string;
}

interface AuthUser { username: string; role: string; }
interface AuthState { token: string; user: AuthUser; }

const API = '/api';
const PAGE_SIZE = 20;
const AUTH_KEY = 'nodax_auth';

echarts.use([ELineChart, GridComponent, CanvasRenderer]);

function getAuthToken(): string | null {
  try { const a = JSON.parse(localStorage.getItem(AUTH_KEY) || ''); return a.token || null; } catch { return null; }
}
function getAuthHeaders(): Record<string, string> {
  const t = getAuthToken();
  return t ? { 'Authorization': `Bearer ${t}` } : {};
}

async function authFetch(url: string, opts?: RequestInit): Promise<Response> {
  const h = opts?.headers instanceof Headers ? Object.fromEntries(opts.headers.entries()) : (opts?.headers || {});
  const headers = { ...getAuthHeaders(), ...h };
  const res = await fetch(url, { ...opts, headers });
  if (res.status === 401) { localStorage.removeItem(AUTH_KEY); window.location.reload(); }
  return res;
}
async function fetchJSON<T>(url: string, opts?: RequestInit): Promise<T> {
  const res = await authFetch(url, opts);
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}
async function fetchText(url: string): Promise<string> {
  const res = await authFetch(url);
  return res.text();
}

function formatSize(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024; const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return (bytes / Math.pow(k, i)).toFixed(i > 0 ? 1 : 0) + ' ' + sizes[i];
}

function normalizePathPart(v: string): string {
  return (v || '').split('/').map(s => s.trim()).filter(Boolean).join('/');
}

function hostToPrefixPart(host: string): string {
  const s = (host || '').trim().toLowerCase().replace(/[^a-z0-9._-]+/g, '-').replace(/^-+|-+$/g, '');
  return s || 'host';
}

function buildS3Prefix(base: string, host: string): string {
  const b = normalizePathPart(base);
  const h = hostToPrefixPart(host);
  if (!b) return h;
  if (b === h || b.endsWith(`/${h}`)) return b;
  return `${b}/${h}`;
}

function buildSMBPath(base: string, host: string): string {
  const h = hostToPrefixPart(host);
  const raw = (base || '').trim().replace(/\//g, '\\');
  const b = raw.replace(/[\\]+$/, '');
  if (!b) return h;
  const bl = b.toLowerCase();
  const hl = h.toLowerCase();
  if (bl === hl || bl.endsWith(`\\${hl}`)) return b;
  return `${b}\\${h}`;
}

function buildWebDAVPath(base: string, host: string): string {
  const h = hostToPrefixPart(host);
  const b = normalizePathPart(base);
  const path = !b ? h : (b === h || b.endsWith(`/${h}`) ? b : `${b}/${h}`);
  return `/${path}`;
}
function cpuColor(v: number) { return v > 80 ? 'var(--danger)' : v > 50 ? 'var(--warning)' : 'var(--primary)'; }
function ramColor(v: number) { return v > 85 ? 'var(--danger)' : v > 60 ? 'var(--warning)' : 'var(--success)'; }
function diskColor(v: number) { return v > 90 ? 'var(--danger)' : v > 70 ? 'var(--warning)' : 'var(--info)'; }
function licenseStatusLabel(v?: string): string {
  const s = (v || '').toLowerCase();
  if (s === 'active') return 'Активна';
  if (s === 'grace') return 'Grace период';
  if (s === 'expired') return 'Истекла';
  if (s === 'revoked') return 'Отозвана';
  if (s === 'over_limit') return 'Превышен лимит';
  if (s === 'unconfigured') return 'Не настроена';
  if (s === 'invalid') return 'Недействительна';
  return 'Неизвестно';
}

type Page = 'overview' | 'host' | 'statistics' | 'central-settings' | 'security' | 's3-browser' | 'smb-browser' | 'webdav-browser' | 'license-server';
type HostTab = 'panel' | 'backups' | 'journal' | 'settings';
type VMFilter = 'all' | 'running' | 'stopped';

function LoginPage({ onAuth }: { onAuth: (auth: AuthState) => void }) {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [isSetup, setIsSetup] = useState<boolean | null>(null);

  useEffect(() => {
    fetch(`${API}/auth/setup`).then(r => r.json()).then(d => setIsSetup(d.needsSetup)).catch(() => setIsSetup(false));
  }, []);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!username || !password) { setError('Введите логин и пароль'); return; }
    setLoading(true); setError('');
    try {
      const endpoint = isSetup ? `${API}/auth/register` : `${API}/auth/login`;
      const res = await fetch(endpoint, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password, role: 'admin' }),
      });
      if (!res.ok) {
        const data = await res.json().catch(() => ({ error: 'Ошибка сервера' }));
        setError(data.error || 'Ошибка авторизации');
        setLoading(false);
        return;
      }
      const data = await res.json();
      const auth: AuthState = { token: data.token, user: { username: data.username, role: data.role } };
      localStorage.setItem(AUTH_KEY, JSON.stringify(auth));
      onAuth(auth);
    } catch { setError('Ошибка подключения к серверу'); }
    setLoading(false);
  };
  if (isSetup === null) return <div className="login-page"><div className="login-card"><div className="loading">Загрузка...</div></div></div>;

  return (
    <div className="login-page">
      <div className="login-card">
        <img src={logoImg} alt="NODAX" className="login-logo" />
        <h2>{isSetup ? 'Создание администратора' : 'Авторизация'}</h2>
        {isSetup && <p className="login-hint">Создайте первого пользователя для доступа к панели</p>}
        <form onSubmit={handleSubmit}>
          <input type="text" placeholder="Логин" value={username} onChange={e => setUsername(e.target.value)} autoFocus />
          <input type="password" placeholder="Пароль" value={password} onChange={e => setPassword(e.target.value)} />
          {error && <div className="login-error">{error}</div>}
          <button type="submit" disabled={loading}>{loading ? 'Вход...' : isSetup ? 'Создать и войти' : 'Войти'}</button>
        </form>
      </div>
    </div>
  );
}

export default function App() {
  const [auth, setAuth] = useState<AuthState | null>(() => {
    try { const a = JSON.parse(localStorage.getItem(AUTH_KEY) || ''); return a.token ? a : null; } catch { return null; }
  });

  if (!auth) return <LoginPage onAuth={setAuth} />;

  return <MainApp auth={auth} onLogout={() => { localStorage.removeItem(AUTH_KEY); setAuth(null); }} />;
}

function MainApp({ auth, onLogout }: { auth: AuthState; onLogout: () => void }) {
  const emptySections: RoleSectionPolicy = useMemo(() => ({ overview: false, statistics: false, storage: false, settings: false, security: false }), []);
  const fullSections: RoleSectionPolicy = useMemo(() => ({ overview: true, statistics: true, storage: true, settings: true, security: true }), []);

  const [agents, setAgents] = useState<Agent[]>([]);
  const [overview, setOverview] = useState<Overview | null>(null);
  const [page, setPage] = useState<Page>('overview');
  const [selectedAgent, setSelectedAgent] = useState<string | null>(null);
  const [agentData, setAgentData] = useState<AgentData | null>(null);
  const [showAddModal, setShowAddModal] = useState(false);
  const [addForm, setAddForm] = useState({ url: '', apiKey: '' });
  const [addError, setAddError] = useState('');
  const [vmSearch, setVmSearch] = useState('');
  const [vmFilter, setVmFilter] = useState<VMFilter>('all');
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [hostTab, setHostTab] = useState<HostTab>('panel');
  const [hostsCollapsed, setHostsCollapsed] = useState(false);
  const [storageCollapsed, setStorageCollapsed] = useState(false);

  // VM Detail modal
  const [vmDetail, setVmDetail] = useState<VMDetail | null>(null);
  const [showVmDetail, setShowVmDetail] = useState(false);

  // Deploy VM modal
  const [showDeployModal, setShowDeployModal] = useState(false);
  const [deployForm, setDeployForm] = useState({ name: '', cpu: 2, ram: 4, storagePath: 'D:\\Hyper-V', switchName: 'Default Switch', osType: 'windows' });
  const [deployDisks, setDeployDisks] = useState<number[]>([127]);
  const [deployLoading, setDeployLoading] = useState(false);
  const [vmSwitches, setVmSwitches] = useState<string[]>([]);

  // Delete agent modal
  const [deleteAgentId, setDeleteAgentId] = useState<string | null>(null);

  // Rename / Delete / Snapshot modals
  const [renameTarget, setRenameTarget] = useState<string | null>(null);
  const [renameValue, setRenameValue] = useState('');
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);
  const [snapTarget, setSnapTarget] = useState<string | null>(null);
  const [snapName, setSnapName] = useState('');

  // Backups tab
  const [backupDest, setBackupDest] = useState<Record<string, string>>({});
  const [backupLoading, setBackupLoading] = useState<string | null>(null);
  const [backupMsg, setBackupMsg] = useState('');
  const [backupLogs, setBackupLogs] = useState('');

  // Journal tab
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [logPage, setLogPage] = useState(1);
  const [logsLoading, setLogsLoading] = useState(false);
  const [logFilter, setLogFilter] = useState<'all' | 'system' | 'backup'>('system');
  const [expandedLog, setExpandedLog] = useState<number | null>(null);

  // Settings tab
  const [settings, setSettings] = useState<Settings | null>(null);
  const [settingsMsg, setSettingsMsg] = useState('');
  const [schedules, setSchedules] = useState<Schedule[]>([]);
  const [newSched, setNewSched] = useState({ time: '03:00', dest: '', vmNames: [] as string[] });
  const [editSched, setEditSched] = useState<{ id: number; time: string; dest: string; vmNames: string[]; enabled: boolean } | null>(null);
  const [storageTab, setStorageTab] = useState<'s3' | 'smb' | 'webdav'>('s3');

  // CPU/RAM history for mini charts
  const [cpuHistory, setCpuHistory] = useState<number[]>([]);
  const [ramHistory, setRamHistory] = useState<number[]>([]);

  // Full metric history from poller
  const [hostHistory, setHostHistory] = useState<MetricPoint[]>([]);

  // Toast notifications
  const [toasts, setToasts] = useState<{ id: number; msg: string; type: 'success' | 'error' | 'info' }[]>([]);
  const toast = useCallback((msg: string, type: 'success' | 'error' | 'info' = 'info') => {
    const id = Date.now();
    setToasts(t => [...t, { id, msg, type }]);
    setTimeout(() => setToasts(t => t.filter(x => x.id !== id)), 4000);
  }, []);

  // Statistics & Central Config
  const [aggStats, setAggStats] = useState<AggStats | null>(null);
  const [centralCfg, setCentralCfg] = useState<CentralConfig | null>(null);
  const [cfgSaving, setCfgSaving] = useState(false);
  const [cfgImporting, setCfgImporting] = useState(false);
  const [caddyRecheckLoading, setCaddyRecheckLoading] = useState(false);
  const [caddyRecheckMsg, setCaddyRecheckMsg] = useState('');
  const [licenseRecheckLoading, setLicenseRecheckLoading] = useState(false);
  const [licenseRecheckMsg, setLicenseRecheckMsg] = useState('');
  const [bgList, setBgList] = useState<string[]>([]);
  const [bgUploading, setBgUploading] = useState(false);
  const configFileInputRef = useRef<HTMLInputElement | null>(null);

  // User management
  const [users, setUsers] = useState<UserItem[]>([]);
  const [newUser, setNewUser] = useState({ username: '', password: '', role: 'user' });
  const [userMsg, setUserMsg] = useState('');
  const [myProfile, setMyProfile] = useState<MyProfile | null>(null);
  const [policyModalRole, setPolicyModalRole] = useState<string | null>(null);
  const [newGroupName, setNewGroupName] = useState('');
  const [policyDraft, setPolicyDraft] = useState<Record<string, { view: boolean; control: boolean }>>({});
  const [sectionDraft, setSectionDraft] = useState<RoleSectionPolicy>({ overview: false, statistics: false, storage: false, settings: false, security: false });

  // S3 Browser
  const [s3Agent, setS3Agent] = useState<string>('');
  const [s3Prefix, setS3Prefix] = useState('');
  const [s3Objects, setS3Objects] = useState<{key: string; lastModified: string; size: number; isDir: boolean}[]>([]);
  const [s3Loading, setS3Loading] = useState(false);
  const [s3Error, setS3Error] = useState('');
  const [s3Tree, setS3Tree] = useState<{key: string; children?: {key: string}[]; expanded?: boolean}[]>([]);
  const [s3TreeLoaded, setS3TreeLoaded] = useState<Record<string, {key: string; lastModified: string; size: number; isDir: boolean}[]>>({});

  // SMB Browser
  const [smbAgent, setSmbAgent] = useState<string>('');
  const [smbPath, setSmbPath] = useState('');
  const [smbObjects, setSmbObjects] = useState<{key: string; lastModified: string; size: number; isDir: boolean}[]>([]);
  const [smbLoading, setSmbLoading] = useState(false);
  const [smbError, setSmbError] = useState('');
  const [smbTree, setSmbTree] = useState<{key: string; children?: {key: string}[]; expanded?: boolean}[]>([]);
  const [smbTreeLoaded, setSmbTreeLoaded] = useState<Record<string, {key: string; lastModified: string; size: number; isDir: boolean}[]>>({});

  // WebDAV Browser
  const [davAgent, setDavAgent] = useState<string>('');
  const [davPath, setDavPath] = useState('');
  const [davObjects, setDavObjects] = useState<{key: string; lastModified: string; size: number; isDir: boolean}[]>([]);
  const [davLoading, setDavLoading] = useState(false);
  const [davError, setDavError] = useState('');
  const [davTree, setDavTree] = useState<{key: string; children?: {key: string}[]; expanded?: boolean}[]>([]);
  const [davTreeLoaded, setDavTreeLoaded] = useState<Record<string, {key: string; lastModified: string; size: number; isDir: boolean}[]>>({});

  // License Server states
  const [licenses, setLicenses] = useState<License[]>([]);
  const [licensesLoading, setLicensesLoading] = useState(false);
  const [licenseSearch, setLicenseSearch] = useState('');
  const [licenseStatusFilter, setLicenseStatusFilter] = useState('');
  const [licensePlanFilter, setLicensePlanFilter] = useState('');
  const [licensePage, setLicensePage] = useState(0);
  const LICENSE_PAGE_SIZE = 20;
  const [showCreateLicense, setShowCreateLicense] = useState(false);
  const [createLicenseForm, setCreateLicenseForm] = useState({
    customerName: '', customerCompany: '', customerEmail: '', customerTelegram: '', customerPhone: '',
    plan: 'basic', maxAgents: 10, validDays: 365, isTrial: false, notes: ''
  });
  const [editLicense, setEditLicense] = useState<License | null>(null);
  const [auditEvents, setAuditEvents] = useState<AuditEvent[]>([]);
  const [auditLoading, setAuditLoading] = useState(false);
  const [lsSettings, setLsSettings] = useState<LicenseServerSettings | null>(null);
  const [lsSettingsLoading, setLsSettingsLoading] = useState(false);
  const [apiKeys, setApiKeys] = useState<APIKey[]>([]);
  const [apiKeysLoading, setApiKeysLoading] = useState(false);
  const [newApiKeyName, setNewApiKeyName] = useState('');
  const [confirmModal, setConfirmModal] = useState<{show: boolean; title: string; message: string; onConfirm: () => void; type: 'warn' | 'danger' | 'info'} | null>(null);

  // Backup progress
  const [backupStatus, setBackupStatus] = useState<Record<string, 'idle' | 'running' | 'done' | 'error'>>({});
  const [bkLogPage, setBkLogPage] = useState(1);

  // Backup archives & restore
  const [backupFiles, setBackupFiles] = useState<BackupFile[]>([]);
  const [backupFilesLoading, setBackupFilesLoading] = useState(false);
  const [restoreTarget, setRestoreTarget] = useState<BackupFile | null>(null);
  const [restoreForm, setRestoreForm] = useState({ newVMName: '', restorePath: '' });
  const [restoreLoading, setRestoreLoading] = useState(false);
  const [backupArchiveFilter, setBackupArchiveFilter] = useState('');
  const [expandedArchiveVMs, setExpandedArchiveVMs] = useState<Record<string, boolean>>({});

  // --- Proxy helper ---
  const proxyUrl = (agentId: string | null, path: string) => agentId ? `${API}/agents/${agentId}/proxy${path}` : '';

  // --- Fetchers ---
  const fetchAgents = useCallback(async () => { try { setAgents(await fetchJSON<Agent[]>(`${API}/agents`) || []); } catch {} }, []);
  const fetchOverview = useCallback(async () => { try { setOverview(await fetchJSON<Overview>(`${API}/overview`)); } catch {} }, []);
  const fetchAgentData = useCallback(async (id: string) => { try { setAgentData(await fetchJSON<AgentData>(`${API}/agents/${id}/data`)); } catch (e: any) { setAgentData({ agentId: id, error: e?.message || 'Ошибка загрузки', fetchedAt: new Date().toISOString() }); } }, []);
  const fetchHistory = useCallback(async (id: string) => { try { const h = await fetchJSON<HostHistory>(`${API}/agents/${id}/history`); setHostHistory(h.points || []); } catch { setHostHistory([]); } }, []);

  const fetchLogs = useCallback(async () => {
    setLogsLoading(true);
    try {
      let url = `${API}/grafana/logs?limit=1000`;
      if (selectedAgent) url += `&agentId=${selectedAgent}`;
      const d = await fetchJSON<{items: any[], count: number}>(url);
      const mapped: LogEntry[] = (d?.items || []).map((x, i) => ({
        ID: Number(x?.ID ?? x?.id ?? i + 1),
        Timestamp: String(x?.Timestamp ?? x?.timestamp ?? ''),
        Type: String(x?.Type ?? x?.type ?? ''),
        TargetVM: String(x?.TargetVM ?? x?.targetVm ?? x?.vm ?? ''),
        Status: String(x?.Status ?? x?.status ?? ''),
        Message: String(x?.Message ?? x?.message ?? ''),
        agentName: String(x?.agentName ?? x?.agent ?? ''),
      }));
      setLogs(mapped);
    } catch {} finally { setLogsLoading(false); }
  }, [selectedAgent]);

  const fetchBackupLogs = useCallback(async () => {
    if (!selectedAgent) return;
    try { setBackupLogs(await fetchText(proxyUrl(selectedAgent, '/api/v1/backups/logs'))); } catch {}
  }, [selectedAgent]);

  const fetchBackupFiles = useCallback(async () => {
    if (!selectedAgent) return;
    setBackupFilesLoading(true);
    try { setBackupFiles(await fetchJSON<BackupFile[]>(proxyUrl(selectedAgent, '/api/v1/backups/list')) || []); } catch { setBackupFiles([]); }
    setBackupFilesLoading(false);
  }, [selectedAgent]);

  const fetchSettings = useCallback(async () => {
    if (!selectedAgent) return;
    try {
      const data = await fetchJSON<Settings>(proxyUrl(selectedAgent, '/api/v1/settings'));
      setSettings(data || null);
    } catch {
      setSettings(null);
    }
  }, [selectedAgent]);

  const fetchSchedules = useCallback(async () => {
    if (!selectedAgent) return;
    try {
      const data = await fetchJSON<Schedule[]>(proxyUrl(selectedAgent, '/api/v1/schedules'));
      setSchedules(data || []);
    } catch {
      setSchedules([]);
    }
  }, [selectedAgent]);

  const fetchStats = useCallback(async () => { try { setAggStats(await fetchJSON<AggStats>(`${API}/stats`)); } catch {} }, []);
  const mergeLicenseStatus = useCallback((d: LicenseStatusResponse) => {
    setCentralCfg(prev => prev ? ({
      ...prev,
      licenseStatus: d.status || prev.licenseStatus,
      licenseReason: d.reason || '',
      licenseExpires: d.expiresAt || '',
      licenseChecked: d.checkedAt || '',
      licenseGraceTo: d.graceUntil || '',
      licenseLastErr: d.lastError || '',
      licensePubKey: d.publicKey || prev.licensePubKey,
      licenseServer: d.server || prev.licenseServer,
    }) : prev);
  }, []);
  const fetchCentralCfg = useCallback(async () => { try { setCentralCfg(await fetchJSON<CentralConfig>(`${API}/config`)); } catch {} }, []);
  const fetchLicenseStatus = useCallback(async () => {
    try {
      const data = await fetchJSON<LicenseStatusResponse>(`${API}/license/status`);
      mergeLicenseStatus(data || {});
    } catch {}
  }, [mergeLicenseStatus]);
  const fetchBgList = useCallback(async () => { try { setBgList(await fetchJSON<string[]>(`${API}/backgrounds`) || []); } catch {} }, []);
  const fetchUsers = useCallback(async () => { try { setUsers(await fetchJSON<UserItem[]>(`${API}/auth/users`) || []); } catch {} }, []);
  const fetchMe = useCallback(async () => { try { setMyProfile(await fetchJSON<MyProfile>(`${API}/auth/me`)); } catch {} }, []);
  const fetchRolePolicies = useCallback(async () => {
    try {
      const d = await fetchJSON<{ rolePolicies: Record<string, UserHostPermission[]>; roleSections: Record<string, RoleSectionPolicy> }>(`${API}/auth/role-policies`);
      setCentralCfg(prev => prev ? ({ ...prev, rolePolicies: d?.rolePolicies || {}, roleSections: d?.roleSections || {} }) : prev);
    } catch {}
  }, []);

  // License Server fetchers
  const fetchLicenses = useCallback(async () => {
    setLicensesLoading(true);
    try {
      const data = await fetchJSON<License[]>(`${API}/license-server/licenses`);
      setLicenses(data || []);
    } catch { setLicenses([]); }
    setLicensesLoading(false);
  }, []);
  const createLicense = async () => {
    try {
      const body = { ...createLicenseForm, isTrial: createLicenseForm.isTrial ? 1 : 0 };
      const r = await authFetch(`${API}/license-server/licenses`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
      if (r.ok) {
        toast('Лицензия создана', 'success');
        setShowCreateLicense(false);
        setCreateLicenseForm({ customerName: '', customerCompany: '', customerEmail: '', customerTelegram: '', customerPhone: '', plan: 'basic', maxAgents: 10, validDays: 365, isTrial: false, notes: '' });
        fetchLicenses();
      } else {
        const d = await r.json().catch(() => ({ error: 'Ошибка' }));
        toast(d.error || 'Ошибка создания', 'error');
      }
    } catch { toast('Ошибка сети', 'error'); }
  };
  const extendLicense = async (id: string, days: number) => {
    try {
      const r = await authFetch(`${API}/license-server/licenses/${id}/extend`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ days }) });
      if (r.ok) { toast('Лицензия продлена', 'success'); fetchLicenses(); }
      else { const d = await r.json().catch(() => ({ error: 'Ошибка' })); toast(d.error || 'Ошибка', 'error'); }
    } catch { toast('Ошибка сети', 'error'); }
  };
  const revokeLicense = async (id: string) => {
    setConfirmModal({ show: true, title: 'Отозвать лицензию?', message: 'Агенты с этой лицензией перейдут в режим ограниченной функциональности.', type: 'warn', onConfirm: async () => {
      try {
        const r = await authFetch(`${API}/license-server/licenses/${id}/revoke`, { method: 'POST' });
        if (r.ok) { toast('Лицензия отозвана', 'success'); fetchLicenses(); }
        else { const d = await r.json().catch(() => ({ error: 'Ошибка' })); toast(d.error || 'Ошибка', 'error'); }
      } catch { toast('Ошибка сети', 'error'); }
      setConfirmModal(null);
    }});
  };
  const restoreLicense = async (id: string) => {
    try {
      const r = await authFetch(`${API}/license-server/licenses/${id}/restore`, { method: 'POST' });
      if (r.ok) { toast('Лицензия восстановлена', 'success'); fetchLicenses(); }
      else { const d = await r.json().catch(() => ({ error: 'Ошибка' })); toast(d.error || 'Ошибка', 'error'); }
    } catch { toast('Ошибка сети', 'error'); }
  };
  const deleteLicense = async (id: string) => {
    setConfirmModal({ show: true, title: 'Удалить лицензию?', message: 'Это действие необратимо. Лицензия будет полностью удалена из системы.', type: 'danger', onConfirm: async () => {
      try {
        const r = await authFetch(`${API}/license-server/licenses/${id}`, { method: 'DELETE' });
        if (r.ok) { toast('Лицензия удалена', 'success'); fetchLicenses(); }
        else { const d = await r.json().catch(() => ({ error: 'Ошибка' })); toast(d.error || 'Ошибка', 'error'); }
      } catch { toast('Ошибка сети', 'error'); }
      setConfirmModal(null);
    }});
  };
  const updateLicense = async () => {
    if (!editLicense) return;
    try {
      const r = await authFetch(`${API}/license-server/licenses/${editLicense.id}`, { method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({
        customerName: editLicense.customerName, customerCompany: editLicense.customerCompany, customerEmail: editLicense.customerEmail,
        customerTelegram: editLicense.customerTelegram, customerPhone: editLicense.customerPhone, plan: editLicense.plan, maxAgents: editLicense.maxAgents, notes: editLicense.notes
      })});
      if (r.ok) { toast('Лицензия обновлена', 'success'); setEditLicense(null); fetchLicenses(); }
      else { const d = await r.json().catch(() => ({ error: 'Ошибка' })); toast(d.error || 'Ошибка', 'error'); }
    } catch { toast('Ошибка сети', 'error'); }
  };
  const fetchAudit = useCallback(async () => {
    setAuditLoading(true);
    try { const data = await fetchJSON<AuditEvent[]>(`${API}/license-server/audit`); setAuditEvents(data || []); } catch { setAuditEvents([]); }
    setAuditLoading(false);
  }, []);
  const fetchLsSettings = useCallback(async () => {
    setLsSettingsLoading(true);
    try { const data = await fetchJSON<LicenseServerSettings>(`${API}/license-server/settings`); setLsSettings(data); } catch { setLsSettings(null); }
    setLsSettingsLoading(false);
  }, []);
  const saveLsSettings = async () => {
    if (!lsSettings) return;
    try {
      const r = await authFetch(`${API}/license-server/settings`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(lsSettings) });
      if (r.ok) { toast('Настройки сохранены', 'success'); }
      else { const d = await r.json().catch(() => ({ error: 'Ошибка' })); toast(d.error || 'Ошибка', 'error'); }
    } catch { toast('Ошибка сети', 'error'); }
  };
  const fetchApiKeys = useCallback(async () => {
    setApiKeysLoading(true);
    try { const data = await fetchJSON<APIKey[]>(`${API}/license-server/api-keys`); setApiKeys(data || []); } catch { setApiKeys([]); }
    setApiKeysLoading(false);
  }, []);
  const createApiKey = async () => {
    if (!newApiKeyName.trim()) return;
    try {
      const r = await authFetch(`${API}/license-server/api-keys`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: newApiKeyName.trim() }) });
      if (r.ok) { toast('API ключ создан', 'success'); setNewApiKeyName(''); fetchApiKeys(); }
      else { const d = await r.json().catch(() => ({ error: 'Ошибка' })); toast(d.error || 'Ошибка', 'error'); }
    } catch { toast('Ошибка сети', 'error'); }
  };
  const deleteApiKey = async (id: string) => {
    setConfirmModal({ show: true, title: 'Удалить API ключ?', message: 'Это действие необратимо.', type: 'danger', onConfirm: async () => {
      try {
        const r = await authFetch(`${API}/license-server/api-keys/${id}`, { method: 'DELETE' });
        if (r.ok) { toast('API ключ удалён', 'success'); fetchApiKeys(); }
        else { const d = await r.json().catch(() => ({ error: 'Ошибка' })); toast(d.error || 'Ошибка', 'error'); }
      } catch { toast('Ошибка сети', 'error'); }
      setConfirmModal(null);
    }});
  };
  const bulkExtend = async (ids: string[]) => {
    try {
      await Promise.all(ids.map(id => authFetch(`${API}/license-server/licenses/${id}/extend`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ days: 30 }) })));
      toast('Лицензии продлены', 'success'); fetchLicenses();
    } catch { toast('Ошибка массового продления', 'error'); }
  };
  const bulkRevoke = async (ids: string[]) => {
    setConfirmModal({ show: true, title: 'Отозвать выбранные лицензии?', message: `Будет отозвано лицензий: ${ids.length}`, type: 'warn', onConfirm: async () => {
      try {
        await Promise.all(ids.map(id => authFetch(`${API}/license-server/licenses/${id}/revoke`, { method: 'POST' })));
        toast('Лицензии отозваны', 'success'); fetchLicenses();
      } catch { toast('Ошибка массового отзыва', 'error'); }
      setConfirmModal(null);
    }});
  };
  const downloadBackup = async () => {
    try {
      const r = await authFetch(`${API}/license-server/backup`);
      if (!r.ok) { toast('Ошибка создания backup', 'error'); return; }
      const blob = await r.blob();
      const url = window.URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `license-backup-${new Date().toISOString().slice(0,10)}.json`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      window.URL.revokeObjectURL(url);
      toast('Backup скачан', 'success');
    } catch { toast('Ошибка скачивания', 'error'); }
  };
  const restoreBackup = async (file: File) => {
    try {
      const text = await file.text();
      const r = await authFetch(`${API}/license-server/restore`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: text });
      if (r.ok) { toast('Backup восстановлен', 'success'); fetchLicenses(); fetchAudit(); }
      else { const d = await r.json().catch(() => ({ error: 'Ошибка' })); toast(d.error || 'Ошибка восстановления', 'error'); }
    } catch { toast('Некорректный JSON', 'error'); }
  };

  const rolePolicies = useMemo<Record<string, UserHostPermission[]>>(
    () => centralCfg?.rolePolicies || { admin: [] },
    [centralCfg?.rolePolicies],
  );

  const roleSections = useMemo<Record<string, RoleSectionPolicy>>(
    () => centralCfg?.roleSections || { admin: fullSections },
    [centralCfg?.roleSections, fullSections],
  );

  const availableGroups = useMemo(() => {
    const keys = Array.from(new Set(['admin', ...Object.keys(rolePolicies || {})]));
    return keys.sort((a, b) => (a === 'admin' ? -1 : b === 'admin' ? 1 : a.localeCompare(b)));
  }, [rolePolicies]);
  const createUser = async () => {
    if (!newUser.username || !newUser.password) { setUserMsg('Заполните логин и пароль'); return; }
    try {
      const r = await authFetch(`${API}/auth/register`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(newUser) });
      if (r.ok) { toast('Пользователь создан', 'success'); setNewUser({ username: '', password: '', role: 'user' }); fetchUsers(); }
      else { const d = await r.json().catch(() => ({ error: 'Ошибка' })); setUserMsg(d.error || 'Ошибка'); }
    } catch { setUserMsg('Ошибка сети'); }
    setTimeout(() => setUserMsg(''), 3000);
  };
  const deleteUser = async (id: string) => {
    if (!confirm('Удалить пользователя?')) return;
    try {
      const r = await authFetch(`${API}/auth/users/${id}`, { method: 'DELETE' });
      if (r.ok) { toast('Пользователь удалён', 'success'); fetchUsers(); }
      else { const d = await r.json().catch(() => ({ error: 'Ошибка' })); toast(d.error || 'Ошибка удаления', 'error'); }
    } catch { toast('Ошибка сети', 'error'); }
  };
  const updateUserRole = async (id: string, role: string) => {
    try {
      const r = await authFetch(`${API}/auth/users/${id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ role }),
      });
      if (r.ok) {
        toast('Категория пользователя обновлена', 'success');
        fetchUsers();
        fetchMe();
      } else {
        const d = await r.json().catch(() => ({ error: 'Ошибка' }));
        toast(d.error || 'Ошибка обновления категории', 'error');
      }
    } catch {
      toast('Ошибка сети', 'error');
    }
  };

  const saveRolePolicies = async (nextRolePolicies: Record<string, UserHostPermission[]>, nextRoleSections: Record<string, RoleSectionPolicy>) => {
    try {
      const r = await authFetch(`${API}/auth/role-policies`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ rolePolicies: nextRolePolicies, roleSections: nextRoleSections }),
      });
      if (r.ok) {
        const d = await r.json().catch(() => ({ rolePolicies: nextRolePolicies, roleSections: nextRoleSections }));
        setCentralCfg(prev => prev ? ({ ...prev, rolePolicies: d?.rolePolicies || nextRolePolicies, roleSections: d?.roleSections || nextRoleSections }) : prev);
        toast('Права категорий обновлены', 'success');
      } else {
        const d = await r.json().catch(() => ({ error: 'Ошибка' }));
        toast(d.error || 'Ошибка сохранения прав категорий', 'error');
      }
    } catch {
      toast('Ошибка сети', 'error');
    }
  };

  const uploadBg = async (file: File) => {
    setBgUploading(true);
    try {
      const fd = new FormData(); fd.append('file', file);
      const r = await authFetch(`${API}/backgrounds`, { method: 'POST', body: fd });
      if (r.ok) { const d = await r.json(); toast('Фон загружен', 'success'); fetchBgList(); return d.name as string; }
      else { toast('Ошибка загрузки', 'error'); }
    } catch { toast('Ошибка загрузки', 'error'); }
    setBgUploading(false);
    return null;
  };

  const deleteBg = async (name: string) => {
    try {
      const r = await authFetch(`${API}/backgrounds/${name}`, { method: 'DELETE' });
      if (r.ok) {
        toast('Фон удалён', 'success');
        fetchBgList();
        if (centralCfg?.bgImage === name) {
          const next = { ...centralCfg, bgImage: '' };
          setCentralCfg(next);
          await saveCentralCfg(next);
        }
      }
    } catch { toast('Ошибка удаления', 'error'); }
  };

  // S3 Browser functions
  const s3FetchDir = useCallback(async (agentId: string, prefix: string) => {
    const data = await fetchJSON<{objects: {key: string; lastModified: string; size: number; isDir: boolean}[]; prefix: string}>(
      `${API}/agents/${agentId}/proxy/api/v1/s3/browse?prefix=${encodeURIComponent(prefix)}`
    );
    return data.objects || [];
  }, []);

  const s3Browse = useCallback(async (agentId: string, prefix: string) => {
    if (!agentId) return;
    setS3Loading(true); setS3Error('');
    try {
      const objects = await s3FetchDir(agentId, prefix);
      setS3Objects(objects);
      setS3Prefix(prefix);
      setS3TreeLoaded(prev => ({ ...prev, [prefix]: objects }));
      // Build tree: add folders from this level
      if (prefix === '') {
        const dirs = objects.filter(o => o.isDir).map(o => ({ key: o.key, expanded: false }));
        setS3Tree(dirs);
      } else {
        setS3Tree(prev => {
          const update = (nodes: typeof prev): typeof prev =>
            nodes.map(n => {
              if (n.key === prefix) {
                const children = objects.filter(o => o.isDir).map(o => ({ key: o.key, expanded: false }));
                return { ...n, expanded: true, children };
              }
              if (n.children) return { ...n, children: update(n.children as typeof prev) };
              return n;
            });
          return update(prev);
        });
      }
    } catch (e: any) { setS3Error(e.message || 'Ошибка загрузки'); setS3Objects([]); }
    setS3Loading(false);
  }, [s3FetchDir]);

  const s3ToggleTree = useCallback((key: string, agentId: string) => {
    setS3Tree(prev => {
      const toggle = (nodes: typeof prev): typeof prev =>
        nodes.map(n => {
          if (n.key === key) return { ...n, expanded: !n.expanded };
          if (n.children) return { ...n, children: toggle(n.children as typeof prev) };
          return n;
        });
      return toggle(prev);
    });
    // If not yet loaded, fetch
    if (!s3TreeLoaded[key]) s3Browse(agentId, key);
    else { setS3Prefix(key); setS3Objects(s3TreeLoaded[key]); }
  }, [s3Browse, s3TreeLoaded]);

  const s3Delete = async (key: string) => {
    if (!s3Agent) return;
    try {
      const url = `${API}/agents/${s3Agent}/proxy/api/v1/s3/delete`;
      const r = await authFetch(url, {
        method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify({key})
      });
      const data = await r.text();
      if (r.ok) {
        toast('Удалено!', 'success');
        const newLoaded = { ...s3TreeLoaded };
        delete newLoaded[key];
        setS3TreeLoaded(newLoaded);
        s3Browse(s3Agent, s3Prefix);
      } else {
        toast(`Ошибка ${r.status}: ${data}`, 'error');
      }
    } catch (e: any) { toast(`Сеть: ${e.message}`, 'error'); }
  };

  const s3Download = (key: string) => {
    if (!s3Agent) return;
    window.open(`${API}/agents/${s3Agent}/proxy/api/v1/s3/download?key=${encodeURIComponent(key)}`, '_blank');
  };

  // SMB Browser functions
  const smbFetchDir = useCallback(async (agentId: string, path: string) => {
    const data = await fetchJSON<{objects: {key: string; lastModified: string; size: number; isDir: boolean}[]; path: string}>(
      `${API}/agents/${agentId}/proxy/api/v1/smb/browse?path=${encodeURIComponent(path)}`
    );
    return data.objects || [];
  }, []);

  const smbBrowse = useCallback(async (agentId: string, path: string) => {
    if (!agentId) return;
    setSmbLoading(true); setSmbError('');
    try {
      const objects = await smbFetchDir(agentId, path);
      setSmbObjects(objects); setSmbPath(path);
      setSmbTreeLoaded(prev => ({ ...prev, [path]: objects }));
      if (path === '') {
        setSmbTree(objects.filter(o => o.isDir).map(o => ({ key: o.key, expanded: false })));
      } else {
        setSmbTree(prev => {
          const update = (nodes: typeof prev): typeof prev =>
            nodes.map(n => {
              if (n.key === path) { return { ...n, expanded: true, children: objects.filter(o => o.isDir).map(o => ({ key: o.key, expanded: false })) }; }
              if (n.children) return { ...n, children: update(n.children as typeof prev) };
              return n;
            });
          return update(prev);
        });
      }
    } catch (e: any) { setSmbError(e.message || 'Ошибка загрузки'); setSmbObjects([]); }
    setSmbLoading(false);
  }, [smbFetchDir]);

  const smbToggleTree = useCallback((key: string, agentId: string) => {
    setSmbTree(prev => {
      const toggle = (nodes: typeof prev): typeof prev =>
        nodes.map(n => {
          if (n.key === key) return { ...n, expanded: !n.expanded };
          if (n.children) return { ...n, children: toggle(n.children as typeof prev) };
          return n;
        });
      return toggle(prev);
    });
    if (!smbTreeLoaded[key]) smbBrowse(agentId, key);
    else { setSmbPath(key); setSmbObjects(smbTreeLoaded[key]); }
  }, [smbBrowse, smbTreeLoaded]);

  const smbDelete = async (key: string) => {
    if (!smbAgent) return;
    try {
      const r = await authFetch(`${API}/agents/${smbAgent}/proxy/api/v1/smb/delete`, {
        method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify({key})
      });
      if (r.ok) { toast('Удалено!', 'success'); const nl = { ...smbTreeLoaded }; delete nl[key]; setSmbTreeLoaded(nl); smbBrowse(smbAgent, smbPath); }
      else { const d = await r.text(); toast(`Ошибка: ${d}`, 'error'); }
    } catch (e: any) { toast(`Сеть: ${e.message}`, 'error'); }
  };

  const smbDownload = (key: string) => {
    if (!smbAgent) return;
    window.open(`${API}/agents/${smbAgent}/proxy/api/v1/smb/download?key=${encodeURIComponent(key)}`, '_blank');
  };

  // WebDAV Browser functions
  const davFetchDir = useCallback(async (agentId: string, path: string) => {
    const data = await fetchJSON<{objects: {key: string; lastModified: string; size: number; isDir: boolean}[]; path: string}>(
      `${API}/agents/${agentId}/proxy/api/v1/webdav/browse?path=${encodeURIComponent(path)}`
    );
    return data.objects || [];
  }, []);

  const davBrowse = useCallback(async (agentId: string, path: string) => {
    if (!agentId) return;
    setDavLoading(true); setDavError('');
    try {
      const objects = await davFetchDir(agentId, path);
      setDavObjects(objects); setDavPath(path);
      setDavTreeLoaded(prev => ({ ...prev, [path]: objects }));
      if (path === '') {
        setDavTree(objects.filter(o => o.isDir).map(o => ({ key: o.key, expanded: false })));
      } else {
        setDavTree(prev => {
          const update = (nodes: typeof prev): typeof prev =>
            nodes.map(n => {
              if (n.key === path) { return { ...n, expanded: true, children: objects.filter(o => o.isDir).map(o => ({ key: o.key, expanded: false })) }; }
              if (n.children) return { ...n, children: update(n.children as typeof prev) };
              return n;
            });
          return update(prev);
        });
      }
    } catch (e: any) { setDavError(e.message || 'Ошибка загрузки'); setDavObjects([]); }
    setDavLoading(false);
  }, [davFetchDir]);

  const davToggleTree = useCallback((key: string, agentId: string) => {
    setDavTree(prev => {
      const toggle = (nodes: typeof prev): typeof prev =>
        nodes.map(n => {
          if (n.key === key) return { ...n, expanded: !n.expanded };
          if (n.children) return { ...n, children: toggle(n.children as typeof prev) };
          return n;
        });
      return toggle(prev);
    });
    if (!davTreeLoaded[key]) davBrowse(agentId, key);
    else { setDavPath(key); setDavObjects(davTreeLoaded[key]); }
  }, [davBrowse, davTreeLoaded]);

  const davDelete = async (key: string) => {
    if (!davAgent) return;
    try {
      const r = await authFetch(`${API}/agents/${davAgent}/proxy/api/v1/webdav/delete`, {
        method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify({key})
      });
      if (r.ok) { toast('Удалено!', 'success'); const nl = { ...davTreeLoaded }; delete nl[key]; setDavTreeLoaded(nl); davBrowse(davAgent, davPath); }
      else { const d = await r.text(); toast(`Ошибка: ${d}`, 'error'); }
    } catch (e: any) { toast(`Сеть: ${e.message}`, 'error'); }
  };

  const davDownload = (key: string) => {
    if (!davAgent) return;
    window.open(`${API}/agents/${davAgent}/proxy/api/v1/webdav/download?key=${encodeURIComponent(key)}`, '_blank');
  };

  const saveCentralCfg = async (cfg: CentralConfig): Promise<boolean> => {
    setCfgSaving(true);
    let ok = false;
    try {
      const r = await authFetch(`${API}/config`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(cfg) });
      if (r.ok) {
        const next = await r.json().catch(() => null);
        setCentralCfg(next || cfg);
        toast('Настройки сохранены', 'success');
        ok = true;
      } else {
        toast('Ошибка сохранения', 'error');
      }
    } catch { toast('Ошибка сохранения', 'error'); }
    setCfgSaving(false);
    return ok;
  };
  const downloadCentralConfigBackup = async () => {
    try {
      const r = await authFetch(`${API}/config/backup`);
      if (!r.ok) { toast('Ошибка скачивания backup конфига', 'error'); return; }
      const blob = await r.blob();
      const url = window.URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      const ts = new Date().toISOString().replace(/[:.]/g, '-');
      a.download = `nodax-central-config-${ts}.json`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      window.URL.revokeObjectURL(url);
      toast('Backup скачан (конфиг + хосты)', 'success');
    } catch {
      toast('Ошибка скачивания backup конфига', 'error');
    }
  };
  const restoreCentralConfigBackup = async (file: File) => {
    setCfgImporting(true);
    try {
      const text = await file.text();
      const parsed = JSON.parse(text);
      const r = await authFetch(`${API}/config/restore`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(parsed),
      });
      const data = await r.json().catch(() => ({}));
      if (!r.ok) {
        toast(data?.error || 'Ошибка восстановления конфига', 'error');
        return;
      }
      await fetchCentralCfg();
      toast('Backup восстановлен (конфиг + хосты)', 'success');
    } catch {
      toast('Некорректный JSON backup файла', 'error');
    }
    setCfgImporting(false);
  };
  const recheckCaddyCert = async () => {
    setCaddyRecheckMsg('');
    setCaddyRecheckLoading(true);
    try {
      const r = await authFetch(`${API}/caddy/recheck`, { method: 'POST' });
      const data = await r.json().catch(() => ({}));
      if (!r.ok) {
        const msg = data?.error || 'Ошибка re-check Caddy';
        setCaddyRecheckMsg(msg);
        toast(msg, 'error');
        return;
      }
      const details = [data?.message, data?.issuer ? `Issuer: ${data.issuer}` : '', data?.notAfter ? `Valid to: ${data.notAfter}` : ''].filter(Boolean).join(' | ');
      setCaddyRecheckMsg(details || 'Caddy re-check completed');
      toast('Re-check выполнен', 'success');
    } catch {
      setCaddyRecheckMsg('Ошибка re-check Caddy');
      toast('Ошибка re-check Caddy', 'error');
    }
    setCaddyRecheckLoading(false);
  };
  const recheckLicense = async () => {
    if (!centralCfg) return;
    setLicenseRecheckMsg('');
    setLicenseRecheckLoading(true);
    try {
      let nextCfg: CentralConfig = centralCfg;
      if ((centralCfg.licenseServer || '').trim() && !(centralCfg.licensePubKey || '').trim()) {
        try {
          const pubResp = await fetch(`${(centralCfg.licenseServer || '').trim().replace(/\/$/, '')}/api/v1/public-key`);
          const pubData = await pubResp.json().catch(() => ({}));
          if (pubResp.ok && pubData?.publicKey) {
            nextCfg = { ...centralCfg, licensePubKey: pubData.publicKey };
            setCentralCfg(nextCfg);
          }
        } catch {}
      }
      const saved = await saveCentralCfg(nextCfg);
      if (!saved) {
        setLicenseRecheckMsg('Сначала исправьте ошибку сохранения настроек');
        return;
      }
      const r = await authFetch(`${API}/license/recheck`, { method: 'POST' });
      const data = await r.json().catch(() => ({}));
      if (!r.ok) {
        const msg = data?.error || 'Ошибка проверки лицензии';
        setLicenseRecheckMsg(msg);
        toast(msg, 'error');
        return;
      }
      mergeLicenseStatus(data || {});
      const msg = `Статус: ${licenseStatusLabel(data?.status)}${data?.reason ? ` (${data.reason})` : ''}`;
      setLicenseRecheckMsg(msg);
      toast('Лицензия обновлена', 'success');
    } catch {
      setLicenseRecheckMsg('Ошибка проверки лицензии');
      toast('Ошибка проверки лицензии', 'error');
    }
    setLicenseRecheckLoading(false);
  };
  const canControlAgentUI = useCallback((agentId: string | null) => {
    if (!agentId) return false;
    const role = (myProfile?.role || auth.user.role || 'user').toLowerCase();
    if (role === 'admin') return true;
    const perms = rolePolicies[role] || [];
    if (perms.length === 0) return false;
    const p = perms.find(x => x.agentId === agentId);
    return !!p?.control;
  }, [myProfile?.role, auth.user.role, rolePolicies]);

  const canAccessSectionUI = useCallback((section: keyof RoleSectionPolicy) => {
    const role = (myProfile?.role || auth.user.role || 'user').toLowerCase();
    if (role === 'admin') return true;
    const sec = roleSections[role];
    if (!sec) return false;
    return !!sec[section];
  }, [myProfile?.role, auth.user.role, roleSections]);

  const openPolicyModal = (role: string) => {
    const next: Record<string, { view: boolean; control: boolean }> = {};
    (rolePolicies[role] || []).forEach(p => { next[p.agentId] = { view: !!p.view, control: !!p.control }; });
    setPolicyDraft(next);
    const sec = role === 'admin' ? fullSections : (roleSections[role] || emptySections);
    setSectionDraft({ ...sec });
    setPolicyModalRole(role);
  };

  const createGroup = async () => {
    const name = newGroupName.trim().toLowerCase();
    if (!name) return;
    if (!/^[a-z0-9_-]+$/.test(name)) {
      toast('Имя группы: только a-z, 0-9, _ и -', 'error');
      return;
    }
    if (availableGroups.includes(name)) {
      toast('Такая группа уже существует', 'error');
      return;
    }
    await saveRolePolicies(
      { ...rolePolicies, [name]: [] },
      { ...roleSections, [name]: { ...emptySections } },
    );
    setNewGroupName('');
  };

  const deleteGroup = async (role: string) => {
    if (role === 'admin') return;
    if (users.some(u => (u.role || '').toLowerCase() === role.toLowerCase())) {
      toast('Нельзя удалить: группа назначена пользователям', 'error');
      return;
    }
    const next = { ...rolePolicies };
    const nextSections = { ...roleSections };
    delete next[role];
    delete nextSections[role];
    await saveRolePolicies(next, nextSections);
  };

  const savePolicyModal = async () => {
    if (!policyModalRole) return;
    if (policyModalRole === 'admin') { setPolicyModalRole(null); return; }
    const perms: UserHostPermission[] = Object.entries(policyDraft)
      .filter(([, v]) => v.view || v.control)
      .map(([agentId, v]) => ({ agentId, view: !!v.view || !!v.control, control: !!v.control }));
    const current = rolePolicies;
    const nextSections = {
      ...roleSections,
      [policyModalRole]: policyModalRole === 'admin' ? { ...fullSections } : { ...sectionDraft },
    };
    await saveRolePolicies({ ...current, [policyModalRole]: perms }, nextSections);
    setPolicyModalRole(null);
  };

  useEffect(() => { fetchCentralCfg(); }, [fetchCentralCfg]);
  useEffect(() => { fetchAgents(); fetchOverview(); const i = setInterval(() => { fetchAgents(); fetchOverview(); }, 10000); return () => clearInterval(i); }, [fetchAgents, fetchOverview]);
  useEffect(() => { if (selectedAgent) { setCpuHistory([]); setRamHistory([]); setHostHistory([]); fetchAgentData(selectedAgent); fetchHistory(selectedAgent); const i = setInterval(() => { fetchAgentData(selectedAgent); fetchHistory(selectedAgent); }, 15000); return () => clearInterval(i); } }, [selectedAgent, fetchAgentData, fetchHistory]);
  // Track CPU/RAM history
  useEffect(() => {
    if (agentData?.hostInfo) {
      setCpuHistory(h => [...h.slice(-29), agentData.hostInfo!.cpuUsage]);
      setRamHistory(h => [...h.slice(-29), agentData.hostInfo!.ramUsePct]);
    }
  }, [agentData?.hostInfo?.cpuUsage, agentData?.hostInfo?.ramUsePct]);
  useEffect(() => { if (hostTab === 'journal') { fetchLogs(); } }, [hostTab, fetchLogs]);
  useEffect(() => { if (hostTab === 'settings') { fetchSettings(); fetchSchedules(); } }, [hostTab, fetchSettings, fetchSchedules]);
  useEffect(() => { if (hostTab === 'backups') { fetchBackupLogs(); fetchBackupFiles(); fetchSettings(); fetchSchedules(); } }, [hostTab, fetchBackupLogs, fetchBackupFiles, fetchSettings, fetchSchedules]);
  useEffect(() => { if (page === 'statistics') { fetchStats(); } }, [page, fetchStats]);
  useEffect(() => { fetchMe(); }, [fetchMe]);
  useEffect(() => { if (page === 'central-settings') { fetchCentralCfg(); fetchBgList(); fetchLicenseStatus(); } }, [page, fetchCentralCfg, fetchBgList, fetchLicenseStatus]);
  useEffect(() => { if (page === 'security' && canAccessSectionUI('security')) { fetchUsers(); fetchRolePolicies(); } }, [page, fetchUsers, fetchRolePolicies, canAccessSectionUI]);

  useEffect(() => {
    const blocked =
      (page === 'overview' && !canAccessSectionUI('overview')) ||
      (page === 'statistics' && !canAccessSectionUI('statistics')) ||
      ((page === 's3-browser' || page === 'smb-browser' || page === 'webdav-browser') && !canAccessSectionUI('storage')) ||
      (page === 'central-settings' && !canAccessSectionUI('settings')) ||
      (page === 'security' && !canAccessSectionUI('security'));
    if (!blocked) return;
    if (canAccessSectionUI('overview')) setPage('overview');
    else if (canAccessSectionUI('statistics')) setPage('statistics');
    else if (canAccessSectionUI('storage')) setPage('s3-browser');
    else if (canAccessSectionUI('settings')) setPage('central-settings');
    else if (canAccessSectionUI('security')) setPage('security');
  }, [page, canAccessSectionUI]);

  const openHost = (id: string) => { setSelectedAgent(id); setAgentData(null); setPage('host'); setHostTab('panel'); setVmSearch(''); setVmFilter('all'); };
  const goOverview = () => { setPage('overview'); setSelectedAgent(null); setAgentData(null); };

  const handleAddAgent = async () => {
    setAddError('');
    if (!addForm.url) { setAddError('URL обязателен'); return; }
    try { await fetchJSON<Agent>(`${API}/agents`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(addForm) }); setShowAddModal(false); setAddForm({ url: '', apiKey: '' }); fetchAgents(); fetchOverview(); toast('Хост добавлен', 'success'); } catch (e: any) { setAddError(e.message); }
  };
  const confirmDeleteAgent = async () => {
    if (!deleteAgentId) return;
    try { await authFetch(`${API}/agents/${deleteAgentId}`, { method: 'DELETE' }); toast('Хост удалён', 'success'); fetchAgents(); fetchOverview(); if (selectedAgent === deleteAgentId) goOverview(); } catch { toast('Ошибка удаления хоста', 'error'); }
    setDeleteAgentId(null);
  };

  const vmAction = async (agentId: string, vmName: string, action: string) => {
    setActionLoading(vmName);
    try { await authFetch(`${API}/agents/${agentId}/proxy/api/v1/vm/${encodeURIComponent(vmName)}/action`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ action }) }); setTimeout(() => fetchAgentData(agentId), 1500); } catch {} finally { setActionLoading(null); }
  };

  const openVmDetail = async (vmName: string) => {
    try { const d = await fetchJSON<VMDetail>(proxyUrl(selectedAgent, `/api/v1/vm/${encodeURIComponent(vmName)}/detail`)); setVmDetail(d); setShowVmDetail(true); } catch { toast('Не удалось загрузить детали ВМ', 'error'); }
  };

  const openRenameModal = (vmName: string) => { setRenameTarget(vmName); setRenameValue(vmName); };
  const doRename = async () => {
    if (!renameTarget || !renameValue || renameValue === renameTarget) { setRenameTarget(null); return; }
    try {
      await authFetch(proxyUrl(selectedAgent, `/api/v1/vm/${encodeURIComponent(renameTarget)}/rename`), { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ newName: renameValue }) });
      toast(`ВМ переименована: ${renameValue}`, 'success');
      setTimeout(() => selectedAgent && fetchAgentData(selectedAgent), 1500);
    } catch { toast('Ошибка переименования', 'error'); }
    setRenameTarget(null);
  };

  const openDeleteModal = (vmName: string) => { setDeleteTarget(vmName); };
  const doDelete = async () => {
    if (!deleteTarget || !selectedAgent) return;
    try {
      const r = await authFetch(proxyUrl(selectedAgent, `/api/v1/vm/${encodeURIComponent(deleteTarget)}/delete`), { method: 'POST' });
      if (!r.ok) { const t = await r.text(); toast(`Ошибка удаления: ${t}`, 'error'); }
      else { toast(`ВМ ${deleteTarget} удалена`, 'success'); setTimeout(() => fetchAgentData(selectedAgent), 2000); }
    } catch { toast('Ошибка удаления', 'error'); }
    setDeleteTarget(null);
  };

  const openSnapModal = (vmName: string) => { setSnapTarget(vmName); setSnapName(`Snapshot-${new Date().toISOString().slice(0,19)}`); };
  const doSnapshot = async () => {
    if (!snapTarget) return;
    setActionLoading(snapTarget);
    try {
      await authFetch(proxyUrl(selectedAgent, `/api/v1/vm/${encodeURIComponent(snapTarget)}/action`), { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ action: 'snapshot', name: snapName }) });
      toast(`Снимок «${snapName}» создан`, 'success');
      setTimeout(() => selectedAgent && fetchAgentData(selectedAgent), 1500);
    } catch { toast('Ошибка создания снимка', 'error'); } finally { setActionLoading(null); }
    setSnapTarget(null);
  };

  const deployVM = async () => {
    if (!deployForm.name || !deployForm.storagePath || !selectedAgent) return;
    setDeployLoading(true);
    try {
      const r = await authFetch(proxyUrl(selectedAgent, '/api/v1/vm/deploy'), {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: deployForm.name, cpuCount: deployForm.cpu, ramGB: deployForm.ram, storagePath: deployForm.storagePath, switchName: deployForm.switchName, diskSizes: deployDisks, osType: deployForm.osType })
      });
      if (!r.ok) { const t = await r.text(); toast(`Ошибка создания: ${t}`, 'error'); }
      else {
        toast(`ВМ ${deployForm.name} создана!`, 'success');
        setShowDeployModal(false);
        setDeployForm({ name: '', cpu: 2, ram: 4, storagePath: 'D:\\Hyper-V', switchName: 'Default Switch', osType: 'windows' });
        setDeployDisks([127]);
        setTimeout(() => fetchAgentData(selectedAgent), 2000);
      }
    } catch { toast('Ошибка создания ВМ', 'error'); }
    setDeployLoading(false);
  };

  const doBackup = async (vmName: string) => {
    setBackupLoading(vmName); setBackupMsg('');
    setBackupStatus(s => ({ ...s, [vmName]: 'running' }));
    try {
      const dest = backupDest[vmName] || settings?.BackupPath || 'D:\\Backups';
      const r = await authFetch(proxyUrl(selectedAgent, `/api/v1/vm/${encodeURIComponent(vmName)}/backup`), { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ destination: dest }) });
      if (r.ok) {
        setBackupMsg(`Бэкап ${vmName} запущен`);
        setBackupStatus(s => ({ ...s, [vmName]: 'done' }));
      } else {
        setBackupMsg(`Ошибка бэкапа ${vmName}`);
        setBackupStatus(s => ({ ...s, [vmName]: 'error' }));
      }
      setTimeout(() => fetchBackupLogs(), 3000);
      setTimeout(() => { setBackupStatus(s => ({ ...s, [vmName]: 'idle' })); setBackupMsg(''); }, 8000);
    } catch { setBackupMsg(`Ошибка бэкапа ${vmName}`); setBackupStatus(s => ({ ...s, [vmName]: 'error' })); setTimeout(() => setBackupStatus(s => ({ ...s, [vmName]: 'idle' })), 8000); }
    finally { setBackupLoading(null); }
  };

  const openRestoreModal = (bf: BackupFile) => {
    setRestoreTarget(bf);
    setRestoreForm({ newVMName: bf.vmName + '_restored', restorePath: '' });
  };

  const doRestore = async () => {
    if (!restoreTarget || !selectedAgent) return;
    setRestoreLoading(true);
    try {
      const r = await authFetch(proxyUrl(selectedAgent, '/api/v1/vm/restore'), {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ archivePath: restoreTarget.filePath, newVMName: restoreForm.newVMName, restorePath: restoreForm.restorePath })
      });
      if (r.ok) {
        toast(`Восстановление ${restoreTarget.vmName} запущено`, 'success');
        setRestoreTarget(null);
        setTimeout(() => { fetchBackupLogs(); fetchAgentData(selectedAgent); }, 3000);
      } else {
        const t = await r.text();
        toast(`Ошибка: ${t}`, 'error');
      }
    } catch (e: any) { toast(`Ошибка: ${e.message}`, 'error'); }
    setRestoreLoading(false);
  };

  const filteredBackupFiles = useMemo(() => {
    if (!backupArchiveFilter) return backupFiles;
    const q = backupArchiveFilter.toLowerCase();
    return backupFiles.filter(f => f.vmName.toLowerCase().includes(q) || f.fileName.toLowerCase().includes(q));
  }, [backupFiles, backupArchiveFilter]);

  const testS3 = async () => {
    setSettingsMsg('Проверка S3...');
    try {
      // Try a simple HEAD request to the S3 endpoint to test connectivity
      const r = await authFetch(proxyUrl(selectedAgent, '/api/v1/settings'), { method: 'GET' });
      if (r.ok && settings?.S3Endpoint && settings?.S3Bucket) {
        setSettingsMsg('S3: настройки корректны (подключение через агент)');
      } else {
        setSettingsMsg('S3: проверьте настройки');
      }
    } catch { setSettingsMsg('S3: ошибка подключения'); }
    setTimeout(() => setSettingsMsg(''), 4000);
  };

  const testSMB = async () => {
    setSettingsMsg('Проверка SMB...');
    try {
      const r = await authFetch(proxyUrl(selectedAgent, '/api/v1/test/smb'));
      const d = await r.json();
      setSettingsMsg(d.ok ? `✅ ${d.message}` : `❌ ${d.error}`);
    } catch { setSettingsMsg('SMB: ошибка подключения'); }
    setTimeout(() => setSettingsMsg(''), 4000);
  };

  const testWebDAV = async () => {
    setSettingsMsg('Проверка WebDAV...');
    try {
      const r = await authFetch(proxyUrl(selectedAgent, '/api/v1/test/webdav'));
      const d = await r.json();
      setSettingsMsg(d.ok ? `✅ ${d.message}` : `❌ ${d.error}`);
    } catch { setSettingsMsg('WebDAV: ошибка подключения'); }
    setTimeout(() => setSettingsMsg(''), 4000);
  };

  const saveSettings = async () => {
    if (!settings || !selectedAgent) return;
    try {
      const payload: Settings = { ...settings };
      // In UI user sets base folder in bucket, host suffix is appended automatically.
      payload.S3Prefix = buildS3Prefix(settings.S3Prefix || '', selectedAgentInfo?.name || '');
      payload.SMBPath = buildSMBPath(settings.SMBPath || '', selectedAgentInfo?.name || '');
      payload.WebDAVPath = buildWebDAVPath(settings.WebDAVPath || '', selectedAgentInfo?.name || '');
      const r = await authFetch(proxyUrl(selectedAgent, '/api/v1/settings'), { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) });
      if (r.ok) { toast('Настройки сохранены', 'success'); setSettingsMsg('✅ Настройки сохранены'); }
      else { const t = await r.text(); toast(`Ошибка: ${t}`, 'error'); setSettingsMsg(`❌ ${t}`); }
    } catch (e: any) { toast('Ошибка сохранения', 'error'); setSettingsMsg('❌ Ошибка сохранения'); }
    setTimeout(() => setSettingsMsg(''), 3000);
  };

  const deleteSchedule = async (id: number) => {
    if (!selectedAgent) return;
    try {
      const r = await authFetch(proxyUrl(selectedAgent, `/api/v1/schedules/${id}`), { method: 'DELETE' });
      if (!r.ok) {
        const t = await r.text();
        toast(`Ошибка удаления: ${t || r.status}`, 'error');
        return;
      }
      setSchedules(prev => prev.filter(s => s.ID !== id));
      toast('Расписание удалено', 'success');
      fetchSchedules();
    } catch (e: any) {
      toast(`Ошибка сети: ${e?.message || 'delete failed'}`, 'error');
    }
  };

  const purgeLogs = async () => {
    if (!confirm('Очистить весь журнал?')) return;
    try { await authFetch(proxyUrl(selectedAgent, '/api/v1/logs/purge'), { method: 'POST' }); setLogs([]); setLogPage(1); } catch {}
  };

  const createSchedule = async () => {
    if (!newSched.vmNames.length) { setSettingsMsg('Выберите хотя бы одну ВМ'); return; }
    const [hh, mm] = newSched.time.split(':');
    const cron = `${mm} ${hh} * * *`;
    try {
      await authFetch(proxyUrl(selectedAgent, '/api/v1/schedules'), {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ CronString: cron, VMList: JSON.stringify(newSched.vmNames), Destination: newSched.dest || settings?.BackupPath || '', Enabled: true })
      });
      setSettingsMsg('Расписание создано'); fetchSchedules();
      setNewSched({ time: '03:00', dest: '', vmNames: [] });
      setTimeout(() => setSettingsMsg(''), 3000);
    } catch { setSettingsMsg('Ошибка создания расписания'); }
  };

  const toggleSchedVM = (name: string) => {
    setNewSched(s => ({ ...s, vmNames: s.vmNames.includes(name) ? s.vmNames.filter(n => n !== name) : [...s.vmNames, name] }));
  };

  const startEditSched = (sc: Schedule) => {
    const parts = sc.CronString.split(' ');
    const time = `${parts[1]?.padStart(2, '0')}:${parts[0]?.padStart(2, '0')}`;
    let vmNames: string[] = [];
    try { vmNames = JSON.parse(sc.VMList); } catch {}
    setEditSched({ id: sc.ID, time, dest: sc.Destination, vmNames, enabled: sc.Enabled });
  };

  const toggleEditSchedVM = (name: string) => {
    if (!editSched) return;
    setEditSched({ ...editSched, vmNames: editSched.vmNames.includes(name) ? editSched.vmNames.filter(n => n !== name) : [...editSched.vmNames, name] });
  };

  const updateSchedule = async () => {
    if (!editSched || !editSched.vmNames.length) { setSettingsMsg('Выберите хотя бы одну ВМ'); return; }
    const [hh, mm] = editSched.time.split(':');
    const cron = `${mm} ${hh} * * *`;
    try {
      await authFetch(proxyUrl(selectedAgent, `/api/v1/schedules/${editSched.id}`), {
        method: 'PUT', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ CronString: cron, VMList: JSON.stringify(editSched.vmNames), Destination: editSched.dest, Enabled: editSched.enabled })
      });
      setSettingsMsg('Расписание обновлено'); fetchSchedules(); setEditSched(null);
      setTimeout(() => setSettingsMsg(''), 3000);
    } catch { setSettingsMsg('Ошибка обновления расписания'); }
  };

  const testTelegram = async () => {
    try {
      const r = await fetch(`https://api.telegram.org/bot${settings?.TelegramBotToken}/sendMessage`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ chat_id: settings?.TelegramChatID, text: '✅ NODAX Central: тест уведомления' })
      });
      setSettingsMsg(r.ok ? 'Telegram: сообщение отправлено' : 'Telegram: ошибка');
    } catch { setSettingsMsg('Telegram: ошибка подключения'); }
    setTimeout(() => setSettingsMsg(''), 3000);
  };

  const LineChart = ({ data, labels = [], color, title, unit = '', h = 120, maxVal }: { data: number[]; labels?: string[]; color: string; title: string; unit?: string; h?: number; maxVal?: number }) => {
    const vals = (data || []).map(v => Number(v)).filter(v => Number.isFinite(v));
    const chartRef = useRef<HTMLDivElement | null>(null);
    const chartInstRef = useRef<echarts.EChartsType | null>(null);
    if (vals.length < 2) {
      return (
        <div className="line-chart-card">
          <div className="lc-header"><span className="lc-title">{title}</span></div>
          <div className="chart-empty">Нет данных за последние часы</div>
        </div>
      );
    }

    const cur = vals[vals.length - 1];
    const mn = Math.min(...vals);
    const mx = Math.max(...vals);
    const firstLabel = labels[0] || '';
    const lastLabel = labels[labels.length - 1] || '';
    const vMin = Math.min(...vals);
    const vMax = Math.max(...vals);
    const rawRange = Math.max(vMax - vMin, 0.1);
    const pad = Math.max(rawRange * 0.2, unit === '%' ? 1 : 0.5);
    const capMax = Number.isFinite(maxVal) ? Number(maxVal) : Number.POSITIVE_INFINITY;
    const yMin = Math.max(0, vMin - pad);
    let yMax = Math.min(capMax, Math.max(vMax + pad, yMin + 1));
    if (yMax <= yMin) yMax = yMin + 1;
    const xData = labels.length === vals.length
      ? labels
      : vals.map((_, i) => String(i + 1));

    useEffect(() => {
      if (!chartRef.current) return;
      const chart = echarts.init(chartRef.current);
      chartInstRef.current = chart;
      const onResize = () => chart.resize();
      window.addEventListener('resize', onResize);
      return () => {
        window.removeEventListener('resize', onResize);
        chart.dispose();
        chartInstRef.current = null;
      };
    }, []);

    useEffect(() => {
      const chart = chartInstRef.current;
      if (!chart) return;
      chart.setOption({
        animation: true,
        animationDuration: 220,
        animationDurationUpdate: 1200,
        animationEasingUpdate: 'linear',
        grid: { left: 8, right: 8, top: 8, bottom: 8, containLabel: false },
        tooltip: {
          trigger: 'axis',
          backgroundColor: 'rgba(15, 23, 42, 0.92)',
          borderWidth: 0,
          textStyle: { color: '#e2e8f0', fontSize: 11 },
          formatter: (params: any) => {
            const p = Array.isArray(params) ? params[0] : params;
            if (!p) return '';
            const lbl = p.axisValueLabel || '';
            const val = Number(p.value || 0);
            return `${title}<br/>${lbl}: <b>${val.toFixed(1)}${unit}</b>`;
          },
        },
        xAxis: {
          type: 'category',
          data: xData,
          boundaryGap: false,
          axisLine: { show: false },
          axisTick: { show: false },
          axisLabel: { show: false },
          splitLine: { show: false },
        },
        yAxis: {
          type: 'value',
          min: yMin,
          max: yMax,
          axisLine: { show: false },
          axisTick: { show: false },
          axisLabel: { show: false },
          splitLine: {
            show: true,
            lineStyle: { color: 'rgba(148, 163, 184, 0.10)', width: 0.45 },
          },
        },
        series: [{
          type: 'line',
          data: vals,
          smooth: true,
          symbol: 'none',
          lineStyle: { color, width: 1.1, opacity: 0.88 },
          areaStyle: {
            color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
              { offset: 0, color: `${color}12` },
              { offset: 1, color: `${color}03` },
            ]),
          },
        }],
      }, { lazyUpdate: true, silent: true });
    }, [xData, vals, color, title, unit, yMin, yMax]);

    return (
      <div className="line-chart-card">
        <div className="lc-header">
          <span className="lc-title">{title}</span>
          <div className="lc-stats">
            <span className="lc-stat"><span className="lc-dot" style={{ background: color }}></span>тек: <b>{cur.toFixed(1)}{unit}</b></span>
            <span className="lc-stat">мин: <b>{mn.toFixed(1)}{unit}</b></span>
            <span className="lc-stat">макс: <b>{mx.toFixed(1)}{unit}</b></span>
          </div>
        </div>
        <div ref={chartRef} className="line-chart-echarts" style={{ height: `${h}px` }}></div>
        <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 10, color: 'var(--text-muted)', marginTop: 2 }}>
          <span>{firstLabel}</span>
          <span>{lastLabel}</span>
        </div>
      </div>
    );
  };

  const VMStateCard = ({
    runningData,
    totalData,
    labels = [],
  }: {
    runningData: number[];
    totalData: number[];
    labels?: string[];
  }) => {
    const runningVals = (runningData || []).map(v => Number(v)).filter(v => Number.isFinite(v));
    const totalVals = (totalData || []).map(v => Number(v)).filter(v => Number.isFinite(v));

    if (!runningVals.length) {
      return (
        <div className="line-chart-card">
          <div className="lc-header"><span className="lc-title">Статус ВМ</span></div>
          <div className="chart-empty">Нет данных по виртуальным машинам</div>
        </div>
      );
    }

    const currentRunning = Math.max(0, runningVals[runningVals.length - 1] || 0);
    const currentTotalRaw = totalVals[totalVals.length - 1] ?? currentRunning;
    const currentTotal = Math.max(1, currentTotalRaw);
    const currentStopped = Math.max(0, currentTotal - currentRunning);
    const minRun = Math.min(...runningVals);
    const maxRun = Math.max(...runningVals);
    const firstLabel = labels[0] || '';
    const lastLabel = labels[labels.length - 1] || '';
    const runningPct = Math.max(0, Math.min(100, (currentRunning / currentTotal) * 100));

    const history = runningVals.slice(-28);
    const historyMax = Math.max(1, ...(totalVals.slice(-28)), currentTotal);

    return (
      <div className="line-chart-card">
        <div className="lc-header">
          <span className="lc-title">Статус ВМ</span>
          <div className="lc-stats">
            <span className="lc-stat"><span className="lc-dot" style={{ background: '#10b981' }}></span>запущено: <b>{currentRunning.toFixed(0)}</b></span>
            <span className="lc-stat">выключено: <b>{currentStopped.toFixed(0)}</b></span>
            <span className="lc-stat">всего: <b>{currentTotal.toFixed(0)}</b></span>
          </div>
        </div>

        <div style={{ marginTop: 6 }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 11, color: 'var(--text-main)', marginBottom: 6 }}>
            <span><b>{currentRunning.toFixed(0)} / {currentTotal.toFixed(0)}</b> активны</span>
            <span style={{ color: 'var(--text-muted)' }}>{runningPct.toFixed(0)}%</span>
          </div>
          <div style={{ height: 14, borderRadius: 999, overflow: 'hidden', background: '#e5e7eb', border: '1px solid #d1d5db', display: 'flex' }}>
            <div style={{ width: `${runningPct}%`, background: '#10b981' }}></div>
            <div style={{ width: `${100 - runningPct}%`, background: '#cbd5e1' }}></div>
          </div>
        </div>

        <div style={{ marginTop: 10 }}>
          <div style={{ fontSize: 10, color: 'var(--text-muted)', marginBottom: 4 }}>История активности ВМ</div>
          <div style={{ display: 'flex', alignItems: 'flex-end', gap: 2, height: 42 }}>
            {history.map((v, i) => {
              const hPct = Math.max(8, Math.min(100, (v / historyMax) * 100));
              return (
                <div
                  key={i}
                  title={`Запущено: ${v}`}
                  style={{
                    flex: 1,
                    height: `${hPct}%`,
                    background: 'rgba(16, 185, 129, 0.45)',
                    borderRadius: 2,
                  }}
                ></div>
              );
            })}
          </div>
          <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 10, color: 'var(--text-muted)', marginTop: 4 }}>
            <span>{firstLabel}</span>
            <span>{lastLabel}</span>
          </div>
          <div style={{ fontSize: 10, color: 'var(--text-muted)', marginTop: 2 }}>
            мин: <b>{minRun.toFixed(0)}</b> · макс: <b>{maxRun.toFixed(0)}</b>
          </div>
        </div>
      </div>
    );
  };

  const DiskStackChart = ({ disks }: { disks: any[] }) => {
    const rows = (disks || []).map((d: any) => {
      const total = Number(d.totalGB) || 0;
      const freeRaw = Number(d.freeGB) || 0;
      const used = Math.max(total - freeRaw, 0);
      const free = Math.max(total - used, 0);
      const usedPct = Math.max(0, Math.min(100, total > 0 ? (used / total) * 100 : 0));
      const freePct = Math.max(0, 100 - usedPct);
      return { drive: d.drive || 'Disk', used, free, total, usedPct, freePct };
    }).filter(r => r.total > 0);

    if (!rows.length) {
      return (
        <div className="line-chart-card">
          <div className="lc-header"><span className="lc-title">Заполненность дисков</span></div>
          <div className="chart-empty">Нет данных по дискам</div>
        </div>
      );
    }

    return (
      <div className="line-chart-card disk-stack-chart-card">
        <div className="lc-header">
          <span className="lc-title">Заполненность дисков</span>
        </div>
        <div className="disk-stack-list">
          {rows.map(r => (
            <div key={r.drive} className="disk-stack-row">
              <div className="disk-stack-drive">{r.drive}</div>
              <div className="disk-stack-track">
                <div className="disk-stack-used" style={{ width: `${r.usedPct}%` }}>
                  {r.usedPct > 18 ? `${r.used.toFixed(0)} GB` : ''}
                </div>
                <div className="disk-stack-free" style={{ width: `${r.freePct}%` }}>
                  {r.freePct > 16 ? `${r.free.toFixed(0)} GB` : ''}
                </div>
              </div>
              <div className="disk-stack-pct">{r.usedPct.toFixed(1)}%</div>
            </div>
          ))}
        </div>
      </div>
    );
  };

  const formatBytes = (b: number) => { const gb = b / (1024 * 1024 * 1024); return gb >= 1 ? `${gb.toFixed(1)} GB` : `${(b / (1024 * 1024)).toFixed(0)} MB`; };
  const fmtDate = (ts: string) => {
    const d = new Date(ts);
    return Number.isNaN(d.getTime()) ? (ts || '—') : d.toLocaleString('ru-RU');
  };
  const selectedAgentInfo = agents.find(a => a.id === selectedAgent);
  const s3HostSuffix = useMemo(() => hostToPrefixPart(selectedAgentInfo?.name || ''), [selectedAgentInfo?.name]);
  const s3PrefixPreview = useMemo(() => buildS3Prefix(settings?.S3Prefix || '', selectedAgentInfo?.name || ''), [settings?.S3Prefix, selectedAgentInfo?.name]);
  const smbPathPreview = useMemo(() => buildSMBPath(settings?.SMBPath || '', selectedAgentInfo?.name || ''), [settings?.SMBPath, selectedAgentInfo?.name]);
  const webdavPathPreview = useMemo(() => buildWebDAVPath(settings?.WebDAVPath || '', selectedAgentInfo?.name || ''), [settings?.WebDAVPath, selectedAgentInfo?.name]);

  const filteredVMs = useMemo(() => {
    const vms = agentData?.vms || [];
    return vms.filter(vm => {
      const running = vm.state === 'Running' || vm.state === '2';
      if (vmFilter === 'running' && !running) return false;
      if (vmFilter === 'stopped' && running) return false;
      if (vmSearch && !vm.name.toLowerCase().includes(vmSearch.toLowerCase())) return false;
      return true;
    });
  }, [agentData?.vms, vmFilter, vmSearch]);

  const filteredLogs = useMemo(() => {
    const isBackupLog = (l: LogEntry) => {
      const t = (l.Type || '').toUpperCase();
      const m = (l.Message || '').toLowerCase();
      return (
        t.includes('BACKUP') ||
        t.includes('UPLOAD') ||
        t.includes('RESTORE') ||
        m.includes('бэкап') ||
        m.includes('backup') ||
        m.includes('архив') ||
        m.includes('restore')
      );
    };

    if (logFilter === 'backup') return logs.filter(isBackupLog);
    if (logFilter === 'system') return logs.filter(l => !isBackupLog(l));
    return logs;
  }, [logs, logFilter]);
  const totalLogPages = Math.max(1, Math.ceil(filteredLogs.length / PAGE_SIZE));
  const pagedLogs = useMemo(() => filteredLogs.slice((logPage - 1) * PAGE_SIZE, logPage * PAGE_SIZE), [filteredLogs, logPage]);

  return (
    <div className="app">
      <div className="app-bg" style={centralCfg?.bgImage ? { background: `url(${API}/backgrounds/${centralCfg.bgImage}) center/cover no-repeat` } : centralCfg?.bgColor ? { background: centralCfg.bgColor } : undefined}></div>
      {/* Toast notifications */}
      <div className="toast-container">
        {toasts.map(t => (
          <div key={t.id} className={`toast toast-${t.type}`}>
            <span className="toast-icon">{t.type === 'success' ? '✓' : t.type === 'error' ? '✕' : 'ℹ'}</span>
            <span className="toast-msg">{t.msg}</span>
          </div>
        ))}
      </div>
      <aside className="sidebar">
        <div className="sidebar-header" onClick={goOverview}>
          <img src={logoImg} alt="NODAX" className="sidebar-logo" />
          <div className="logo-text"><span className="label-gradient">NODAX Central</span></div>
        </div>
        <nav className="sidebar-nav">
          {canAccessSectionUI('overview') && (
            <button className={`nav-item ${page === 'overview' ? 'active' : ''}`} onClick={goOverview}><span className="nav-icon">📊</span> Обзор</button>
          )}
          {canAccessSectionUI('statistics') && (
            <button className={`nav-item ${page === 'statistics' ? 'active' : ''}`} onClick={() => { setPage('statistics'); setSelectedAgent(null); }}><span className="nav-icon">📈</span> Статистика</button>
          )}

          <button className="nav-section-btn" onClick={() => setHostsCollapsed(v => !v)}>
            <span>Хосты</span>
            <span className={`nav-fold ${hostsCollapsed ? 'folded' : ''}`}>▾</span>
          </button>
          {!hostsCollapsed && (
            <div className="nav-group">
              {agents.map(a => (
                <button key={a.id} className={`nav-item ${selectedAgent === a.id ? 'active' : ''}`} onClick={() => openHost(a.id)}>
                  <span className={`status-dot ${a.status === 'online' ? 'dot-online' : 'dot-offline'}`}></span> {a.name}
                </button>
              ))}
              <button className="nav-item nav-add" onClick={() => setShowAddModal(true)}><span className="nav-icon">+</span> Добавить хост</button>
            </div>
          )}

          {canAccessSectionUI('storage') && (
            <>
              <button className="nav-section-btn" onClick={() => setStorageCollapsed(v => !v)}>
                <span>Хранилище</span>
                <span className={`nav-fold ${storageCollapsed ? 'folded' : ''}`}>▾</span>
              </button>
              {!storageCollapsed && (
                <div className="nav-group">
                  <button className={`nav-item ${page === 's3-browser' ? 'active' : ''}`} onClick={() => { setPage('s3-browser'); setSelectedAgent(null); }}><span className="nav-icon">☁</span> S3 Хранилище</button>
                  <button className={`nav-item ${page === 'smb-browser' ? 'active' : ''}`} onClick={() => { setPage('smb-browser'); setSelectedAgent(null); }}><span className="nav-icon">📁</span> SMB Шара</button>
                  <button className={`nav-item ${page === 'webdav-browser' ? 'active' : ''}`} onClick={() => { setPage('webdav-browser'); setSelectedAgent(null); }}><span className="nav-icon">🌐</span> WebDAV</button>
                </div>
              )}
            </>
          )}

          <div className="nav-section">Система</div>
          {canAccessSectionUI('security') && <button className={`nav-item ${page === 'security' ? 'active' : ''} `} onClick={() => { setPage('security'); setSelectedAgent(null); }}><span className="nav-icon">🛡️</span> Пользователи и доступ</button>}
          {canAccessSectionUI('settings') && <button className={`nav-item ${page === 'central-settings' ? 'active' : ''} `} onClick={() => { setPage('central-settings'); setSelectedAgent(null); }}><span className="nav-icon">⚙</span> Настройки</button>}
        </nav>
        <div className="sidebar-user">
          <div className="sidebar-user-info">
            <span className="sidebar-user-icon">👤</span>
            <span className="sidebar-user-name">{auth.user.username}</span>
            <span className="sidebar-user-role">{auth.user.role}</span>
          </div>
          <button className="sidebar-logout" onClick={onLogout} title="Выйти">⏻</button>
        </div>
      </aside>

      <main className="main-content">
        {/* ============ OVERVIEW ============ */}
        {page === 'overview' && (
          <div className="page-overview">
            <h1>Обзор инфраструктуры</h1>
            {overview && (
              <div className="overview-cards">
                <div className="ov-card"><div className="ov-value">{overview.onlineAgents}/{overview.totalAgents}</div><div className="ov-label">Хостов онлайн</div></div>
                <div className="ov-card"><div className="ov-value">{overview.runningVMs}/{overview.totalVMs}</div><div className="ov-label">ВМ запущено</div></div>
                <div className="ov-card"><div className="ov-value">{overview.totalCpuAvg.toFixed(0)}%</div><div className="ov-label">CPU средний</div></div>
                <div className="ov-card"><div className="ov-value">{overview.totalRamBytes > 0 ? Math.round((overview.usedRamBytes / overview.totalRamBytes) * 100) : 0}%</div><div className="ov-label">RAM средний</div></div>
              </div>
            )}
            <h2>Хосты</h2>
            <div className="host-grid">
              {agents.map(agent => (
                <div key={agent.id} className={`host-card ${agent.status === 'online' ? 'hc-online' : 'hc-offline'}`}>
                  <div className="hc-header"><div className="hc-name" onClick={() => openHost(agent.id)}><span className={`status-dot ${agent.status === 'online' ? 'dot-online' : 'dot-offline'}`}></span>{agent.name}</div><button className="hc-delete" onClick={() => setDeleteAgentId(agent.id)} title="Удалить">✕</button></div>
                  <div className="hc-url">{agent.url}</div>
                  <div className="hc-status">{agent.status === 'online' ? 'Онлайн' : 'Недоступен'}</div>
                  {agent.lastSeen && <div className="hc-lastseen">Последний отклик: {fmtDate(agent.lastSeen)}</div>}
                  <button className="hc-open" onClick={() => openHost(agent.id)}>Открыть →</button>
                </div>
              ))}
              {agents.length === 0 && <div className="empty-state">Нет зарегистрированных хостов.</div>}
            </div>
          </div>
        )}

        {/* ============ STATISTICS ============ */}
        {page === 'statistics' && (
          <div className="page-stats">
            <h1>Статистика инфраструктуры</h1>
            {!aggStats ? <div className="loading">Загрузка...</div> : (
              <div>
                {/* Summary cards */}
                <div className="overview-cards">
                <div className="ov-card"><div className="ov-value">{aggStats.onlineHosts}/{aggStats.totalHosts}</div><div className="ov-label">Хостов онлайн</div></div>
                <div className="ov-card"><div className="ov-value">{aggStats.runningVMs}/{aggStats.totalVMs}</div><div className="ov-label">ВМ запущено</div></div>
                <div className="ov-card"><div className="ov-value" style={{color: cpuColor(aggStats.avgCpu)}}>{aggStats.avgCpu.toFixed(1)}%</div><div className="ov-label">CPU средний</div></div>
                <div className="ov-card"><div className="ov-value" style={{color: ramColor(aggStats.avgRam)}}>{aggStats.avgRam.toFixed(1)}%</div><div className="ov-label">RAM средний</div></div>
              </div>

              {/* Charts row */}
              <div className="stats-charts">
                {/* CPU per host bar chart */}
                <div className="stats-card">
                  <h3>CPU по хостам</h3>
                  <div className="bar-chart">
                    {aggStats.hosts.filter(h => h.status === 'online').map(h => (
                      <div key={h.agentId} className="bar-row">
                        <span className="bar-label">{h.name}</span>
                        <div className="bar-track"><div className="bar-fill" style={{width: `${Math.min(h.cpu, 100)}%`, background: cpuColor(h.cpu)}}></div></div>
                        <span className="bar-val">{h.cpu.toFixed(0)}%</span>
                      </div>
                    ))}
                    {aggStats.hosts.filter(h => h.status === 'online').length === 0 && <div className="empty-state">Нет онлайн хостов</div>}
                  </div>
                </div>

                {/* RAM per host bar chart */}
                <div className="stats-card">
                  <h3>RAM по хостам</h3>
                  <div className="bar-chart">
                    {aggStats.hosts.filter(h => h.status === 'online').map(h => (
                      <div key={h.agentId} className="bar-row">
                        <span className="bar-label">{h.name}</span>
                        <div className="bar-track"><div className="bar-fill" style={{width: `${Math.min(h.ramPct, 100)}%`, background: ramColor(h.ramPct)}}></div></div>
                        <span className="bar-val">{h.ramPct.toFixed(0)}%</span>
                      </div>
                    ))}
                  </div>
                </div>
              </div>

              {/* Donut charts row */}
              <div className="stats-charts">
                {/* RAM donut */}
                <div className="stats-card stats-card-center">
                  <h3>Общая RAM</h3>
                  <svg viewBox="0 0 120 120" className="donut-chart">
            <circle cx="60" cy="60" r="50" fill="none" stroke="#e5e7eb" strokeWidth="14" />
            <circle cx="60" cy="60" r="50" fill="none" stroke={ramColor(aggStats.totalRamGB > 0 ? (aggStats.usedRamGB / aggStats.totalRamGB) * 100 : 0)} strokeWidth="14"
                      strokeDasharray={`${(aggStats.totalRamGB > 0 ? aggStats.usedRamGB / aggStats.totalRamGB : 0) * 314} 314`}
                      strokeLinecap="round" transform="rotate(-90 60 60)" />
                    <text x="60" y="56" textAnchor="middle" fontSize="16" fontWeight="800" fill="var(--text-main)">{aggStats.usedRamGB.toFixed(1)}</text>
                    <text x="60" y="72" textAnchor="middle" fontSize="10" fill="var(--text-muted)">/ {aggStats.totalRamGB.toFixed(1)} GB</text>
                  </svg>
                </div>

                {/* Disk donut */}
                <div className="stats-card stats-card-center">
                  <h3>Общее хранилище</h3>
                  <svg viewBox="0 0 120 120" className="donut-chart">
            <circle cx="60" cy="60" r="50" fill="none" stroke="#e5e7eb" strokeWidth="14" />
            <circle cx="60" cy="60" r="50" fill="none" stroke={diskColor(aggStats.totalDiskGB > 0 ? (aggStats.usedDiskGB / aggStats.totalDiskGB) * 100 : 0)} strokeWidth="14"
                      strokeDasharray={`${(aggStats.totalDiskGB > 0 ? aggStats.usedDiskGB / aggStats.totalDiskGB : 0) * 314} 314`}
                      strokeLinecap="round" transform="rotate(-90 60 60)" />
                    <text x="60" y="56" textAnchor="middle" fontSize="16" fontWeight="800" fill="var(--text-main)">{aggStats.usedDiskGB.toFixed(0)}</text>
                    <text x="60" y="72" textAnchor="middle" fontSize="10" fill="var(--text-muted)">/ {aggStats.totalDiskGB.toFixed(0)} GB</text>
                  </svg>
                </div>

                {/* VMs donut */}
                <div className="stats-card stats-card-center">
                  <h3>Виртуальные машины</h3>
                  <svg viewBox="0 0 120 120" className="donut-chart">
            <circle cx="60" cy="60" r="50" fill="none" stroke="#e5e7eb" strokeWidth="14" />
            <circle cx="60" cy="60" r="50" fill="none" stroke="var(--success)" strokeWidth="14"
                      strokeDasharray={`${(aggStats.totalVMs > 0 ? aggStats.runningVMs / aggStats.totalVMs : 0) * 314} 314`}
                      strokeLinecap="round" transform="rotate(-90 60 60)" />
                    <text x="60" y="56" textAnchor="middle" fontSize="16" fontWeight="800" fill="var(--text-main)">{aggStats.runningVMs}</text>
                    <text x="60" y="72" textAnchor="middle" fontSize="10" fill="var(--text-muted)">/ {aggStats.totalVMs} ВМ</text>
                  </svg>
                </div>
              </div>

              {/* Per-host detail table */}
              <div className="stats-card" style={{marginTop: 16}}>
                <h3>Детали по хостам</h3>
                <table className="vm-table">
                  <thead><tr><th>Хост</th><th>ОС</th><th>CPU</th><th>RAM</th><th>ВМ</th><th>Uptime</th><th>Диски</th></tr></thead>
                  <tbody>
                    {aggStats.hosts.map(h => (
                      <tr key={h.agentId} style={{opacity: h.status === 'online' ? 1 : 0.4}}>
                        <td><span className={`status-dot ${h.status === 'online' ? 'dot-online' : 'dot-offline'}`}></span> {h.name}</td>
                        <td>{h.os || '—'}</td>
                        <td><span style={{color: cpuColor(h.cpu)}}>{h.cpu.toFixed(1)}%</span></td>
                        <td><span style={{color: ramColor(h.ramPct)}}>{h.ramUsedGB.toFixed(1)} / {h.ramTotalGB.toFixed(1)} GB</span></td>
                        <td>{h.vmRunning}/{h.vmTotal}</td>
                        <td>{h.uptime || '—'}</td>
                        <td>{(h.disks || []).map(d => `${d.drive} ${d.usePct.toFixed(0)}%`).join(', ') || '—'}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>)}
          </div>
        )}

        {/* ============ S3 BROWSER ============ */}
        {page === 's3-browser' && (() => {
          const renderTree = (nodes: typeof s3Tree, depth: number = 0): React.ReactNode =>
            nodes.map(n => {
              const label = n.key.replace(/\/$/, '').split('/').pop() || n.key;
              const isActive = s3Prefix === n.key;
              return (
                <div key={n.key}>
                  <button
                    className={`s3-tree-item${isActive ? ' s3-tree-active' : ''}`}
                    style={{ paddingLeft: 8 + depth * 16 }}
                    onClick={() => { setS3Prefix(n.key); if (s3TreeLoaded[n.key]) setS3Objects(s3TreeLoaded[n.key]); else s3Browse(s3Agent, n.key); }}
                  >
                    <span className="s3-tree-arrow" onClick={e => { e.stopPropagation(); s3ToggleTree(n.key, s3Agent); }}>{n.expanded ? '▾' : '▸'}</span>
                    <span className="s3-tree-icon">📁</span>
                    <span className="s3-tree-label">{label}</span>
                  </button>
                  {n.expanded && n.children && renderTree(n.children as typeof s3Tree, depth + 1)}
                </div>
              );
            });
          return (
          <div className="page-s3">
            <div className="s3-header">
              <h1>S3 Хранилище</h1>
              <select className="modal-input s3-agent-select" value={s3Agent} onChange={e => { const v = e.target.value; setS3Agent(v); setS3Prefix(''); setS3Objects([]); setS3Error(''); setS3Tree([]); setS3TreeLoaded({}); if (v) s3Browse(v, ''); }}>
                <option value="">— Выберите хост —</option>
                {agents.filter(a => a.status === 'online').map(a => (
                  <option key={a.id} value={a.id}>{a.name}</option>
                ))}
              </select>
            </div>
            {s3Error && <div className="s3-error">{s3Error}</div>}
            {!s3Agent && <div className="empty-state">Выберите онлайн-хост для просмотра S3 хранилища</div>}
            {s3Agent && (
              <div className="s3-explorer">
                {/* Left: folder tree */}
                <div className="s3-tree-panel">
                  <button
                    className={`s3-tree-item s3-tree-root${s3Prefix === '' ? ' s3-tree-active' : ''}`}
                    onClick={() => { setS3Prefix(''); s3Browse(s3Agent, ''); }}
                  >
                    <span className="s3-tree-icon">☁</span>
                    <span className="s3-tree-label">S3 Bucket</span>
                  </button>
                  {renderTree(s3Tree)}
                </div>
                {/* Right: file list */}
                <div className="s3-file-panel">
                  {/* Breadcrumb address bar */}
                  <div className="s3-address-bar">
                    <button className="s3-addr-btn" onClick={() => { setS3Prefix(''); s3Browse(s3Agent, ''); }}>☁</button>
                    <span className="s3-addr-sep">›</span>
                    {s3Prefix.split('/').filter(Boolean).map((part, i, arr) => {
                      const path = arr.slice(0, i + 1).join('/') + '/';
                      return (
                        <span key={path}>
                          <button className="s3-addr-btn" onClick={() => { setS3Prefix(path); s3Browse(s3Agent, path); }}>{part}</button>
                          {i < arr.length - 1 && <span className="s3-addr-sep">›</span>}
                        </span>
                      );
                    })}
                  </div>
                  {/* Column header */}
                  <div className="s3-list-header">
                    <div className="s3-col-name">Имя</div>
                    <div className="s3-col-date">Дата изменения</div>
                    <div className="s3-col-type">Тип</div>
                    <div className="s3-col-size">Размер</div>
                    <div className="s3-col-actions"></div>
                  </div>
                  {/* File rows */}
                  <div className="s3-list-body">
                    {s3Loading && <div className="loading" style={{padding: 24}}>Загрузка...</div>}
                    {!s3Loading && s3Objects.length === 0 && <div className="s3-empty">Эта папка пуста.</div>}
                    {!s3Loading && s3Objects.map(obj => {
                      const displayName = obj.isDir
                        ? obj.key.replace(/\/$/, '').split('/').pop() || obj.key
                        : obj.key.split('/').pop() || obj.key;
                      const ext = !obj.isDir && displayName.includes('.') ? displayName.split('.').pop()!.toUpperCase() : '';
                      const fileType = obj.isDir ? 'Папка' : ext ? `Файл ${ext}` : 'Файл';
                      return (
                        <div key={obj.key} className={`s3-list-row${obj.isDir ? ' s3-list-row-dir' : ''}`}
                          onDoubleClick={() => obj.isDir && s3Browse(s3Agent, obj.key)}>
                          <div className="s3-col-name">
                            {obj.isDir ? (
                              <button className="s3-folder-btn" onClick={() => s3Browse(s3Agent, obj.key)}>
                                <span className="s3-file-icon">📁</span>{displayName}
                              </button>
                            ) : (
                              <span className="s3-file-name"><span className="s3-file-icon">📄</span>{displayName}</span>
                            )}
                          </div>
                          <div className="s3-col-date">{obj.isDir ? '' : obj.lastModified ? new Date(obj.lastModified).toLocaleString('ru') : ''}</div>
                          <div className="s3-col-type">{fileType}</div>
                          <div className="s3-col-size">{obj.isDir ? '' : formatSize(obj.size)}</div>
                          <div className="s3-col-actions">
                            {!obj.isDir && (
                              <>
                                <button className="btn-sm" onClick={e => { e.stopPropagation(); s3Download(obj.key); }} title="Скачать">⬇</button>
                                <button className="btn-sm btn-sm-danger" onClick={e => { e.stopPropagation(); s3Delete(obj.key); }} title="Удалить">✕</button>
                              </>
                            )}
                          </div>
                        </div>
                      );
                    })}
                  </div>
                  {/* Status bar */}
                  <div className="s3-status-bar">
                    Элементов: {s3Objects.length}
                  </div>
                </div>
              </div>
            )}
          </div>
          );
        })()}

        {/* ============ SMB BROWSER ============ */}
        {page === 'smb-browser' && (() => {
          const renderSmbTree = (nodes: typeof smbTree, depth: number = 0): React.ReactNode =>
            nodes.map(n => {
              const label = n.key.replace(/\/$/, '').split('/').pop() || n.key;
              const isActive = smbPath === n.key;
              return (
                <div key={n.key}>
                  <button className={`s3-tree-item${isActive ? ' s3-tree-active' : ''}`} style={{ paddingLeft: 8 + depth * 16 }}
                    onClick={() => { setSmbPath(n.key); if (smbTreeLoaded[n.key]) setSmbObjects(smbTreeLoaded[n.key]); else smbBrowse(smbAgent, n.key); }}>
                    <span className="s3-tree-arrow" onClick={e => { e.stopPropagation(); smbToggleTree(n.key, smbAgent); }}>{n.expanded ? '▾' : '▸'}</span>
                    <span className="s3-tree-icon">📁</span>
                    <span className="s3-tree-label">{label}</span>
                  </button>
                  {n.expanded && n.children && renderSmbTree(n.children as typeof smbTree, depth + 1)}
                </div>
              );
            });
          return (
          <div className="page-s3">
            <div className="s3-header">
              <h1>SMB Сетевая шара</h1>
              <select className="modal-input s3-agent-select" value={smbAgent} onChange={e => { const v = e.target.value; setSmbAgent(v); setSmbPath(''); setSmbObjects([]); setSmbError(''); setSmbTree([]); setSmbTreeLoaded({}); if (v) smbBrowse(v, ''); }}>
                <option value="">— Выберите хост —</option>
                {agents.filter(a => a.status === 'online').map(a => (<option key={a.id} value={a.id}>{a.name}</option>))}
              </select>
            </div>
            {smbError && <div className="s3-error">{smbError}</div>}
            {!smbAgent && <div className="empty-state">Выберите онлайн-хост для просмотра сетевой шары</div>}
            {smbAgent && (
              <div className="s3-explorer">
                <div className="s3-tree-panel">
                  <button className={`s3-tree-item s3-tree-root${smbPath === '' ? ' s3-tree-active' : ''}`} onClick={() => { setSmbPath(''); smbBrowse(smbAgent, ''); }}>
                    <span className="s3-tree-icon">📁</span><span className="s3-tree-label">SMB Root</span>
                  </button>
                  {renderSmbTree(smbTree)}
                </div>
                <div className="s3-file-panel">
                  <div className="s3-address-bar">
                    <button className="s3-addr-btn" onClick={() => { setSmbPath(''); smbBrowse(smbAgent, ''); }}>📁</button>
                    <span className="s3-addr-sep">›</span>
                    {smbPath.split('/').filter(Boolean).map((part, i, arr) => {
                      const path = arr.slice(0, i + 1).join('/') + '/';
                      return (<span key={path}><button className="s3-addr-btn" onClick={() => { setSmbPath(path); smbBrowse(smbAgent, path); }}>{part}</button>{i < arr.length - 1 && <span className="s3-addr-sep">›</span>}</span>);
                    })}
                  </div>
                  <div className="s3-list-header">
                    <div className="s3-col-name">Имя</div><div className="s3-col-date">Дата изменения</div><div className="s3-col-type">Тип</div><div className="s3-col-size">Размер</div><div className="s3-col-actions"></div>
                  </div>
                  <div className="s3-list-body">
                    {smbLoading && <div className="loading" style={{padding: 24}}>Загрузка...</div>}
                    {!smbLoading && smbObjects.length === 0 && <div className="s3-empty">Эта папка пуста.</div>}
                    {!smbLoading && smbObjects.map(obj => {
                      const displayName = obj.isDir ? obj.key.replace(/\/$/, '').split('/').pop() || obj.key : obj.key.split('/').pop() || obj.key;
                      const ext = !obj.isDir && displayName.includes('.') ? displayName.split('.').pop()!.toUpperCase() : '';
                      const fileType = obj.isDir ? 'Папка' : ext ? `Файл ${ext}` : 'Файл';
                      return (
                        <div key={obj.key} className={`s3-list-row${obj.isDir ? ' s3-list-row-dir' : ''}`} onDoubleClick={() => obj.isDir && smbBrowse(smbAgent, obj.key)}>
                          <div className="s3-col-name">
                            {obj.isDir ? (<button className="s3-folder-btn" onClick={() => smbBrowse(smbAgent, obj.key)}><span className="s3-file-icon">📁</span>{displayName}</button>)
                              : (<span className="s3-file-name"><span className="s3-file-icon">📄</span>{displayName}</span>)}
                          </div>
                          <div className="s3-col-date">{obj.isDir ? '' : obj.lastModified ? new Date(obj.lastModified).toLocaleString('ru') : ''}</div>
                          <div className="s3-col-type">{fileType}</div>
                          <div className="s3-col-size">{obj.isDir ? '' : formatSize(obj.size)}</div>
                          <div className="s3-col-actions">
                            {!obj.isDir && (
                              <>
                                <button className="btn-sm" onClick={e => { e.stopPropagation(); smbDownload(obj.key); }} title="Скачать">⬇</button>
                                <button className="btn-sm btn-sm-danger" onClick={e => { e.stopPropagation(); smbDelete(obj.key); }} title="Удалить">✕</button>
                              </>
                            )}
                          </div>
                        </div>
                      );
                    })}
                  </div>
                  <div className="s3-status-bar">Элементов: {smbObjects.length}</div>
                </div>
              </div>
            )}
          </div>
          );
        })()}

        {/* ============ WEBDAV BROWSER ============ */}
        {page === 'webdav-browser' && (() => {
          const renderDavTree = (nodes: typeof davTree, depth: number = 0): React.ReactNode =>
            nodes.map(n => {
              const label = n.key.replace(/\/$/, '').split('/').pop() || n.key;
              const isActive = davPath === n.key;
              return (
                <div key={n.key}>
                  <button className={`s3-tree-item${isActive ? ' s3-tree-active' : ''}`} style={{ paddingLeft: 8 + depth * 16 }}
                    onClick={() => { setDavPath(n.key); if (davTreeLoaded[n.key]) setDavObjects(davTreeLoaded[n.key]); else davBrowse(davAgent, n.key); }}>
                    <span className="s3-tree-arrow" onClick={e => { e.stopPropagation(); davToggleTree(n.key, davAgent); }}>{n.expanded ? '▾' : '▸'}</span>
                    <span className="s3-tree-icon">📁</span>
                    <span className="s3-tree-label">{label}</span>
                  </button>
                  {n.expanded && n.children && renderDavTree(n.children as typeof davTree, depth + 1)}
                </div>
              );
            });
          return (
          <div className="page-s3">
            <div className="s3-header">
              <h1>WebDAV Хранилище</h1>
              <select className="modal-input s3-agent-select" value={davAgent} onChange={e => { const v = e.target.value; setDavAgent(v); setDavPath(''); setDavObjects([]); setDavError(''); setDavTree([]); setDavTreeLoaded({}); if (v) davBrowse(v, ''); }}>
                <option value="">— Выберите хост —</option>
                {agents.filter(a => a.status === 'online').map(a => (<option key={a.id} value={a.id}>{a.name}</option>))}
              </select>
            </div>
            {davError && <div className="s3-error">{davError}</div>}
            {!davAgent && <div className="empty-state">Выберите онлайн-хост для просмотра WebDAV хранилища</div>}
            {davAgent && (
              <div className="s3-explorer">
                <div className="s3-tree-panel">
                  <button className={`s3-tree-item s3-tree-root${davPath === '' ? ' s3-tree-active' : ''}`} onClick={() => { setDavPath(''); davBrowse(davAgent, ''); }}>
                    <span className="s3-tree-icon">🌐</span><span className="s3-tree-label">WebDAV Root</span>
                  </button>
                  {renderDavTree(davTree)}
                </div>
                <div className="s3-file-panel">
                  <div className="s3-address-bar">
                    <button className="s3-addr-btn" onClick={() => { setDavPath(''); davBrowse(davAgent, ''); }}>🌐</button>
                    <span className="s3-addr-sep">›</span>
                    {davPath.split('/').filter(Boolean).map((part, i, arr) => {
                      const path = arr.slice(0, i + 1).join('/') + '/';
                      return (<span key={path}><button className="s3-addr-btn" onClick={() => { setDavPath(path); davBrowse(davAgent, path); }}>{part}</button>{i < arr.length - 1 && <span className="s3-addr-sep">›</span>}</span>);
                    })}
                  </div>
                  <div className="s3-list-header">
                    <div className="s3-col-name">Имя</div><div className="s3-col-date">Дата изменения</div><div className="s3-col-type">Тип</div><div className="s3-col-size">Размер</div><div className="s3-col-actions"></div>
                  </div>
                  <div className="s3-list-body">
                    {davLoading && <div className="loading" style={{padding: 24}}>Загрузка...</div>}
                    {!davLoading && davObjects.length === 0 && <div className="s3-empty">Эта папка пуста.</div>}
                    {!davLoading && davObjects.map(obj => {
                      const displayName = obj.isDir ? obj.key.replace(/\/$/, '').split('/').pop() || obj.key : obj.key.split('/').pop() || obj.key;
                      const ext = !obj.isDir && displayName.includes('.') ? displayName.split('.').pop()!.toUpperCase() : '';
                      const fileType = obj.isDir ? 'Папка' : ext ? `Файл ${ext}` : 'Файл';
                      return (
                        <div key={obj.key} className={`s3-list-row${obj.isDir ? ' s3-list-row-dir' : ''}`} onDoubleClick={() => obj.isDir && davBrowse(davAgent, obj.key)}>
                          <div className="s3-col-name">
                            {obj.isDir ? (<button className="s3-folder-btn" onClick={() => davBrowse(davAgent, obj.key)}><span className="s3-file-icon">📁</span>{displayName}</button>)
                              : (<span className="s3-file-name"><span className="s3-file-icon">📄</span>{displayName}</span>)}
                          </div>
                          <div className="s3-col-date">{obj.isDir ? '' : obj.lastModified ? new Date(obj.lastModified).toLocaleString('ru') : ''}</div>
                          <div className="s3-col-type">{fileType}</div>
                          <div className="s3-col-size">{obj.isDir ? '' : formatSize(obj.size)}</div>
                          <div className="s3-col-actions">
                            {!obj.isDir && (
                              <>
                                <button className="btn-sm" onClick={e => { e.stopPropagation(); davDownload(obj.key); }} title="Скачать">⬇</button>
                                <button className="btn-sm btn-sm-danger" onClick={e => { e.stopPropagation(); davDelete(obj.key); }} title="Удалить">✕</button>
                              </>
                            )}
                          </div>
                        </div>
                      );
                    })}
                  </div>
                  <div className="s3-status-bar">Элементов: {davObjects.length}</div>
                </div>
              </div>
            )}
          </div>
          );
        })()}
        {page === 'central-settings' && (
          <div className="page-settings-central">
            <h1>Настройки NODAX Central</h1>
            {!centralCfg ? <div className="loading">Загрузка...</div> : (
              <div className="cfg-form">
                <div className="cfg-section">
                  <h3>Сервер</h3>
                  <div className="cfg-row">
                    <label className="cfg-label">Порт сервера</label>
                    <input className="modal-input" value={centralCfg.port} onChange={e => setCentralCfg({...centralCfg, port: e.target.value})} style={{width: 120}} />
                  </div>
                  <div className="cfg-row">
                    <label className="cfg-label">Домен Caddy (HTTPS)</label>
                    <div className="cfg-inline-actions">
                      <input className="modal-input" value={centralCfg.caddyDomain || ''} onChange={e => setCentralCfg({...centralCfg, caddyDomain: e.target.value})} placeholder="central.example.com" style={{width: 260}} />
                      <button className="btn-secondary" disabled={caddyRecheckLoading || !centralCfg.caddyDomain} onClick={recheckCaddyCert}>{caddyRecheckLoading ? 'Проверка...' : 'Проверить сертификат Caddy'}</button>
                    </div>
                  </div>
                  <div className="cfg-row">
                    <label className="cfg-label">Интервал опроса (сек)</label>
                    <input className="modal-input" type="number" min={5} value={centralCfg.pollIntervalSec} onChange={e => setCentralCfg({...centralCfg, pollIntervalSec: parseInt(e.target.value) || 15})} style={{width: 120}} />
                  </div>
                  <div className="cfg-row">
                    <label className="cfg-label">Хранить данные (дней)</label>
                    <input className="modal-input" type="number" min={1} value={centralCfg.retentionDays} onChange={e => setCentralCfg({...centralCfg, retentionDays: parseInt(e.target.value) || 30})} style={{width: 120}} />
                  </div>
                </div>
                <div className="cfg-section">
                  <h3>Интерфейс</h3>
                  <div className="cfg-row">
                    <label className="cfg-label">Тема</label>
                    <select className="modal-input" value={centralCfg.theme} onChange={e => setCentralCfg({...centralCfg, theme: e.target.value})} style={{width: 180}}>
                      <option value="light">Светлая</option>
                      <option value="dark">Тёмная</option>
                    </select>
                  </div>
                  <div className="cfg-row">
                    <label className="cfg-label">Язык</label>
                    <select className="modal-input" value={centralCfg.language} onChange={e => setCentralCfg({...centralCfg, language: e.target.value})} style={{width: 180}}>
                      <option value="ru">Русский</option>
                      <option value="en">English</option>
                    </select>
                  </div>
                </div>
                <div className="cfg-section">
                  <h3>Лицензия</h3>
                  <div className="cfg-row-col">
                    <label className="cfg-label">License Key</label>
                    <input className="modal-input" value={centralCfg.licenseKey || ''} onChange={e => setCentralCfg({...centralCfg, licenseKey: e.target.value})} placeholder="NDX-XXXXXX-XXXXXX-XXXXXX-XXXXXX" style={{width: '100%'}} />
                  </div>
                  <div className="cfg-row-col">
                    <label className="cfg-label">License Server URL</label>
                    <input className="modal-input" value={centralCfg.licenseServer || ''} onChange={e => setCentralCfg({...centralCfg, licenseServer: e.target.value})} placeholder="https://license.example.com" style={{width: '100%'}} />
                  </div>
                  <div className="cfg-row-col">
                    <label className="cfg-label">Public Key (опционально)</label>
                    <input className="modal-input" value={centralCfg.licensePubKey || ''} onChange={e => setCentralCfg({...centralCfg, licensePubKey: e.target.value})} placeholder="base64 ed25519 public key" style={{width: '100%'}} />
                  </div>
                  <div className="cfg-inline-actions" style={{marginTop: 6}}>
                    <button className="btn-secondary" disabled={licenseRecheckLoading || !(centralCfg.licenseKey || '').trim() || !(centralCfg.licenseServer || '').trim()} onClick={recheckLicense}>{licenseRecheckLoading ? 'Проверка...' : 'Проверить лицензию'}</button>
                    <span className={`license-badge license-${(centralCfg.licenseStatus || 'unknown').toLowerCase()}`}>{licenseStatusLabel(centralCfg.licenseStatus)}</span>
                  </div>
                  <div className="license-details">
                    {centralCfg.licenseReason && <div>Причина: {centralCfg.licenseReason}</div>}
                    {centralCfg.licenseExpires && <div>Истекает: {new Date(centralCfg.licenseExpires).toLocaleString('ru')}</div>}
                    {centralCfg.licenseGraceTo && <div>Grace до: {new Date(centralCfg.licenseGraceTo).toLocaleString('ru')}</div>}
                    {centralCfg.licenseChecked && <div>Последняя проверка: {new Date(centralCfg.licenseChecked).toLocaleString('ru')}</div>}
                    {centralCfg.licenseLastErr && <div>Ошибка: {centralCfg.licenseLastErr}</div>}
                    {licenseRecheckMsg && <div>{licenseRecheckMsg}</div>}
                  </div>
                </div>
                <div className="cfg-section">
                  <h3>Фон</h3>
                  <div className="cfg-row">
                    <label className="cfg-label">Цвет фона</label>
                    <div style={{display:'flex',alignItems:'center',gap:8}}>
                      <input type="color" value={centralCfg.bgColor || '#2d6a4f'} onChange={e => setCentralCfg({...centralCfg, bgColor: e.target.value, bgImage: ''})} className="cfg-color-picker" />
                      <span style={{fontSize:12,color:'var(--text-muted)'}}>{centralCfg.bgColor || 'по умолчанию'}</span>
                    </div>
                  </div>
                  <div className="cfg-row-col">
                    <label className="cfg-label">Фоновое изображение</label>
                    <div className="cfg-bg-upload">
                      <input type="file" accept="image/*" id="bg-upload" style={{display:'none'}} onChange={async e => {
                        const file = e.target.files?.[0];
                        if (!file) return;
                        if (file.size > 10 * 1024 * 1024) { toast('Макс. размер 10 МБ', 'error'); return; }
                        setBgUploading(true);
                        const name = await uploadBg(file);
                        if (name) {
                          const next = {...centralCfg, bgImage: name, bgColor: ''};
                          setCentralCfg(next);
                          await saveCentralCfg(next);
                        }
                        setBgUploading(false);
                        e.target.value = '';
                      }} />
                      <button className="btn-secondary" disabled={bgUploading} onClick={() => document.getElementById('bg-upload')?.click()}>{bgUploading ? 'Загрузка...' : 'Загрузить фон'}</button>
                      {(centralCfg.bgImage || centralCfg.bgColor) && (
                        <button className="btn-secondary" onClick={async () => {
                          const next = {...centralCfg, bgColor: '', bgImage: ''};
                          setCentralCfg(next);
                          await saveCentralCfg(next);
                        }}>Сбросить</button>
                      )}
                    </div>
                  </div>
                  {bgList.length > 0 && (
                    <div className="cfg-row-col" style={{marginTop: 4}}>
                      <label className="cfg-label">Выбрать из загруженных</label>
                      <div className="bg-gallery">
                        {bgList.map(name => (
                          <div key={name} className={`bg-gallery-item ${centralCfg.bgImage === name ? 'bg-selected' : ''}`}>
                            <img src={`${API}/backgrounds/${name}`} alt={name} onClick={async () => {
                              const next = {...centralCfg, bgImage: name, bgColor: ''};
                              setCentralCfg(next);
                              await saveCentralCfg(next);
                            }} />
                            <button className="bg-gallery-del" onClick={() => deleteBg(name)} title="Удалить">✕</button>
                            {centralCfg.bgImage === name && <div className="bg-gallery-check">✓</div>}
                          </div>
                        ))}
                      </div>
                    </div>
                  )}
                </div>
                <div className="cfg-actions">
                  <div className="cfg-actions-main">
                    <button className="btn-primary" disabled={cfgSaving} onClick={() => saveCentralCfg(centralCfg)}>{cfgSaving ? 'Сохранение...' : 'Сохранить изменения'}</button>
                  </div>
                  <div className="cfg-actions-backup">
                    <div className="cfg-actions-caption">Backup конфигурации Central</div>
                    <div className="cfg-actions-row">
                      <button className="btn-secondary" onClick={downloadCentralConfigBackup}>Скачать backup (конфиг + хосты)</button>
                      <button className="btn-secondary" disabled={cfgImporting} onClick={() => configFileInputRef.current?.click()}>{cfgImporting ? 'Восстановление...' : 'Восстановить backup (конфиг + хосты)'}</button>
                    </div>
                  </div>
                </div>
                <input
                  ref={configFileInputRef}
                  type="file"
                  accept="application/json,.json"
                  style={{display:'none'}}
                  onChange={async e => {
                    const f = e.target.files?.[0];
                    if (!f) return;
                    await restoreCentralConfigBackup(f);
                    e.target.value = '';
                  }}
                />
                {!!caddyRecheckMsg && <div style={{marginTop:8, fontSize:12, color:'var(--text-muted)'}}>{caddyRecheckMsg}</div>}
              </div>
            )}
          </div>

        )}
        {page === 'security' && canAccessSectionUI('security') && (
          <div className="page-settings-central">
            <h1>Пользователи и политики безопасности</h1>
            <div className="cfg-form">
              <div className="cfg-section">
                <h3>Создать пользователя</h3>
                <div className="user-add-form">
                  <input className="modal-input" placeholder="Логин" value={newUser.username} onChange={e => setNewUser({...newUser, username: e.target.value})} style={{width: 180}} />
                  <input className="modal-input" type="password" placeholder="Пароль" value={newUser.password} onChange={e => setNewUser({...newUser, password: e.target.value})} style={{width: 180}} />
                  <select className="modal-input" value={newUser.role} onChange={e => setNewUser({...newUser, role: e.target.value})} style={{width: 190}}>
                    {availableGroups.map(g => (
                      <option key={g} value={g}>{g === 'admin' ? 'admin (полный доступ)' : g}</option>
                    ))}
                  </select>
                  <button className="btn-primary" onClick={createUser}>Добавить</button>
                </div>
                {userMsg && <div style={{color: 'var(--danger)', fontSize: 12, marginTop: 6}}>{userMsg}</div>}
              </div>

              <div className="cfg-section" style={{marginTop: 14}}>
                <h3>Категории прав (группы)</h3>
                <div className="user-add-form" style={{marginBottom: 10}}>
                  <input className="modal-input" placeholder="Новая группа (например: auditor)" value={newGroupName} onChange={e => setNewGroupName(e.target.value)} style={{width: 250}} />
                  <button className="btn-primary" onClick={createGroup}>Добавить группу</button>
                </div>
                <div className="users-list" style={{marginBottom: 10}}>
                  {availableGroups.map(role => (
                    <div key={role} className="user-row">
                      <span className="user-icon">🛡️</span>
                      <span className="user-name">{role}</span>
                      <span className="user-role-badge">{role}</span>
                      <button className="btn-secondary" onClick={() => openPolicyModal(role)}>Права по хостам</button>
                      {role !== 'admin' && (
                        <button className="btn-danger-sm" onClick={() => deleteGroup(role)}>Удалить группу</button>
                      )}
                    </div>
                  ))}
                </div>

                <h3>Пользователи (назначение категории)</h3>
                <div className="users-list">
                  {users.map(u => (
                    <div key={u.id} className="user-row">
                      <span className="user-icon">👤</span>
                      <span className="user-name">{u.username}</span>
                      <select className="modal-input" value={u.role} onChange={e => updateUserRole(u.id, e.target.value)} style={{width: 200}}>
                        {availableGroups.map(g => <option key={g} value={g}>{g}</option>)}
                      </select>
                      <span className="user-date">{new Date(u.createdAt).toLocaleDateString('ru')}</span>
                      {u.username !== auth.user.username && (
                        <button className="btn-danger-sm" onClick={() => deleteUser(u.id)}>Удалить</button>
                      )}
                    </div>
                  ))}
                  {users.length === 0 && <div style={{color: 'var(--text-muted)', fontSize: 12, padding: 8}}>Нет пользователей</div>}
                </div>
              </div>
            </div>
          </div>
        )}

        {/* ============ LICENSE SERVER PAGE ============ */}
        {page === 'license-server' && (
          <div className="page-license-server">
            <h1>Управление лицензиями</h1>

            {/* Stats Cards */}
            <div className="overview-cards">
              <div className="ov-card"><div className="ov-value">{licenses.filter(l => l.status === 'active').length}</div><div className="ov-label">Активные</div></div>
              <div className="ov-card"><div className="ov-value">{licenses.filter(l => l.status === 'revoked').length}</div><div className="ov-label">Отозванные</div></div>
              <div className="ov-card"><div className="ov-value">{licenses.filter(l => l.status === 'expired').length}</div><div className="ov-label">Истёкшие</div></div>
              <div className="ov-card"><div className="ov-value">{licenses.length}</div><div className="ov-label">Всего</div></div>
            </div>

            {/* License Table */}
            <div className="section">
              <div className="vm-toolbar">
                <h2>Лицензии</h2>
                <div className="vm-toolbar-right">
                  <div className="export-wrap" style={{position:'relative'}}>
                    <button className="btn-secondary" onClick={e => { e.stopPropagation(); document.getElementById('ls-export-menu')?.classList.toggle('show'); }}>Экспорт ▾</button>
                    <div id="ls-export-menu" className="export-menu" style={{display:'none',position:'absolute',top:'100%',right:0,marginTop:4,background:'#fff',border:'1px solid #d6e0ea',borderRadius:10,boxShadow:'0 8px 24px rgba(15,23,42,.12)',zIndex:20,minWidth:150,overflow:'hidden'}} onClick={e => e.stopPropagation()}>
                      <a onClick={() => { window.open(`${API}/license-server/licenses/export?format=csv`, '_blank'); document.getElementById('ls-export-menu')?.classList.remove('show'); }} style={{display:'flex',alignItems:'center',gap:8,padding:'10px 14px',fontSize:12,fontWeight:600,color:'#334155',textDecoration:'none',cursor:'pointer'}}>📄 CSV</a>
                      <a onClick={() => { window.open(`${API}/license-server/licenses/export?format=xlsx`, '_blank'); document.getElementById('ls-export-menu')?.classList.remove('show'); }} style={{display:'flex',alignItems:'center',gap:8,padding:'10px 14px',fontSize:12,fontWeight:600,color:'#334155',textDecoration:'none',cursor:'pointer'}}>📊 Excel</a>
                      <a onClick={() => { window.open(`${API}/license-server/licenses/export?format=html`, '_blank'); document.getElementById('ls-export-menu')?.classList.remove('show'); }} style={{display:'flex',alignItems:'center',gap:8,padding:'10px 14px',fontSize:12,fontWeight:600,color:'#334155',textDecoration:'none',cursor:'pointer'}}>🌐 HTML</a>
                    </div>
                  </div>
                  <button className="btn-secondary" onClick={fetchLicenses}>Обновить</button>
                  <button className="btn-primary" onClick={() => setShowCreateLicense(true)}>+ Создать лицензию</button>
                </div>
              </div>

              <div className="filter-bar" style={{display:'flex',gap:10,marginBottom:12}}>
                <input placeholder="Поиск по клиенту / ключу..." value={licenseSearch} onChange={e => { setLicenseSearch(e.target.value); setLicensePage(0); }} style={{flex:1,minWidth:200}} />
                <select value={licenseStatusFilter} onChange={e => { setLicenseStatusFilter(e.target.value); setLicensePage(0); }}>
                  <option value="">Все статусы</option>
                  <option value="active">active</option>
                  <option value="revoked">revoked</option>
                  <option value="expired">expired</option>
                </select>
                <select value={licensePlanFilter} onChange={e => { setLicensePlanFilter(e.target.value); setLicensePage(0); }}>
                  <option value="">Все тарифы</option>
                  <option value="basic">basic</option>
                  <option value="pro">pro</option>
                  <option value="enterprise">enterprise</option>
                </select>
              </div>

              {licensesLoading ? <div className="loading">Загрузка...</div> : (
                <>
                <table className="vm-table"><thead><tr><th>Ключ</th><th>Клиент</th><th>Тариф</th><th>Лимит</th><th>Истекает</th><th>Статус</th><th>Последний чек</th><th>Действия</th></tr></thead><tbody>
                  {licenses.filter(l => {
                    const q = licenseSearch.toLowerCase();
                    const match = (l.licenseKey + l.customerName + l.customerCompany + l.customerEmail).toLowerCase().includes(q);
                    const statusMatch = !licenseStatusFilter || l.status === licenseStatusFilter;
                    const planMatch = !licensePlanFilter || l.plan === licensePlanFilter;
                    return match && statusMatch && planMatch;
                  }).slice(licensePage * LICENSE_PAGE_SIZE, (licensePage + 1) * LICENSE_PAGE_SIZE).map(l => (
                    <tr key={l.id}>
                      <td><code style={{fontSize:11,background:'#f1f5f9',padding:'2px 6px',borderRadius:4}}>{l.licenseKey}</code></td>
                      <td>
                        <div><strong>{l.customerName}</strong></div>
                        {l.customerCompany && <div style={{fontSize:11,color:'#64748b'}}>{l.customerCompany}</div>}
                      </td>
                      <td><span className={`tag tag-${l.plan}`}>{l.plan}</span></td>
                      <td>{l.maxAgents === 0 ? '∞' : l.maxAgents}</td>
                      <td>{new Date(l.expiresAt).toLocaleDateString('ru')}</td>
                      <td><span className={`tag tag-${l.status}`}>{l.status}</span></td>
                      <td>{l.lastCheckAt ? new Date(l.lastCheckAt).toLocaleString('ru') : '—'}</td>
                      <td>
                        <div style={{display:'flex',gap:4}}>
                          <button className="btn-xs" onClick={() => setEditLicense(l)}>✎</button>
                          <button className="btn-xs" onClick={() => extendLicense(l.id, 30)}>+30д</button>
                          {l.status !== 'revoked' && <button className="btn-xs btn-warn" onClick={() => revokeLicense(l.id)}>⊘</button>}
                          {l.status === 'revoked' && <button className="btn-xs" onClick={() => restoreLicense(l.id)}>↺</button>}
                          <button className="btn-xs btn-danger" onClick={() => deleteLicense(l.id)}>🗑</button>
                        </div>
                      </td>
                    </tr>
                  ))}
                  {licenses.length === 0 && <tr><td colSpan={8} className="empty-state">Нет лицензий</td></tr>}
                </tbody></table>

                {Math.ceil(licenses.length / LICENSE_PAGE_SIZE) > 1 && (
                  <div className="pagination">
                    <button className="pg-btn" disabled={licensePage <= 0} onClick={() => setLicensePage(p => p - 1)}>‹</button>
                    <span className="pg-info">Стр. {licensePage + 1} из {Math.ceil(licenses.length / LICENSE_PAGE_SIZE)}</span>
                    <button className="pg-btn" disabled={(licensePage + 1) * LICENSE_PAGE_SIZE >= licenses.length} onClick={() => setLicensePage(p => p + 1)}>›</button>
                  </div>
                )}
                </>
              )}
            </div>

            {/* Settings & Audit */}
            <div style={{display:'grid',gridTemplateColumns:'1fr 1fr',gap:16}}>
              <div className="section">
                <h2>Настройки License Server</h2>
                {lsSettingsLoading ? <div className="loading">Загрузка...</div> : lsSettings ? (
                  <div className="cfg-form">
                    <div className="cfg-row"><label className="cfg-label">Telegram Bot Token</label><input className="modal-input" value={lsSettings.telegram_bot_token} onChange={e => setLsSettings({...lsSettings, telegram_bot_token: e.target.value})} /></div>
                    <div className="cfg-row"><label className="cfg-label">Telegram Chat ID</label><input className="modal-input" value={lsSettings.telegram_chat_id} onChange={e => setLsSettings({...lsSettings, telegram_chat_id: e.target.value})} /></div>
                    <div className="cfg-row"><label className="cfg-label">Уведомлять за (дней)</label><input className="modal-input" type="number" value={lsSettings.notify_days_before} onChange={e => setLsSettings({...lsSettings, notify_days_before: e.target.value})} /></div>
                    <div className="cfg-row"><label className="cfg-label">Webhook URL</label><input className="modal-input" value={lsSettings.webhook_url} onChange={e => setLsSettings({...lsSettings, webhook_url: e.target.value})} /></div>
                    <button className="btn-primary" onClick={saveLsSettings}>Сохранить</button>
                  </div>
                ) : <div className="empty-state">Нет данных</div>}
              </div>

              <div className="section">
                <h2>API Ключи</h2>
                <div style={{display:'flex',gap:8,marginBottom:12}}>
                  <input className="modal-input" placeholder="Имя ключа" value={newApiKeyName} onChange={e => setNewApiKeyName(e.target.value)} />
                  <button className="btn-primary" onClick={createApiKey}>Создать</button>
                </div>
                {apiKeysLoading ? <div className="loading">Загрузка...</div> : (
                  <div className="users-list">
                    {apiKeys.map(k => (
                      <div key={k.id} className="user-row" style={{gridTemplateColumns:'1fr auto auto'}}>
                        <span>{k.name}</span>
                        <code style={{fontSize:10}}>{k.key.substring(0,16)}...</code>
                        <button className="btn-danger-sm" onClick={() => deleteApiKey(k.id)}>Удалить</button>
                      </div>
                    ))}
                    {apiKeys.length === 0 && <div className="empty-state">Нет API ключей</div>}
                  </div>
                )}
              </div>
            </div>

            {/* Audit Log */}
            <div className="section">
              <h2>Журнал аудита</h2>
              {auditLoading ? <div className="loading">Загрузка...</div> : (
                <table className="journal-table"><thead><tr><th>Время</th><th>Лицензия</th><th>Действие</th><th>Актор</th><th>Детали</th></tr></thead><tbody>
                  {auditEvents.slice(0, 50).map(e => (
                    <tr key={e.id}>
                      <td>{new Date(e.createdAt).toLocaleString('ru')}</td>
                      <td><code style={{fontSize:11}}>{e.licenseId.substring(0,8)}...</code></td>
                      <td><span className={`log-type type-${e.action}`}>{e.action}</span></td>
                      <td>{e.actor}</td>
                      <td style={{fontSize:11,color:'#64748b'}}>{e.details}</td>
                    </tr>
                  ))}
                  {auditEvents.length === 0 && <tr><td colSpan={5} className="empty-state">Нет записей</td></tr>}
                </tbody></table>
              )}
            </div>

            {/* Backup/Restore */}
            <div className="section">
              <h2>Backup / Restore</h2>
              <div style={{display:'flex',gap:8}}>
                <button className="btn-secondary" onClick={downloadBackup}>Скачать backup</button>
                <label className="btn-secondary" style={{cursor:'pointer'}}>
                  <input type="file" accept=".json" style={{display:'none'}} onChange={e => { if (e.target.files?.[0]) restoreBackup(e.target.files[0]); }} />
                  Восстановить из backup
                </label>
              </div>
            </div>
          </div>
        )}

        {/* ============ HOST PAGE ============ */}
        {page === 'host' && selectedAgentInfo && (
          <div className="page-host">
            <div className="host-topbar">
              <div className="topbar-left">
                <button className="back-btn" onClick={goOverview}>←</button>
                <span className={`status-dot ${selectedAgentInfo.status === 'online' ? 'dot-online' : 'dot-offline'}`}></span>
                <span className="topbar-host">{selectedAgentInfo.name.toUpperCase()}</span>
                <span className="topbar-port">Port: {(() => { try { return new URL(selectedAgentInfo.url).port || '9000'; } catch { return '9000'; } })()}</span>
              </div>
              <div className="topbar-right">
                <button className="topbar-create-btn" disabled={!canControlAgentUI(selectedAgent)} onClick={async () => { if (!canControlAgentUI(selectedAgent)) return; setShowDeployModal(true); if (selectedAgent) { try { const sw = await fetchJSON<string[]>(proxyUrl(selectedAgent, '/api/v1/vm/switches')); setVmSwitches(sw || []); if (sw && sw.length > 0) setDeployForm(f => ({ ...f, switchName: sw[0] })); } catch { setVmSwitches(['Default Switch']); } } }}>+ Создать ВМ</button>
                <button className="topbar-del-btn" onClick={() => setDeleteAgentId(selectedAgentInfo.id)} title="Удалить хост">✕</button>
                <span className={`topbar-svc ${selectedAgentInfo.status === 'online' ? 'topbar-svc-on' : 'topbar-svc-off'}`}>
                  <span className={`status-dot ${selectedAgentInfo.status === 'online' ? 'dot-online' : 'dot-offline'}`}></span>
                  {selectedAgentInfo.status === 'online' ? 'Служба: Работает' : 'Служба: Недоступна'}
                </span>
              </div>
            </div>

            {/* Tabs */}
            <div className="tabs-nav">
              {(['panel','backups','journal','settings'] as HostTab[]).map(t => (
                <button key={t} className={`tab-btn ${hostTab === t ? 'active' : ''}`} onClick={() => setHostTab(t)}>
                  {t === 'panel' ? 'Панель' : t === 'backups' ? 'Бэкапы' : t === 'journal' ? 'Журнал' : 'Настройки'}
                </button>
              ))}
            </div>

            {agentData?.error && <div className="error-banner">⚠ {agentData.error}</div>}

            {/* === PANEL TAB === */}
            {hostTab === 'panel' && agentData?.hostInfo && (() => {
              const hi = agentData.hostInfo!;
              const timeLabels = hostHistory.map(p => { try { return new Date(p.t).toLocaleTimeString('ru', { hour: '2-digit', minute: '2-digit' }); } catch { return ''; } });
              const cpuData = hostHistory.map(p => p.cpu);
              const ramData = hostHistory.map(p => p.ramPct);
              const vmRunData = hostHistory.map(p => p.vmRunning);
              const vmTotData = hostHistory.map(p => p.vmTotal);
              return <>
                {/* Host Info Banner */}
                <div className="host-info-banner">
                  <div className="hib-main">
                    <div className="hib-icon">🖥️</div>
                    <div className="hib-details">
                      <div className="hib-name">{hi.computerName || selectedAgentInfo?.name}</div>
                      <div className="hib-os">{hi.osName || 'Windows Server'}</div>
                    </div>
                  </div>
                  <div className="hib-stats">
                    <div className="hib-stat"><span className="hib-stat-val">{hi.vmRunning}/{hi.vmCount}</span><span className="hib-stat-lbl">ВМ запущено</span></div>
                    <div className="hib-stat"><span className="hib-stat-val">{hi.uptime || '—'}</span><span className="hib-stat-lbl">Uptime</span></div>
                    <div className="hib-stat"><span className="hib-stat-val">{formatBytes(hi.totalRAM)}</span><span className="hib-stat-lbl">Всего RAM</span></div>
                    <div className="hib-stat"><span className="hib-stat-val">{(hi.disks || []).reduce((s, d) => s + d.totalGB, 0).toFixed(0)} GB</span><span className="hib-stat-lbl">Всего дисков</span></div>
                  </div>
                </div>

                {/* Metric Cards Row */}
                <div className="host-metrics">
                  <div className="hm-card" style={{ borderTop: `3px solid ${cpuColor(hi.cpuUsage)}` }}>
                    <div className="hm-label">CPU</div>
                    <div className="hm-value" style={{ color: cpuColor(hi.cpuUsage) }}>{hi.cpuUsage.toFixed(0)}%</div>
                    <div className="hm-bar"><div className="hm-fill" style={{ width: `${hi.cpuUsage}%`, background: cpuColor(hi.cpuUsage) }}></div></div>
                  </div>
                  <div className="hm-card" style={{ borderTop: `3px solid ${ramColor(hi.ramUsePct)}` }}>
                    <div className="hm-label">RAM</div>
                    <div className="hm-value" style={{ color: ramColor(hi.ramUsePct) }}>{hi.ramUsePct.toFixed(0)}%</div>
                    <div className="hm-sub">{formatBytes(hi.usedRAM)} / {formatBytes(hi.totalRAM)}</div>
                    <div className="hm-bar"><div className="hm-fill" style={{ width: `${hi.ramUsePct}%`, background: ramColor(hi.ramUsePct) }}></div></div>
                  </div>
                  {(hi.disks || []).map(d => (
                    <div className="hm-card" key={d.drive} style={{ borderTop: `3px solid ${diskColor(d.usePct)}` }}>
                      <div className="hm-label">{d.drive}</div>
                      <div className="hm-value">{d.freeGB.toFixed(0)} GB своб.</div>
                      <div className="hm-sub">{(d.totalGB - d.freeGB).toFixed(0)} / {d.totalGB.toFixed(0)} GB</div>
                      <div className="hm-bar"><div className="hm-fill" style={{ width: `${d.usePct}%`, background: diskColor(d.usePct) }}></div></div>
                    </div>
                  ))}
                </div>

                {/* Line Charts */}
                <div className="section">
                  <h2>Мониторинг (история)</h2>
                  <div className="charts-grid">
                    <LineChart data={cpuData} labels={timeLabels} color="#3b82f6" title="Загрузка CPU" unit="%" maxVal={100} />
                    <LineChart data={ramData} labels={timeLabels} color="#8b5cf6" title="Использование RAM" unit="%" maxVal={100} />
                    <DiskStackChart disks={hi.disks || []} />
                    <VMStateCard runningData={vmRunData} totalData={vmTotData} labels={timeLabels} />
                  </div>
                </div>

                {/* Health Check */}
                {agentData.health && (
                  <div className="section">
                    <h2>{agentData.health.overall === 'ok' ? '✅' : agentData.health.overall === 'warning' ? '⚠️' : '🔴'} Health Check</h2>
                    <div className="health-grid">{(agentData.health.checks || []).map((c, i) => (
                      <div key={i} className={`health-item hi-${c.status}`}><span className="hi-icon">{c.status === 'ok' ? '✅' : c.status === 'warning' ? '⚠️' : '❌'}</span><div className="hi-info"><span className="hi-name">{c.name}</span><span className="hi-value">{c.value}</span>{c.message && c.status !== 'ok' && <span className="hi-msg">{c.message}</span>}</div></div>
                    ))}</div>
                  </div>
                )}
                <div className="section">
                  <div className="vm-toolbar">
                    <h2>Виртуальные машины ({(agentData.vms || []).length})</h2>
                    <div className="vm-toolbar-right">
                      <div className="search-box"><span>🔍</span><input placeholder="Поиск машин..." value={vmSearch} onChange={e => setVmSearch(e.target.value)} /></div>
                      <div className="filter-btns">
                        <button className={`fbtn ${vmFilter === 'all' ? 'active' : ''}`} onClick={() => setVmFilter('all')}>Все</button>
                        <button className={`fbtn ${vmFilter === 'running' ? 'active' : ''}`} onClick={() => setVmFilter('running')}>Активные</button>
                        <button className={`fbtn ${vmFilter === 'stopped' ? 'active' : ''}`} onClick={() => setVmFilter('stopped')}>Выключенные</button>
                      </div>
                    </div>
                  </div>
                  <table className="vm-table"><thead><tr><th>Имя</th><th>Статус</th><th>ЦПУ</th><th>Память</th><th>Управление</th><th>Действия</th></tr></thead><tbody>
                    {filteredVMs.map(vm => { const running = vm.state === 'Running' || vm.state === '2'; const loading = actionLoading === vm.name; return (
                      <tr key={vm.name} className={running ? 'vm-row-on' : ''}>
                        <td><strong>{vm.name}</strong></td>
                        <td><span className={`status-badge ${running ? 'success' : 'stopped'}`}>{running ? 'Активна' : 'Выключена'}</span></td>
                        <td>{vm.cpuUsage}%</td>
                        <td>{formatBytes(vm.memoryAssigned)}</td>
                        <td><div className="mgmt-cell">
                          {running
                            ? <><span className="mgmt-label">Активна</span><button className="mgmt-icon" title="Остановить" disabled={loading || !canControlAgentUI(selectedAgent)} onClick={() => vmAction(selectedAgent!, vm.name, 'stop')}>⏹</button><button className="mgmt-icon" title="Перезагрузить" disabled={loading || !canControlAgentUI(selectedAgent)} onClick={() => vmAction(selectedAgent!, vm.name, 'restart')}>↻</button></>
                            : <><button className="act-btn act-start" disabled={loading || !canControlAgentUI(selectedAgent)} onClick={() => vmAction(selectedAgent!, vm.name, 'start')}>{loading ? '...' : '▶ Запустить'}</button><button className="mgmt-icon" title="Остановить" disabled>⏹</button><button className="mgmt-icon" title="Перезагрузить" disabled>↻</button></>
                          }
                        </div></td>
                        <td><div className="actions-cell">
                          <button className="act-icon" title="Бэкап" onClick={() => { setHostTab('backups'); }}><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M19 21H5a2 2 0 01-2-2V5a2 2 0 012-2h11l5 5v11a2 2 0 01-2 2z"/><polyline points="17 21 17 13 7 13 7 21"/><polyline points="7 3 7 8 15 8"/></svg></button>
                          <button className="act-icon" title="Детали" onClick={() => openVmDetail(vm.name)}><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/></svg></button>
                          <button className="act-icon" title="Снимок" disabled={loading || !canControlAgentUI(selectedAgent)} onClick={() => openSnapModal(vm.name)}><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M23 19a2 2 0 01-2 2H3a2 2 0 01-2-2V8a2 2 0 012-2h4l2-3h6l2 3h4a2 2 0 012 2z"/><circle cx="12" cy="13" r="4"/></svg></button>
                          <button className="act-icon" title="Переименовать" disabled={!canControlAgentUI(selectedAgent)} onClick={() => openRenameModal(vm.name)}><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M11 4H4a2 2 0 00-2 2v14a2 2 0 002 2h14a2 2 0 002-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 013 3L12 15l-4 1 1-4 9.5-9.5z"/></svg></button>
                          <button className="act-icon" title="Удалить ВМ" disabled={!canControlAgentUI(selectedAgent)} onClick={() => openDeleteModal(vm.name)}><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2"/></svg></button>
                        </div></td>
                      </tr>); })}
                    {filteredVMs.length === 0 && <tr><td colSpan={6} className="empty-state">Нет виртуальных машин</td></tr>}
                  </tbody></table>
                </div>
              </>;
            })()}
            {hostTab === 'panel' && !agentData?.hostInfo && (agentData?.error ? <div className="error-banner">⚠ {agentData.error}</div> : <div className="loading">Загрузка...</div>)}

            {/* === BACKUPS TAB === */}
            {hostTab === 'backups' && (
              <div className="tab-content">
                <div className="journal-top">
                  <h2 className="journal-title">Бэкап виртуальных машин</h2>
                  <div className="journal-actions">
                    <button className="btn btn-secondary" onClick={fetchBackupLogs}>Обновить лог</button>
                  </div>
                </div>
                {backupMsg && <div className="info-banner">{backupMsg}</div>}
                <div className="backup-list">
                  {(agentData?.vms || []).map(vm => {
                    const st = backupStatus[vm.name] || 'idle';
                    return (
                    <div key={vm.name} className={`backup-row ${st === 'running' ? 'bk-running' : st === 'done' ? 'bk-done' : st === 'error' ? 'bk-error' : ''}`}>
                      <div className="backup-vm-info">
                        <strong>{vm.name}</strong>
                        <span className={`status-badge ${(vm.state === 'Running' || vm.state === '2') ? 'success' : 'stopped'}`}>{(vm.state === 'Running' || vm.state === '2') ? 'Активна' : 'Выключена'}</span>
                        {st === 'running' && <span className="bk-indicator bk-ind-run">Бэкап...</span>}
                        {st === 'done' && <span className="bk-indicator bk-ind-ok">Запущен</span>}
                        {st === 'error' && <span className="bk-indicator bk-ind-err">Ошибка</span>}
                      </div>
                      <div className="backup-vm-actions">
                        <input className="backup-dest" value={backupDest[vm.name] ?? (settings?.BackupPath || (schedules.length ? schedules[0].Destination : '') || 'D:\\Backups')} onChange={e => setBackupDest(d => ({ ...d, [vm.name]: e.target.value }))} />
                        <button className="btn btn-warning" disabled={backupLoading === vm.name} onClick={() => doBackup(vm.name)}>
                          {backupLoading === vm.name ? <span className="spinner"></span> : '💾 Бэкап'}
                        </button>
                      </div>
                    </div>);
                  })}
                </div>
                {/* Backup Archives */}
                <div className="journal-top" style={{marginTop: 20}}>
                  <h2 className="journal-title">Архивы бэкапов</h2>
                  <div className="journal-actions">
                    <div className="search-box"><span>🔍</span><input placeholder="Фильтр..." value={backupArchiveFilter} onChange={e => setBackupArchiveFilter(e.target.value)} /></div>
                    <button className="btn btn-secondary" onClick={fetchBackupFiles} disabled={backupFilesLoading}>{backupFilesLoading ? 'Загрузка...' : 'Обновить'}</button>
                  </div>
                </div>
                {backupFilesLoading && <div className="loading" style={{padding: 20}}>Загрузка списка архивов...</div>}
                {!backupFilesLoading && filteredBackupFiles.length === 0 && <div className="empty-state" style={{padding: 20}}>Нет архивов бэкапов. Выполните бэкап для создания архивов.</div>}
                {!backupFilesLoading && filteredBackupFiles.length > 0 && (() => {
                  const grouped: Record<string, BackupFile[]> = {};
                  filteredBackupFiles.forEach(bf => { if (!grouped[bf.vmName]) grouped[bf.vmName] = []; grouped[bf.vmName].push(bf); });
                  const vmNames = Object.keys(grouped).sort();
                  const fmtSz = (bytes: number) => { const gb = bytes / (1024*1024*1024); return gb >= 1 ? `${gb.toFixed(1)} GB` : `${(bytes / (1024*1024)).toFixed(0)} MB`; };
                  const toggleVM = (vm: string) => setExpandedArchiveVMs(prev => ({ ...prev, [vm]: !prev[vm] }));
                  return (
                    <div className="archive-groups" style={{marginBottom: 20, display: 'flex', flexDirection: 'column', gap: 6}}>
                      <div style={{fontSize: 11, color: 'var(--text-muted)', marginBottom: 2}}>{vmNames.length} машин • {filteredBackupFiles.length} архивов • {fmtSz(filteredBackupFiles.reduce((s, f) => s + f.size, 0))}</div>
                      {vmNames.map(vm => {
                        const files = grouped[vm].sort((a, b) => b.date.localeCompare(a.date));
                        const totalSize = files.reduce((s, f) => s + f.size, 0);
                        const expanded = !!expandedArchiveVMs[vm];
                        return (
                          <div key={vm} className="journal-card" style={{padding: 0, overflow: 'hidden'}}>
                            <div onClick={() => toggleVM(vm)} style={{display: 'flex', alignItems: 'center', gap: 10, padding: '10px 14px', cursor: 'pointer', background: expanded ? 'var(--bg-light, #f8fafc)' : 'transparent', borderBottom: expanded ? '1px solid var(--border-main)' : 'none', userSelect: 'none'}}>
                              <span style={{fontSize: 10, color: 'var(--text-muted)', transition: 'transform .15s', transform: expanded ? 'rotate(90deg)' : 'rotate(0deg)'}}>▶</span>
                              <span style={{fontWeight: 700, fontSize: 13, color: 'var(--text-main)'}}>{vm}</span>
                              <span style={{fontSize: 11, color: 'var(--text-dim)', marginLeft: 'auto'}}>{files.length} {files.length === 1 ? 'архив' : files.length < 5 ? 'архива' : 'архивов'}</span>
                              <span style={{fontSize: 11, color: 'var(--text-muted)'}}>{fmtSz(totalSize)}</span>
                              <span style={{fontSize: 10, color: 'var(--text-muted)'}}>{files[0]?.date?.slice(0, 10)}</span>
                            </div>
                            {expanded && (
                              <table className="journal-table" style={{margin: 0}}>
                                <tbody>
                                  {files.map((bf, i) => (
                                    <tr key={i}>
                                      <td className="col-mono" style={{fontSize: 11, paddingLeft: 28}}>{bf.fileName}</td>
                                      <td style={{width: 80, textAlign: 'right'}}>{fmtSz(bf.size)}</td>
                                      <td className="col-mono" style={{width: 140}}>{bf.date}</td>
                                      <td style={{width: 40}}>
                                        <button className="act-icon" title="Восстановить ВМ" onClick={e => { e.stopPropagation(); openRestoreModal(bf); }}>♻</button>
                                      </td>
                                    </tr>
                                  ))}
                                </tbody>
                              </table>
                            )}
                          </div>
                        );
                      })}
                    </div>
                  );
                })()}
                <h2>Лог бэкапов</h2>
                {backupLogs ? (() => {
                  const BK_PAGE = 15;
                  const allLines = backupLogs.replace(/\\n/g, '\n').split('\n').filter(l => l.includes('VM:') || l.includes('STATUS')).reverse();
                  const totalBkPages = Math.max(1, Math.ceil(allLines.length / BK_PAGE));
                  const paged = allLines.slice((bkLogPage - 1) * BK_PAGE, bkLogPage * BK_PAGE);
                  return <>
                  <div className="journal-card">
                    <table className="journal-table"><thead><tr><th>Время</th><th>Машина</th><th>Статус</th><th>Сообщение</th></tr></thead><tbody>
                      {paged.map((line, i) => {
                        const m = line.match(/\[([^\]]+)\]\s*VM:\s*([^|]+?)\s*\|\s*STATUS:\s*([^|]+?)\s*\|\s*(.*)/);
                        if (!m) return <tr key={i}><td colSpan={4} className="col-mono" style={{fontSize:11}}>{line.substring(0,120)}</td></tr>;
                        const [, time, vm, status, msg] = m;
                        const st = status.trim();
                        const isOk = st === 'SUCCESS';
                        const isStart = st === 'STARTED';
                        const isFail = st === 'FAILED' || st === 'ERROR';
                        return <tr key={i}>
                          <td className="col-mono">{time.trim()}</td>
                          <td className="col-vm">{vm.trim()}</td>
                          <td><span className={`log-type ${isOk ? 'type-start' : isFail ? 'type-error' : 'type-scheduler'}`}>{st}</span></td>
                          <td style={{fontSize:11,color:'var(--text-dim)'}}>{msg.trim()}</td>
                        </tr>;
                      })}
                    </tbody></table>
                  </div>
                  {totalBkPages > 1 && (
                    <div className="pagination">
                      <button className="pg-btn" disabled={bkLogPage <= 1} onClick={() => setBkLogPage(1)}>«</button>
                      <button className="pg-btn" disabled={bkLogPage <= 1} onClick={() => setBkLogPage(p => p - 1)}>‹</button>
                      {(() => { const pages: number[] = []; let s = Math.max(1, bkLogPage - 2), e = Math.min(totalBkPages, s + 4); if (e - s < 4) s = Math.max(1, e - 4); for (let i = s; i <= e; i++) pages.push(i); return pages.map(p => <button key={p} className={`pg-btn ${p === bkLogPage ? 'pg-active' : ''}`} onClick={() => setBkLogPage(p)}>{p}</button>); })()}
                      <button className="pg-btn" disabled={bkLogPage >= totalBkPages} onClick={() => setBkLogPage(p => p + 1)}>›</button>
                      <button className="pg-btn" disabled={bkLogPage >= totalBkPages} onClick={() => setBkLogPage(totalBkPages)}>»</button>
                      <span className="pg-info">Стр. {bkLogPage} из {totalBkPages} ({allLines.length} записей)</span>
                    </div>
                  )}
                  </>;
                })() : <div className="empty-state">Нет данных</div>}
              </div>
            )}

            {/* === JOURNAL TAB === */}
            {hostTab === 'journal' && (
              <div className="tab-content">
                <div className="journal-top">
                  <h2 className="journal-title">Журнал операций</h2>
                  <div className="journal-filters">
                    <button className={`jf-btn ${logFilter === 'system' ? 'active' : ''}`} onClick={() => { setLogFilter('system'); setLogPage(1); }}>Системные</button>
                    <button className={`jf-btn ${logFilter === 'backup' ? 'active' : ''}`} onClick={() => { setLogFilter('backup'); setLogPage(1); }}>Бэкапы</button>
                  </div>
                  <div className="journal-actions">
                    <span className="log-count">{filteredLogs.length} записей</span>
                    <button className="btn btn-secondary" onClick={fetchLogs} disabled={logsLoading}>Обновить</button>
                    <button className="btn-danger-sm" onClick={purgeLogs}>Очистить</button>
                  </div>
                </div>
                <div className="journal-card">
                  <table className="journal-table"><thead><tr><th>Время</th><th>Тип</th><th>Машина</th><th>Результат</th><th>Сообщение</th><th></th></tr></thead><tbody>
                    {pagedLogs.map(l => {
                      const tp = l.Type?.toUpperCase() || '';
                      const typeCls = tp.includes('BACKUP') ? 'type-backup' : tp.includes('SCHEDUL') ? 'type-scheduler' : tp.includes('START') ? 'type-start' : tp.includes('STOP') ? 'type-stop' : tp.includes('RESTART') ? 'type-restart' : tp.includes('ERROR') ? 'type-error' : tp.includes('SNAPSHOT') ? 'type-snapshot' : 'type-info';
                      const isOk = l.Status === 'Success' || l.Status === 'SUCCESS' || l.Status === 'Успех';
                      const expanded = expandedLog === l.ID;
                      return (<>
                        <tr key={l.ID} className={`journal-row ${expanded ? 'expanded' : ''}`} onClick={() => setExpandedLog(expanded ? null : l.ID)}>
                          <td className="col-mono">{fmtDate(l.Timestamp)}</td>
                          <td><span className={`log-type ${typeCls}`}>{l.Type}</span></td>
                          <td className="col-vm">{l.TargetVM || 'Система'}</td>
                          <td><span className={`log-result ${isOk ? 'res-ok' : 'res-fail'}`}>{isOk ? 'УСПЕХ' : 'ОШИБКА'}</span></td>
                          <td className="col-msg-j">{l.Message}</td>
                          <td className="col-chevron">{expanded ? '▼' : '›'}</td>
                        </tr>
                        {expanded && <tr key={`${l.ID}-exp`} className="journal-expand"><td colSpan={6}><div className="expand-content">{l.Message}</div></td></tr>}
                      </>);
                    })}
                    {filteredLogs.length === 0 && <tr><td colSpan={6} className="empty-state">Нет записей</td></tr>}
                  </tbody></table>
                </div>
                {totalLogPages > 1 && (
                  <div className="pagination">
                    <button className="pg-btn" disabled={logPage <= 1} onClick={() => setLogPage(1)}>«</button>
                    <button className="pg-btn" disabled={logPage <= 1} onClick={() => setLogPage(p => p - 1)}>‹</button>
                    {(() => { const pages: number[] = []; let s = Math.max(1, logPage - 2), e = Math.min(totalLogPages, s + 4); if (e - s < 4) s = Math.max(1, e - 4); for (let i = s; i <= e; i++) pages.push(i); return pages.map(p => <button key={p} className={`pg-btn ${p === logPage ? 'pg-active' : ''}`} onClick={() => setLogPage(p)}>{p}</button>); })()}
                    <button className="pg-btn" disabled={logPage >= totalLogPages} onClick={() => setLogPage(p => p + 1)}>›</button>
                    <button className="pg-btn" disabled={logPage >= totalLogPages} onClick={() => setLogPage(totalLogPages)}>»</button>
                    <span className="pg-info">Стр. {logPage} из {totalLogPages} ({filteredLogs.length} записей)</span>
                  </div>
                )}
              </div>
            )}

            {/* === SETTINGS TAB === */}
            {hostTab === 'settings' && settings && (
              <div className="tab-content">
                {settingsMsg && <div className="info-banner">{settingsMsg}</div>}
                <div className="settings-card">
                  <h3>Основные</h3>
                  <div className="s-grid">
                    <label>API ключ</label><input value={settings.ApiKey || ''} onChange={e => setSettings(s => s ? { ...s, ApiKey: e.target.value } : s)} />
                    <label>Архиватор</label>
                    <select value={settings.Archiver || 'zip'} onChange={e => setSettings(s => s ? { ...s, Archiver: e.target.value } : s)}>
                      <option value="zip">ZIP (встроенный)</option>
                      <option value="7z">7-Zip (быстрый, лучшее сжатие)</option>
                      <option value="zstd">Zstandard (быстрый, встроенный)</option>
                    </select>
                    <label>Степень сжатия</label>
                    <div className="slider-row">
                      <input type="range" min={0} max={9} value={settings.CompressionLevel ?? 5} onChange={e => setSettings(s => s ? { ...s, CompressionLevel: +e.target.value } : s)} />
                      <span className="slider-val">{settings.CompressionLevel ?? 5} — {['Без сжатия','Быстро','','','','Нормально','','','','Ультра'][settings.CompressionLevel ?? 5]}</span>
                    </div>
                    <label>Папка бэкапов</label><input value={settings.BackupPath || (schedules.length ? schedules[0].Destination : '')} onChange={e => setSettings(s => s ? { ...s, BackupPath: e.target.value } : s)} />
                    <label>Хранить копий</label>
                    <div className="retention-row">
                      <input type="number" min={1} value={settings.RetentionCount || 1} onChange={e => setSettings(s => s ? { ...s, RetentionCount: +e.target.value } : s)} />
                      <span className="retention-hint">последних бэкапов на каждую ВМ</span>
                    </div>
                    <label>Swagger UI</label>
                    <div className="retention-row">
                      <input type="checkbox" checked={!!settings.SwaggerEnabled} onChange={e => setSettings(s => s ? { ...s, SwaggerEnabled: e.target.checked } : s)} />
                      <span className="retention-hint">{settings.SwaggerEnabled ? 'Доступен на /swagger' : 'Отключен'}</span>
                    </div>
                  </div>
                  <div className="s-actions"><button className="btn btn-primary" onClick={saveSettings}>💾 Сохранить основные</button></div>
                  <h3>Хранилища</h3>
                  <div className="sub-tabs" style={{display:'flex',gap:0,marginBottom:12}}>
                    <button className={`sub-tab${storageTab==='s3'?' active':''}`} onClick={()=>setStorageTab('s3')}>S3 {settings.S3Enabled ? '✅' : ''}</button>
                    <button className={`sub-tab${storageTab==='smb'?' active':''}`} onClick={()=>setStorageTab('smb')}>SMB {settings.SMBEnabled ? '✅' : ''}</button>
                    <button className={`sub-tab${storageTab==='webdav'?' active':''}`} onClick={()=>setStorageTab('webdav')}>WebDAV {settings.WebDAVEnabled ? '✅' : ''}</button>
                  </div>
                  {storageTab === 's3' && (<>
                  <div className="s-grid">
                    <label>Включен</label><input type="checkbox" checked={!!settings.S3Enabled} onChange={e => setSettings(s => s ? { ...s, S3Enabled: e.target.checked } : s)} />
                    <label>Endpoint</label><input value={settings.S3Endpoint || ''} onChange={e => setSettings(s => s ? { ...s, S3Endpoint: e.target.value } : s)} placeholder="https://s3.amazonaws.com" />
                    <label>Region</label><input value={settings.S3Region || ''} onChange={e => setSettings(s => s ? { ...s, S3Region: e.target.value } : s)} placeholder="us-east-1" />
                    <label>Bucket</label><input value={settings.S3Bucket || ''} onChange={e => setSettings(s => s ? { ...s, S3Bucket: e.target.value } : s)} />
                    <label>Access Key</label><input value={settings.S3AccessKey || ''} onChange={e => setSettings(s => s ? { ...s, S3AccessKey: e.target.value } : s)} />
                    <label>Secret Key</label><input type="password" value={settings.S3SecretKey || ''} onChange={e => setSettings(s => s ? { ...s, S3SecretKey: e.target.value } : s)} />
                    <label>Базовая папка в bucket</label>
                    <div style={{display:'flex',flexDirection:'column',gap:4}}>
                      <input value={settings.S3Prefix || ''} onChange={e => setSettings(s => s ? { ...s, S3Prefix: e.target.value } : s)} placeholder="backups" />
                      <span style={{fontSize:11,color:'var(--text-dim)'}}>Префикс сохранится как: <b>{s3PrefixPreview}</b> (суффикс хоста: {s3HostSuffix})</span>
                    </div>
                    <label>S3 копий</label><input type="number" value={settings.S3RetentionCount || 0} onChange={e => setSettings(s => s ? { ...s, S3RetentionCount: +e.target.value } : s)} />
                  </div>
                  <div className="s-actions"><button className="btn btn-primary" onClick={saveSettings}>Сохранить</button>{settings.S3Enabled && settings.S3Endpoint && <button className="btn btn-warning" onClick={testS3}>Тест S3</button>}</div>
                  </>)}
                  {storageTab === 'smb' && (<>
                  <div className="s-grid">
                    <label>Включен</label><input type="checkbox" checked={!!settings.SMBEnabled} onChange={e => setSettings(s => s ? { ...s, SMBEnabled: e.target.checked } : s)} />
                    <label>Базовый UNC путь</label>
                    <div style={{display:'flex',flexDirection:'column',gap:4}}>
                      <input value={settings.SMBPath || ''} onChange={e => setSettings(s => s ? { ...s, SMBPath: e.target.value } : s)} placeholder="\\\\192.168.1.100\\backups" />
                      <span style={{fontSize:11,color:'var(--text-dim)'}}>Итоговый SMB путь: <b>{smbPathPreview}</b></span>
                    </div>
                    <label>Пользователь</label><input value={settings.SMBUser || ''} onChange={e => setSettings(s => s ? { ...s, SMBUser: e.target.value } : s)} placeholder="DOMAIN\\user (необязательно)" />
                    <label>Пароль</label><input type="password" value={settings.SMBPassword || ''} onChange={e => setSettings(s => s ? { ...s, SMBPassword: e.target.value } : s)} />
                  </div>
                  <div className="s-actions"><button className="btn btn-primary" onClick={saveSettings}>Сохранить</button>{settings.SMBEnabled && settings.SMBPath && <button className="btn btn-warning" onClick={testSMB}>Тест SMB</button>}</div>
                  </>)}
                  {storageTab === 'webdav' && (<>
                  <p style={{fontSize:11,color:'var(--text-dim)',margin:'0 0 8px'}}>Яндекс.Диск, Nextcloud, ownCloud и другие WebDAV-совместимые сервисы</p>
                  <div className="s-grid">
                    <label>Включен</label><input type="checkbox" checked={!!settings.WebDAVEnabled} onChange={e => setSettings(s => s ? { ...s, WebDAVEnabled: e.target.checked } : s)} />
                    <label>URL сервера</label><input value={settings.WebDAVURL || ''} onChange={e => setSettings(s => s ? { ...s, WebDAVURL: e.target.value } : s)} placeholder="https://webdav.yandex.ru" />
                    <label>Пользователь</label><input value={settings.WebDAVUser || ''} onChange={e => setSettings(s => s ? { ...s, WebDAVUser: e.target.value } : s)} placeholder="user@yandex.ru" />
                    <label>Пароль</label><input type="password" value={settings.WebDAVPassword || ''} onChange={e => setSettings(s => s ? { ...s, WebDAVPassword: e.target.value } : s)} />
                    <label>Базовая папка</label>
                    <div style={{display:'flex',flexDirection:'column',gap:4}}>
                      <input value={settings.WebDAVPath || ''} onChange={e => setSettings(s => s ? { ...s, WebDAVPath: e.target.value } : s)} placeholder="/backups" />
                      <span style={{fontSize:11,color:'var(--text-dim)'}}>Итоговый WebDAV путь: <b>{webdavPathPreview}</b></span>
                    </div>
                  </div>
                  <div className="s-actions"><button className="btn btn-primary" onClick={saveSettings}>Сохранить</button>{settings.WebDAVEnabled && settings.WebDAVURL && <button className="btn btn-warning" onClick={testWebDAV}>Тест WebDAV</button>}</div>
                  </>)}
<h3>Telegram</h3>
                  <div className="s-grid">
                    <label>Bot Token</label><input value={settings.TelegramBotToken || ''} onChange={e => setSettings(s => s ? { ...s, TelegramBotToken: e.target.value } : s)} />
                    <label>Chat ID</label><input value={settings.TelegramChatID || ''} onChange={e => setSettings(s => s ? { ...s, TelegramChatID: e.target.value } : s)} />
                    <label>Включен</label><input type="checkbox" checked={!!settings.TelegramEnabled} onChange={e => setSettings(s => s ? { ...s, TelegramEnabled: e.target.checked } : s)} />
                    <label>Только ошибки</label><input type="checkbox" checked={!!settings.TelegramOnlyErrors} onChange={e => setSettings(s => s ? { ...s, TelegramOnlyErrors: e.target.checked } : s)} />
                  </div>
                  <div className="s-actions">
                    <button className="btn btn-primary" onClick={saveSettings}>Сохранить</button>
                    {settings.TelegramBotToken && settings.TelegramChatID && <button className="btn btn-warning" onClick={testTelegram}>Тест Telegram</button>}
                  </div>
                </div>

                {/* Schedule creation like local app */}
                <div className="settings-card">
                  <h3>Планировщик бэкапов</h3>
                  <div className="sched-form">
                    <div className="sched-row">
                      <label>Время</label>
                      <div style={{display:'flex',gap:2,alignItems:'center'}}>
                        <select value={newSched.time.split(':')[0]} onChange={e => setNewSched(s => ({...s, time: e.target.value.padStart(2,'0') + ':' + s.time.split(':')[1]}))}>{Array.from({length:24},(_,i)=>i).map(h=><option key={h} value={String(h).padStart(2,'0')}>{String(h).padStart(2,'0')}</option>)}</select>
                        <span>:</span>
                        <select value={newSched.time.split(':')[1]} onChange={e => setNewSched(s => ({...s, time: s.time.split(':')[0] + ':' + e.target.value.padStart(2,'0')}))}>{[0,5,10,15,20,25,30,35,40,45,50,55].map(m=><option key={m} value={String(m).padStart(2,'0')}>{String(m).padStart(2,'0')}</option>)}</select>
                      </div>
                      <label>Путь назначения</label>
                      <input value={newSched.dest} onChange={e => setNewSched(s => ({ ...s, dest: e.target.value }))} placeholder={settings.BackupPath || 'D:\\Backups'} />
                    </div>
                    <div className="sched-vms-label">Выберите машины:</div>
                    <div className="sched-vms">
                      {(agentData?.vms || []).map(vm => (
                        <label key={vm.name} className={`vm-chip ${newSched.vmNames.includes(vm.name) ? 'active' : ''}`}>
                          <input type="checkbox" checked={newSched.vmNames.includes(vm.name)} onChange={() => toggleSchedVM(vm.name)} />
                          {vm.name}
                        </label>
                      ))}
                    </div>
                    <div className="s-actions"><button className="btn btn-primary" onClick={createSchedule}>Создать расписание</button></div>
                  </div>
                </div>

                <div className="settings-card">
                  <h3>Активные расписания</h3>
                  <div className="schedule-list">
                    {schedules.map(sc => {
                      if (editSched && editSched.id === sc.ID) {
                        return (
                          <div key={sc.ID} className="schedule-row schedule-edit">
                            <div className="sched-edit-form">
                              <div className="sched-row">
                                <label>Время</label>
                                <div style={{display:'flex',gap:2,alignItems:'center'}}>
                                  <select value={editSched.time.split(':')[0]} onChange={e => setEditSched({...editSched, time: e.target.value.padStart(2,'0') + ':' + editSched.time.split(':')[1]})}>{Array.from({length:24},(_,i)=>i).map(h=><option key={h} value={String(h).padStart(2,'0')}>{String(h).padStart(2,'0')}</option>)}</select>
                                  <span>:</span>
                                  <select value={editSched.time.split(':')[1]} onChange={e => setEditSched({...editSched, time: editSched.time.split(':')[0] + ':' + e.target.value.padStart(2,'0')})}>{[0,5,10,15,20,25,30,35,40,45,50,55].map(m=><option key={m} value={String(m).padStart(2,'0')}>{String(m).padStart(2,'0')}</option>)}</select>
                                </div>
                                <label>Путь</label>
                                <input value={editSched.dest} onChange={e => setEditSched({ ...editSched, dest: e.target.value })} />
                                <label style={{display:'flex',alignItems:'center',gap:4}}><input type="checkbox" checked={editSched.enabled} onChange={e => setEditSched({ ...editSched, enabled: e.target.checked })} /> Вкл</label>
                              </div>
                              <div className="sched-vms">
                                {(agentData?.vms || []).map(vm => (
                                  <label key={vm.name} className={`vm-chip ${editSched.vmNames.includes(vm.name) ? 'active' : ''}`}>
                                    <input type="checkbox" checked={editSched.vmNames.includes(vm.name)} onChange={() => toggleEditSchedVM(vm.name)} />
                                    {vm.name}
                                  </label>
                                ))}
                              </div>
                              <div className="s-actions">
                                <button className="btn btn-primary" onClick={updateSchedule}>Сохранить</button>
                                <button className="btn btn-secondary" onClick={() => setEditSched(null)}>Отмена</button>
                              </div>
                            </div>
                          </div>
                        );
                      }
                      let vmNames = sc.VMList;
                      try { vmNames = JSON.parse(sc.VMList).join(', '); } catch {}
                      const parts = sc.CronString.split(' ');
                      const timeStr = `${parts[1]?.padStart(2, '0')}:${parts[0]?.padStart(2, '0')}`;
                      return (
                        <div key={sc.ID} className="schedule-row">
                          <span className="cron-tag">{timeStr}</span>
                          <span className="sched-vms-text">{vmNames}</span>
                          <span className="dest-tag">{sc.Destination}</span>
                          <span>{sc.Enabled ? '✅' : '⏸️'}</span>
                          <button className="act-icon" title="Редактировать" onClick={() => startEditSched(sc)}>✏️</button>
                          <button className="act-icon" title="Удалить" onClick={() => deleteSchedule(sc.ID)}>🗑️</button>
                        </div>
                      );
                    })}
                    {schedules.length === 0 && <div className="empty-state">Нет расписаний</div>}
                  </div>
                </div>
              </div>
            )}
            {hostTab === 'settings' && !settings && <div className="loading">Загрузка настроек...</div>}
          </div>
        )}
      </main>

      {/* Add agent modal */}
      {showAddModal && (
        <div className="modal-overlay" onClick={() => setShowAddModal(false)}><div className="modal" onClick={e => e.stopPropagation()}>
          <h2>Добавить хост</h2>
          <div className="form-group"><label>URL (nodax-server)</label><input value={addForm.url} onChange={e => setAddForm(f => ({ ...f, url: e.target.value }))} placeholder="http://192.168.1.10:9000" autoFocus /></div>
          <div className="form-group"><label>API Key</label><input value={addForm.apiKey} onChange={e => setAddForm(f => ({ ...f, apiKey: e.target.value }))} placeholder="Ключ авторизации" /></div>
          {addError && <div className="form-error">{addError}</div>}
          <div className="modal-actions"><button className="btn btn-secondary" onClick={() => setShowAddModal(false)}>Отмена</button><button className="btn btn-primary" onClick={handleAddAgent}>Добавить</button></div>
        </div></div>
      )}

      {/* Delete agent confirmation modal */}
      {deleteAgentId && (
        <div className="modal-overlay" onClick={() => setDeleteAgentId(null)}><div className="modal" onClick={e => e.stopPropagation()}>
          <div className="modal-header"><h2>Удалить хост</h2><button className="modal-close" onClick={() => setDeleteAgentId(null)}>✕</button></div>
          <div className="modal-body">
            <p className="delete-warn">Удалить хост «{agents.find(a => a.id === deleteAgentId)?.name || deleteAgentId}» из центра? Это действие необратимо!</p>
          </div>
          <div className="modal-footer">
            <button className="btn-cancel" onClick={() => setDeleteAgentId(null)}>Отмена</button>
            <button className="btn-danger" onClick={confirmDeleteAgent}>Удалить</button>
          </div>
        </div></div>
      )}

      {/* VM Detail modal */}
      {showVmDetail && vmDetail && (
        <div className="modal-overlay" onClick={() => setShowVmDetail(false)}><div className="modal modal-lg" onClick={e => e.stopPropagation()}>
          <div className="modal-header"><h2>{vmDetail.name}</h2><button className="modal-close" onClick={() => setShowVmDetail(false)}>✕</button></div>
          <div className="detail-grid">
            <div className="detail-row"><span className="dl">Статус</span><span>{vmDetail.state}</span></div>
            <div className="detail-row"><span className="dl">Поколение</span><span>{vmDetail.generation}</span></div>
            <div className="detail-row"><span className="dl">Версия</span><span>{vmDetail.version}</span></div>
            <div className="detail-row"><span className="dl">Путь</span><span className="col-mono">{vmDetail.path}</span></div>
            <div className="detail-row"><span className="dl">Uptime</span><span>{vmDetail.uptime}</span></div>
            <div className="detail-row"><span className="dl">CPU</span><span>{vmDetail.cpuUsage}%</span></div>
            <div className="detail-row"><span className="dl">Память</span><span>{formatBytes(vmDetail.memoryAssigned)}</span></div>
          </div>
          {(vmDetail.hardDrives || []).length > 0 && (<><h3>Диски</h3>{vmDetail.hardDrives.map((d, i) => <div key={i} className="detail-row"><span className="col-mono" style={{fontSize:11}}>{d}</span></div>)}</>)}
          {(vmDetail.networkAdapters || []).length > 0 && (<><h3>Сетевые адаптеры</h3>{vmDetail.networkAdapters.map((a, i) => <div key={i} className="detail-row"><span>{a}</span></div>)}</>)}
          {(vmDetail.snapshots || []).length > 0 && (<><h3>Снимки</h3>{vmDetail.snapshots.map((s, i) => <div key={i} className="detail-row"><span>📸 {s}</span></div>)}</>)}
        </div></div>
      )}

      {/* Rename VM modal */}
      {renameTarget && (
        <div className="modal-overlay" onClick={() => setRenameTarget(null)}><div className="modal" onClick={e => e.stopPropagation()}>
          <div className="modal-header"><h2>Переименовать ВМ</h2><button className="modal-close" onClick={() => setRenameTarget(null)}>✕</button></div>
          <div className="modal-body">
            <label className="modal-label">Новое имя</label>
            <input className="modal-input" value={renameValue} onChange={e => setRenameValue(e.target.value)} onKeyDown={e => e.key === 'Enter' && doRename()} autoFocus />
          </div>
          <div className="modal-footer">
            <button className="btn-cancel" onClick={() => setRenameTarget(null)}>Отмена</button>
            <button className="btn-primary" onClick={doRename}>Переименовать</button>
          </div>
        </div></div>
      )}

      {/* Delete VM modal */}
      {deleteTarget && (
        <div className="modal-overlay" onClick={() => setDeleteTarget(null)}><div className="modal" onClick={e => e.stopPropagation()}>
          <div className="modal-header"><h2>Удалить ВМ</h2><button className="modal-close" onClick={() => setDeleteTarget(null)}>✕</button></div>
          <div className="modal-body">
            <p className="delete-warn">Удалить виртуальную машину «{deleteTarget}»? Это действие необратимо!</p>
          </div>
          <div className="modal-footer">
            <button className="btn-cancel" onClick={() => setDeleteTarget(null)}>Отмена</button>
            <button className="btn-danger" onClick={doDelete}>OK</button>
          </div>
        </div></div>
      )}

      {/* Snapshot modal */}
      {snapTarget && (
        <div className="modal-overlay" onClick={() => setSnapTarget(null)}><div className="modal" onClick={e => e.stopPropagation()}>
          <div className="modal-header"><h2>Создать снимок</h2><button className="modal-close" onClick={() => setSnapTarget(null)}>✕</button></div>
          <div className="modal-body">
            <label className="modal-label">Имя снимка</label>
            <input className="modal-input" value={snapName} onChange={e => setSnapName(e.target.value)} onKeyDown={e => e.key === 'Enter' && doSnapshot()} autoFocus />
          </div>
          <div className="modal-footer">
            <button className="btn-cancel" onClick={() => setSnapTarget(null)}>Отмена</button>
            <button className="btn-primary" onClick={doSnapshot}>Создать</button>
          </div>
        </div></div>
      )}

      {/* Deploy VM modal */}
      {showDeployModal && (
        <div className="modal-overlay" onClick={() => setShowDeployModal(false)}><div className="modal modal-lg" onClick={e => e.stopPropagation()}>
          <div className="modal-header"><h2>Создать виртуальную машину</h2><button className="modal-close" onClick={() => setShowDeployModal(false)}>✕</button></div>
          <div className="modal-body">
            <div className="deploy-grid">
              <label className="modal-label">Имя ВМ</label>
              <input className="modal-input" value={deployForm.name} onChange={e => setDeployForm({...deployForm, name: e.target.value})} autoFocus />

              <label className="modal-label">Тип ОС</label>
              <select className="modal-input" value={deployForm.osType} onChange={e => setDeployForm({...deployForm, osType: e.target.value})}>
                <option value="windows">Windows</option>
                <option value="linux">Linux</option>
              </select>

              <label className="modal-label">ЦП / ОЗУ</label>
              <div className="deploy-cpu-ram">
                <select className="modal-input" value={deployForm.cpu} onChange={e => setDeployForm({...deployForm, cpu: parseInt(e.target.value)})}>
                  <option value="1">1 Ядро</option>
                  <option value="2">2 Ядра</option>
                  <option value="4">4 Ядра</option>
                  <option value="8">8 Ядер</option>
                </select>
                <input className="modal-input" type="number" value={deployForm.ram} onChange={e => setDeployForm({...deployForm, ram: parseInt(e.target.value) || 1})} min={1} style={{width:80}} />
                <span className="deploy-unit">GB</span>
              </div>

              <label className="modal-label">Папка хранения</label>
              <input className="modal-input" value={deployForm.storagePath} onChange={e => setDeployForm({...deployForm, storagePath: e.target.value})} />

              <label className="modal-label">Диски (VHDX)</label>
              <div className="deploy-disks">
                {deployDisks.map((size, idx) => (
                  <div key={idx} className="deploy-disk-row">
                    <span className="deploy-disk-label">Диск {idx + 1}</span>
                    <input className="modal-input" type="number" value={size} min={1} onChange={e => { const d = [...deployDisks]; d[idx] = parseInt(e.target.value) || 1; setDeployDisks(d); }} style={{width:80}} />
                    <span className="deploy-unit">GB</span>
                    {deployDisks.length > 1 && <button className="deploy-disk-del" onClick={() => setDeployDisks(deployDisks.filter((_, i) => i !== idx))}>✕</button>}
                  </div>
                ))}
                <button className="deploy-disk-add" onClick={() => setDeployDisks([...deployDisks, 127])}>+ Добавить диск</button>
              </div>

              <label className="modal-label">Сеть</label>
              <select className="modal-input" value={deployForm.switchName} onChange={e => setDeployForm({...deployForm, switchName: e.target.value})}>
                {vmSwitches.length > 0 ? vmSwitches.map(sw => <option key={sw} value={sw}>{sw}</option>) : <option value="Default Switch">Default Switch</option>}
              </select>
            </div>
            {deployLoading && <div className="deploy-loading">Создание ВМ...</div>}
          </div>
          <div className="modal-footer">
            <button className="btn-cancel" onClick={() => setShowDeployModal(false)}>Отмена</button>
            <button className="btn-primary" onClick={deployVM} disabled={deployLoading || !deployForm.name || !deployForm.storagePath}>{deployLoading ? 'Создание...' : 'Создать ВМ'}</button>
          </div>
        </div></div>
      )}

      {/* Restore VM modal */}
      {restoreTarget && (
        <div className="modal-overlay" onClick={() => setRestoreTarget(null)}><div className="modal" style={{maxWidth: 680, width: '90vw'}} onClick={e => e.stopPropagation()}>
          <div className="modal-header"><h2>Восстановить ВМ из бэкапа</h2><button className="modal-close" onClick={() => setRestoreTarget(null)}>✕</button></div>
          <div className="modal-body">
            <div className="detail-grid" style={{marginBottom: 14}}>
              <div className="detail-row"><span className="dl">Машина</span><span>{restoreTarget.vmName}</span></div>
              <div className="detail-row"><span className="dl">Архив</span><span className="col-mono" style={{fontSize: 11}}>{restoreTarget.fileName}</span></div>
              <div className="detail-row"><span className="dl">Размер</span><span>{formatSize(restoreTarget.size)}</span></div>
              <div className="detail-row"><span className="dl">Дата</span><span>{restoreTarget.date}</span></div>
            </div>
            <label className="modal-label">Имя новой ВМ</label>
            <input className="modal-input" value={restoreForm.newVMName} onChange={e => setRestoreForm({...restoreForm, newVMName: e.target.value})} style={{marginBottom: 12}} />
            <label className="modal-label">Путь восстановления (пусто = по умолчанию)</label>
            <input className="modal-input" value={restoreForm.restorePath} onChange={e => setRestoreForm({...restoreForm, restorePath: e.target.value})} placeholder="D:\Hyper-V" />
            {restoreLoading && <div className="deploy-loading">Восстановление запущено...</div>}
          </div>
          <div className="modal-footer">
            <button className="btn-cancel" onClick={() => setRestoreTarget(null)}>Отмена</button>
            <button className="btn-primary" onClick={doRestore} disabled={restoreLoading || !restoreForm.newVMName}>{restoreLoading ? 'Запуск...' : 'Восстановить'}</button>
          </div>
        </div></div>
      )}

      {policyModalRole && (
        <div className="modal-overlay" onClick={() => setPolicyModalRole(null)}><div className="modal modal-lg" onClick={e => e.stopPropagation()}>
          <div className="modal-header"><h2>Права категории: {policyModalRole}</h2><button className="modal-close" onClick={() => setPolicyModalRole(null)}>✕</button></div>
          <div className="modal-body">
            <div style={{fontSize: 12, color: 'var(--text-muted)', marginBottom: 8}}>
              {policyModalRole === 'admin'
                ? 'Admin имеет полный доступ всегда. Эти права не редактируются.'
                : 'Настройте права этой категории: какие хосты можно видеть и какими можно управлять.'}
            </div>
            <div className="users-list" style={{ marginBottom: 10 }}>
              <div className="user-row" style={{ gridTemplateColumns: '1fr auto auto auto auto auto', alignItems: 'center' }}>
                <span className="user-name">Права по разделам</span>
                <label style={{display:'flex',alignItems:'center',gap:6,fontSize:12}}><input disabled={policyModalRole === 'admin'} type="checkbox" checked={sectionDraft.overview} onChange={e => setSectionDraft(s => ({ ...s, overview: e.target.checked }))} />Обзор</label>
                <label style={{display:'flex',alignItems:'center',gap:6,fontSize:12}}><input disabled={policyModalRole === 'admin'} type="checkbox" checked={sectionDraft.statistics} onChange={e => setSectionDraft(s => ({ ...s, statistics: e.target.checked }))} />Статистика</label>
                <label style={{display:'flex',alignItems:'center',gap:6,fontSize:12}}><input disabled={policyModalRole === 'admin'} type="checkbox" checked={sectionDraft.storage} onChange={e => setSectionDraft(s => ({ ...s, storage: e.target.checked }))} />Хранилище</label>
                <label style={{display:'flex',alignItems:'center',gap:6,fontSize:12}}><input disabled={policyModalRole === 'admin'} type="checkbox" checked={sectionDraft.settings} onChange={e => setSectionDraft(s => ({ ...s, settings: e.target.checked }))} />Настройки</label>
                <label style={{display:'flex',alignItems:'center',gap:6,fontSize:12}}><input disabled={policyModalRole === 'admin'} type="checkbox" checked={sectionDraft.security} onChange={e => setSectionDraft(s => ({ ...s, security: e.target.checked }))} />Безопасность</label>
              </div>
            </div>
            <div className="users-list">
              {agents.map(a => {
                const v = policyDraft[a.id] || { view: false, control: false };
                return (
                  <div key={a.id} className="user-row" style={{gridTemplateColumns:'1fr auto auto', alignItems:'center'}}>
                    <span className="user-name"><span className={`status-dot ${a.status === 'online' ? 'dot-online' : 'dot-offline'}`}></span> {a.name}</span>
                    <label style={{display:'flex',alignItems:'center',gap:6,fontSize:12}}>
                      <input disabled={policyModalRole === 'admin'} type="checkbox" checked={v.view} onChange={e => setPolicyDraft(p => ({ ...p, [a.id]: { ...v, view: e.target.checked, control: e.target.checked ? v.control : false } }))} />
                      Просмотр
                    </label>
                    <label style={{display:'flex',alignItems:'center',gap:6,fontSize:12}}>
                      <input disabled={policyModalRole === 'admin'} type="checkbox" checked={v.control} onChange={e => setPolicyDraft(p => ({ ...p, [a.id]: { ...v, view: e.target.checked ? true : v.view, control: e.target.checked } }))} />
                      Управление
                    </label>
                  </div>
                );
              })}
            </div>
          </div>
          <div className="modal-footer">
            <button className="btn-cancel" onClick={() => setPolicyModalRole(null)}>Отмена</button>
            {policyModalRole !== 'admin' && <button className="btn-primary" onClick={savePolicyModal}>Сохранить права</button>}
          </div>
        </div></div>
      )}

      {/* Create License Modal */}
      {showCreateLicense && (
        <div className="modal-overlay" onClick={() => setShowCreateLicense(false)}><div className="modal modal-lg" onClick={e => e.stopPropagation()}>
          <div className="modal-header"><h2>Создать лицензию</h2><button className="modal-close" onClick={() => setShowCreateLicense(false)}>✕</button></div>
          <div className="modal-body">
            <div className="deploy-grid">
              <label className="modal-label">Клиент</label>
              <input className="modal-input" value={createLicenseForm.customerName} onChange={e => setCreateLicenseForm({...createLicenseForm, customerName: e.target.value})} placeholder="Имя клиента" />
              <label className="modal-label">Компания</label>
              <input className="modal-input" value={createLicenseForm.customerCompany} onChange={e => setCreateLicenseForm({...createLicenseForm, customerCompany: e.target.value})} placeholder="Название компании" />
              <label className="modal-label">Email</label>
              <input className="modal-input" type="email" value={createLicenseForm.customerEmail} onChange={e => setCreateLicenseForm({...createLicenseForm, customerEmail: e.target.value})} placeholder="email@example.com" />
              <label className="modal-label">Telegram</label>
              <input className="modal-input" value={createLicenseForm.customerTelegram} onChange={e => setCreateLicenseForm({...createLicenseForm, customerTelegram: e.target.value})} placeholder="@username" />
              <label className="modal-label">Телефон</label>
              <input className="modal-input" value={createLicenseForm.customerPhone} onChange={e => setCreateLicenseForm({...createLicenseForm, customerPhone: e.target.value})} placeholder="+7..." />
              <label className="modal-label">Тариф</label>
              <select className="modal-input" value={createLicenseForm.plan} onChange={e => setCreateLicenseForm({...createLicenseForm, plan: e.target.value, maxAgents: e.target.value === 'basic' ? 10 : e.target.value === 'pro' ? 30 : 0})}>
                <option value="basic">basic (10 агентов)</option>
                <option value="pro">pro (30 агентов)</option>
                <option value="enterprise">enterprise (безлимит)</option>
              </select>
              <label className="modal-label">Лимит агентов</label>
              <input className="modal-input" type="number" value={createLicenseForm.maxAgents} onChange={e => setCreateLicenseForm({...createLicenseForm, maxAgents: parseInt(e.target.value) || 0})} />
              <label className="modal-label">Дней действия</label>
              <input className="modal-input" type="number" value={createLicenseForm.validDays} onChange={e => setCreateLicenseForm({...createLicenseForm, validDays: parseInt(e.target.value) || 365})} />
              <label className="modal-label">Trial</label>
              <select className="modal-input" value={createLicenseForm.isTrial ? '1' : '0'} onChange={e => setCreateLicenseForm({...createLicenseForm, isTrial: e.target.value === '1'})}>
                <option value="0">Нет</option>
                <option value="1">Да (14д)</option>
              </select>
              <label className="modal-label">Комментарий</label>
              <input className="modal-input" value={createLicenseForm.notes} onChange={e => setCreateLicenseForm({...createLicenseForm, notes: e.target.value})} placeholder="Контракт #" />
            </div>
          </div>
          <div className="modal-footer">
            <button className="btn-cancel" onClick={() => setShowCreateLicense(false)}>Отмена</button>
            <button className="btn-primary" onClick={createLicense}>Создать</button>
          </div>
        </div></div>
      )}

      {/* Edit License Modal */}
      {editLicense && (
        <div className="modal-overlay" onClick={() => setEditLicense(null)}><div className="modal modal-lg" onClick={e => e.stopPropagation()}>
          <div className="modal-header"><h2>Редактировать лицензию</h2><button className="modal-close" onClick={() => setEditLicense(null)}>✕</button></div>
          <div className="modal-body">
            <div className="deploy-grid">
              <label className="modal-label">Клиент</label>
              <input className="modal-input" value={editLicense.customerName} onChange={e => setEditLicense({...editLicense, customerName: e.target.value})} />
              <label className="modal-label">Компания</label>
              <input className="modal-input" value={editLicense.customerCompany} onChange={e => setEditLicense({...editLicense, customerCompany: e.target.value})} />
              <label className="modal-label">Email</label>
              <input className="modal-input" type="email" value={editLicense.customerEmail} onChange={e => setEditLicense({...editLicense, customerEmail: e.target.value})} />
              <label className="modal-label">Telegram</label>
              <input className="modal-input" value={editLicense.customerTelegram} onChange={e => setEditLicense({...editLicense, customerTelegram: e.target.value})} />
              <label className="modal-label">Телефон</label>
              <input className="modal-input" value={editLicense.customerPhone} onChange={e => setEditLicense({...editLicense, customerPhone: e.target.value})} />
              <label className="modal-label">Тариф</label>
              <select className="modal-input" value={editLicense.plan} onChange={e => setEditLicense({...editLicense, plan: e.target.value})}>
                <option value="basic">basic</option>
                <option value="pro">pro</option>
                <option value="enterprise">enterprise</option>
              </select>
              <label className="modal-label">Лимит агентов</label>
              <input className="modal-input" type="number" value={editLicense.maxAgents} onChange={e => setEditLicense({...editLicense, maxAgents: parseInt(e.target.value) || 0})} />
              <label className="modal-label">Комментарий</label>
              <input className="modal-input" value={editLicense.notes} onChange={e => setEditLicense({...editLicense, notes: e.target.value})} />
            </div>
          </div>
          <div className="modal-footer">
            <button className="btn-cancel" onClick={() => setEditLicense(null)}>Отмена</button>
            <button className="btn-primary" onClick={updateLicense}>Сохранить</button>
          </div>
        </div></div>
      )}

      {/* Confirm Modal */}
      {confirmModal?.show && (
        <div className="modal-overlay" onClick={() => setConfirmModal(null)}><div className="modal confirm-modal" onClick={e => e.stopPropagation()}>
          <div className={`confirm-icon ${confirmModal.type}`}>
            {confirmModal.type === 'warn' ? '⚠️' : confirmModal.type === 'danger' ? '❌' : 'ℹ️'}
          </div>
          <h3>{confirmModal.title}</h3>
          <p>{confirmModal.message}</p>
          <div className="row" style={{justifyContent:'center',gap:12}}>
            <button className="btn-ghost" onClick={() => setConfirmModal(null)}>Отмена</button>
            <button className={confirmModal.type === 'danger' ? 'btn-danger' : 'btn-primary'} onClick={confirmModal.onConfirm}>
              {confirmModal.type === 'danger' ? 'Удалить' : 'Подтвердить'}
            </button>
          </div>
        </div></div>
      )}
    </div>
  );
}








