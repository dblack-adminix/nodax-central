package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Server struct {
	store      *Store
	adminToken string
	graceDays  int
	signKey    ed25519.PrivateKey
	pubKey     ed25519.PublicKey
}

type validateRequest struct {
	LicenseKey string `json:"licenseKey"`
	InstanceID string `json:"instanceId"`
	Hostname   string `json:"hostname"`
	Version    string `json:"version"`
	AgentCount int    `json:"agentCount"`
}

type signedValidatePayload struct {
	LicenseID    string `json:"licenseId,omitempty"`
	Status       string `json:"status"`
	Valid        bool   `json:"valid"`
	Reason       string `json:"reason,omitempty"`
	Plan         string `json:"plan,omitempty"`
	MaxAgents    int    `json:"maxAgents,omitempty"`
	ExpiresAt    string `json:"expiresAt,omitempty"`
	GraceDays    int    `json:"graceDays"`
	ServerTime   string `json:"serverTime"`
	InstanceID   string `json:"instanceId,omitempty"`
	LicenseKey   string `json:"licenseKey,omitempty"`
	CustomerName string `json:"customerName,omitempty"`
}

func main() {
	dbPath := strings.TrimSpace(os.Getenv("LICENSE_DB_PATH"))
	if dbPath == "" {
		dbPath = resolveDataFilePath("license-server.db")
	}
	adminToken := strings.TrimSpace(os.Getenv("LICENSE_ADMIN_TOKEN"))
	if adminToken == "" {
		adminToken = randomHex(24)
		log.Printf("[WARN] LICENSE_ADMIN_TOKEN не задан. Сгенерирован временный токен: %s", adminToken)
	}

	graceDays := 7
	if v := strings.TrimSpace(os.Getenv("LICENSE_GRACE_DAYS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			graceDays = n
		}
	}

	store, err := NewStore(dbPath)
	if err != nil {
		log.Fatalf("init store: %v", err)
	}
	defer store.Close()

	priv, pub, err := loadOrCreateSigningKey()
	if err != nil {
		log.Fatalf("init signing key: %v", err)
	}

	defaultPass := strings.TrimSpace(os.Getenv("LICENSE_ADMIN_PASSWORD"))
	if defaultPass == "" {
		defaultPass = "admin"
	}
	if err := store.EnsureAdmin(defaultPass); err != nil {
		log.Fatalf("init admin user: %v", err)
	}
	log.Printf("Admin user: admin (default password если первый запуск: %s)", defaultPass)

	srv := &Server{store: store, adminToken: adminToken, graceDays: graceDays, signKey: priv, pubKey: pub}
	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.handleRoot)
	mux.HandleFunc("/admin", srv.handleAdminPage)
	mux.HandleFunc("/client", srv.handleClientPage)
	mux.HandleFunc("/assets/logo", srv.handleLogo)
	mux.HandleFunc("/healthz", srv.handleHealth)
	mux.HandleFunc("/api/v1/public-key", srv.handlePublicKey)
	mux.HandleFunc("/api/v1/license/validate", srv.handleValidate)

	mux.HandleFunc("/api/v1/auth/login", srv.handleLogin)
	mux.HandleFunc("/api/v1/auth/logout", srv.handleLogout)
	mux.HandleFunc("/api/v1/auth/me", srv.handleAuthMe)
	mux.HandleFunc("/api/v1/auth/change-password", srv.withAdmin(srv.handleChangePassword))
	mux.HandleFunc("/api/v1/client/auth/login", srv.handleClientLogin)
	mux.HandleFunc("/api/v1/client/auth/logout", srv.handleClientLogout)
	mux.HandleFunc("/api/v1/client/auth/me", srv.handleClientAuthMe)
	mux.HandleFunc("/api/v1/client/license", srv.handleClientLicense)

	mux.HandleFunc("/api/v1/licenses", srv.withAdmin(srv.handleLicenses))
	mux.HandleFunc("/api/v1/licenses/export", srv.withAdmin(srv.handleLicensesExport))
	mux.HandleFunc("/api/v1/licenses/{id}", srv.withAdmin(srv.handleLicenseByID))
	mux.HandleFunc("/api/v1/licenses/{id}/extend", srv.withAdmin(srv.handleLicenseExtend))
	mux.HandleFunc("/api/v1/licenses/{id}/revoke", srv.withAdmin(srv.handleLicenseRevoke))
	mux.HandleFunc("/api/v1/licenses/{id}/restore", srv.withAdmin(srv.handleLicenseRestore))

	mux.HandleFunc("/api/v1/audit", srv.withAdmin(srv.handleAudit))
	mux.HandleFunc("/api/v1/settings", srv.withAdmin(srv.handleSettings))
	mux.HandleFunc("/api/v1/api-keys", srv.withAdmin(srv.handleAPIKeys))
	mux.HandleFunc("/api/v1/api-keys/{id}", srv.withAdmin(srv.handleAPIKeyDelete))
	mux.HandleFunc("/api/v1/backup", srv.withAdmin(srv.handleBackup))
	mux.HandleFunc("/api/v1/restore", srv.withAdmin(srv.handleRestore))
	mux.HandleFunc("/api/v1/test-telegram", srv.withAdmin(srv.handleTestTelegram))
	mux.HandleFunc("/api/v1/broadcast-clients", srv.withAdmin(srv.handleBroadcastClients))
	mux.HandleFunc("/api/v1/test-webhook", srv.withAdmin(srv.handleTestWebhook))

	go srv.expirationNotifier()
	go srv.telegramBindingLoop()

	port := strings.TrimSpace(os.Getenv("LICENSE_SERVER_PORT"))
	if port == "" {
		port = "8091"
	}
	log.Printf("License Server запущен на :%s", port)
	log.Printf("Public key (base64): %s", base64.StdEncoding.EncodeToString(pub))
	if err := http.ListenAndServe(":"+port, cors(mux)); err != nil {
		log.Fatal(err)
	}
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusFound)
}

func (s *Server) handleAdminPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", 405)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(adminPageHTML))
}

func (s *Server) handleClientPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", 405)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(clientPageHTML))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, 200, map[string]any{"status": "ok", "time": time.Now().UTC().Format(time.RFC3339)})
}

func (s *Server) handlePublicKey(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, 200, map[string]string{
		"algorithm": "ed25519",
		"publicKey": base64.StdEncoding.EncodeToString(s.pubKey),
	})
}

func (s *Server) handleLogo(w http.ResponseWriter, r *http.Request) {
	ex, err := os.Executable()
	if err != nil {
		http.Error(w, "logo not found", 404)
		return
	}
	baseDir := filepath.Dir(ex)
	candidates := []string{
		filepath.Join(baseDir, "frontend", "src", "logo.png"),
		filepath.Join(baseDir, "frontend", "dist", "assets", "logo-fDMdz5zR.png"),
		filepath.Join(baseDir, "logo.png"),
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			w.Header().Set("Content-Type", "image/png")
			http.ServeFile(w, r, p)
			return
		}
	}
	http.Error(w, "logo not found", 404)
}

const adminPageHTML = `<!doctype html>
<html lang="ru">
<head>
<meta charset="UTF-8"/>
<meta name="viewport" content="width=device-width,initial-scale=1.0"/>
<title>NODAX License Server</title>
<style>
@import url('https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700;800&family=JetBrains+Mono:wght@400;500&display=swap');
:root{
  --primary:#16a2a7;--primary-hover:#11858a;--success:#18a05e;--warning:#d17a22;--danger:#d33b3b;
  --bg-app:#f2f4f6;--bg-card:#ffffff;--bg-sidebar:rgba(255,255,255,0.82);--border-main:#eef1f4;--border-hover:#d9e0e6;
  --shadow-card:0 6px 20px rgba(15,23,42,0.06);--text-main:#1f2937;--text-dim:#4b5563;--text-muted:#9aa3af;
  --radius-sm:6px;--radius-md:10px;--radius-lg:14px;
}
*{box-sizing:border-box}
body{margin:0;font-family:'Inter',-apple-system,sans-serif;font-size:13px;line-height:1.4;overflow:hidden;color:var(--text-main)}
.app-bg{position:fixed;inset:0;z-index:-1;background:radial-gradient(ellipse 80% 80% at 30% 50%,rgba(120,190,60,0.45) 0%,transparent 60%),radial-gradient(ellipse 70% 70% at 70% 40%,rgba(30,140,150,0.5) 0%,transparent 60%),radial-gradient(ellipse 90% 90% at 50% 80%,rgba(40,120,90,0.4) 0%,transparent 60%),linear-gradient(135deg,#2d6a4f 0%,#40916c 25%,#74b35a 45%,#52b69a 65%,#1a8a8a 85%,#1b6b6b 100%)}
.app{display:flex;height:100vh}
.sidebar{width:250px;flex-shrink:0;display:flex;flex-direction:column;background:rgba(255,255,255,0.82);backdrop-filter:blur(16px);-webkit-backdrop-filter:blur(16px);border-right:1px solid rgba(255,255,255,0.3);box-shadow:1px 0 12px rgba(0,0,0,0.06)}
.sidebar-header{display:flex;align-items:center;gap:12px;padding:14px 16px;border-bottom:1px solid var(--border-main)}
.sidebar-logo{height:38px;width:auto;object-fit:contain}
.logo-text{font-size:13px;font-weight:800;letter-spacing:1px;text-transform:uppercase}
.label-gradient{background:linear-gradient(135deg,#e8820c 0%,#d4a017 40%,#5cb85c 70%,#16a2a7 100%);-webkit-background-clip:text;-webkit-text-fill-color:transparent;background-clip:text;font-weight:900}
.sidebar-nav{flex:1;padding:6px 8px;overflow-y:auto}
.nav-item{display:flex;align-items:center;gap:10px;width:100%;padding:9px 14px;border:none;background:transparent;color:var(--text-dim);font-size:13px;font-weight:600;border-radius:var(--radius-sm);cursor:pointer;transition:all .12s;text-align:left}
.nav-item:hover{background:rgba(22,162,167,0.08);color:var(--text-main)}
.nav-item.active{background:rgba(22,162,167,0.14);color:var(--primary)}
.nav-icon{font-size:15px;width:20px;text-align:center}
.nav-section{font-size:10px;font-weight:700;text-transform:uppercase;color:var(--text-muted);padding:14px 14px 4px;letter-spacing:1.2px}
.sidebar-user{padding:12px 16px;border-top:1px solid var(--border-main);display:flex;align-items:center;gap:8px;margin-top:auto}
.sidebar-user-name{font-size:12px;font-weight:600;color:var(--text-main);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.sidebar-logout{background:none;border:1px solid var(--border-main);color:var(--text-muted);width:28px;height:28px;border-radius:8px;cursor:pointer;font-size:14px;display:flex;align-items:center;justify-content:center;transition:all .15s;flex-shrink:0}
.sidebar-logout:hover{background:rgba(211,59,59,0.15);color:var(--danger);border-color:rgba(211,59,59,0.3)}
.main-content{flex:1;padding:24px 28px;overflow-y:auto;max-height:100vh}
h1{font-size:20px;font-weight:800;margin-bottom:20px;color:#fff;text-shadow:0 1px 8px rgba(0,0,0,0.5),0 0 2px rgba(0,0,0,0.3)}
h2{font-size:15px;font-weight:700;margin:20px 0 12px;color:#fff;text-shadow:0 1px 6px rgba(0,0,0,0.5),0 0 2px rgba(0,0,0,0.3)}
.card{background:#fff;border:1px solid var(--border-main);border-radius:var(--radius-md);padding:18px 20px;box-shadow:var(--shadow-card);margin-bottom:14px}
.overview-cards{display:grid;grid-template-columns:repeat(auto-fill,minmax(180px,1fr));gap:14px;margin-bottom:20px}
.ov-card{background:#fff;border:1px solid var(--border-main);border-radius:var(--radius-md);padding:18px 20px;text-align:center;box-shadow:var(--shadow-card)}
.ov-value{font-size:28px;font-weight:800;color:var(--primary)}
.ov-label{font-size:11px;font-weight:600;color:var(--text-muted);margin-top:4px;text-transform:uppercase;letter-spacing:0.4px}
.row{display:flex;gap:10px;flex-wrap:wrap;align-items:center}
input,select,textarea{padding:6px 10px;border:1px solid var(--border-main);border-radius:var(--radius-sm);font-size:13px;background:#fff;color:var(--text-main);font-family:inherit;outline:none}
input:focus,select:focus,textarea:focus{border-color:var(--primary)}
.field{display:flex;flex-direction:column;gap:4px}
.field label{font-size:11px;font-weight:600;color:var(--text-dim);text-transform:uppercase;letter-spacing:0.5px}
button{font-family:inherit;font-size:12px;font-weight:600;cursor:pointer;border:1px solid transparent;transition:all 0.1s ease;padding:6px 12px;border-radius:var(--radius-sm)}
.btn{background:var(--primary);color:#fff}
.btn:hover{background:var(--primary-hover)}
.btn-ghost{background:transparent;color:var(--text-dim);border:1px solid var(--border-main)}
.btn-ghost:hover{background:var(--bg-app);border-color:var(--border-hover)}
.btn-danger{background:rgba(211,59,59,0.1);color:var(--danger);border:1px solid rgba(211,59,59,0.25)}
.btn-danger:hover{background:rgba(211,59,59,0.18)}
.btn-sm{padding:4px 10px;font-size:11px}
table{width:100%;border-collapse:collapse;font-size:12px;background:#fff;border-radius:var(--radius-sm);overflow:hidden}
th,td{border-bottom:1px solid var(--border-main);text-align:left;padding:10px 12px}
th{background:#f8fafc;font-size:10px;text-transform:uppercase;letter-spacing:0.5px;color:var(--text-dim);font-weight:700}
tbody tr:hover{background:#f0f7ff}
.status{font-size:10px;font-weight:700;padding:2px 8px;border-radius:6px;display:inline-block}
.s-active{background:#dcfce7;color:var(--success)}
.s-revoked{background:#fee2e2;color:var(--danger)}
.s-expired{background:#fef3c7;color:var(--warning)}
.tabs{display:flex;gap:0;margin-bottom:14px;border-bottom:2px solid var(--border-main)}
.tab{padding:10px 18px;font-weight:600;font-size:13px;cursor:pointer;border-bottom:3px solid transparent;margin-bottom:-2px;color:var(--text-muted);transition:color .15s,border-color .15s;white-space:nowrap}
.tab:hover{color:var(--text-main)}
.tab.active{color:var(--primary);border-bottom-color:var(--primary)}
.tab-content{display:none}
.tab-content.active{display:block}
.login-wrap{display:flex;align-items:center;justify-content:center;height:100vh;padding:20px}
.login-card{background:#fff;border-radius:var(--radius-lg);padding:32px;width:100%;max-width:380px;box-shadow:0 24px 64px rgba(0,0,0,0.2),0 0 0 1px rgba(255,255,255,0.3)}
.login-card h2{margin:0 0 20px;font-size:18px;font-weight:700;text-align:center;color:var(--text-main);text-shadow:none}
.login-card .field{margin-bottom:14px}
.login-card input{width:100%}
.login-card button{width:100%;margin-top:4px;padding:10px}
.msg{font-size:12px;font-weight:600;color:var(--primary);margin-top:6px}
.msg.error{color:var(--danger)}
.filter-bar{display:flex;gap:8px;margin-bottom:10px;align-items:center;flex-wrap:wrap}
.pager{display:flex;gap:8px;align-items:center;justify-content:center;margin-top:10px;font-size:12px}
.modal-overlay{position:fixed;top:0;left:0;right:0;bottom:0;background:rgba(15,23,42,.45);backdrop-filter:blur(4px);z-index:100;display:none;align-items:center;justify-content:center}
.modal-overlay.show{display:flex}
.modal{background:#fff;border-radius:var(--radius-lg);padding:24px;max-width:480px;width:92%;box-shadow:0 24px 64px rgba(15,23,42,.22)}
.modal h2{margin:0 0 16px;font-size:18px;color:var(--text-main);text-shadow:none}
.confirm-modal{text-align:center;max-width:380px}
.confirm-icon{width:56px;height:56px;border-radius:50%;display:inline-flex;align-items:center;justify-content:center;font-size:28px;margin-bottom:12px}
.confirm-icon.warn{background:linear-gradient(135deg,#fef3c7,#fde68a);color:#92400e}
.confirm-icon.danger{background:linear-gradient(135deg,#fee2e2,#fecaca);color:var(--danger)}
.export-wrap{position:relative;display:inline-block}
.export-menu{display:none;position:absolute;top:100%;right:0;margin-top:4px;background:#fff;border:1px solid var(--border-main);border-radius:var(--radius-sm);box-shadow:0 8px 24px rgba(15,23,42,.12);z-index:20;min-width:150px;overflow:hidden}
.export-menu.show{display:block}
.export-menu a{display:flex;align-items:center;gap:8px;padding:10px 14px;font-size:12px;font-weight:600;color:var(--text-dim);text-decoration:none;cursor:pointer;transition:background .12s}
.export-menu a:hover{background:#f0f7ff;color:var(--primary)}
.tag{display:inline-block;font-size:10px;padding:2px 8px;border-radius:6px;font-weight:600}
.tag-basic{background:#e0f2fe;color:#0369a1}
.tag-pro{background:#dcfce7;color:var(--success)}
.tag-enterprise{background:#f3e8ff;color:#7c3aed}
.action-row{display:flex;gap:6px;align-items:center}
.icon-btn{width:28px;height:28px;padding:0;border-radius:8px;display:inline-flex;align-items:center;justify-content:center;font-size:14px}
.icon-btn.edit{background:#eef2ff;color:#3730a3;border:1px solid #c7d2fe}
.icon-btn.extend{background:#ecfeff;color:#155e75;border:1px solid #a5f3fc}
.icon-btn.revoke{background:#fff7ed;color:#9a3412;border:1px solid #fed7aa}
.icon-btn.restore{background:#ecfdf5;color:#166534;border:1px solid #86efac}
.icon-btn.delete{background:#fef2f2;color:#991b1b;border:1px solid #fca5a5}
.host-meta{display:flex;flex-direction:column;gap:2px}
.host-name{font-weight:600}
.host-ip{font-size:11px;color:var(--text-muted);font-family:'JetBrains Mono',monospace}
.create-grid{display:grid;grid-template-columns:repeat(4,1fr);gap:10px;align-items:end}
.finance-grid{display:grid;grid-template-columns:repeat(4,1fr);gap:10px;margin-bottom:14px}
.kpi{background:#fff;border:1px solid var(--border-main);border-radius:var(--radius-md);padding:14px;box-shadow:var(--shadow-card)}
.kpi .label{font-size:10px;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.4px}
.kpi .val{font-size:24px;font-weight:800;margin-top:4px;color:var(--text-main)}
.price-grid{display:grid;grid-template-columns:1fr 1fr 1fr 1fr auto;gap:10px;align-items:end}
.chart-row{display:grid;grid-template-columns:1fr 1fr;gap:14px;margin-top:14px}
.chart-box{background:#fff;border:1px solid var(--border-main);border-radius:var(--radius-md);padding:14px;min-height:180px;box-shadow:var(--shadow-card)}
.chart-box h3{margin:0 0 10px;font-size:14px;color:var(--text-main);text-shadow:none}
.settings-grid{display:grid;grid-template-columns:1fr 1fr;gap:14px}
.settings-grid .card{margin-bottom:0}
@media(max-width:1024px){.sidebar{width:220px}.create-grid{grid-template-columns:1fr 1fr}.finance-grid,.price-grid{grid-template-columns:1fr 1fr}.settings-grid,.chart-row{grid-template-columns:1fr}}
@media(max-width:760px){.sidebar{display:none}.create-grid{grid-template-columns:1fr}.finance-grid,.price-grid{grid-template-columns:1fr}}
</style>
</head>
<body>
<div class="app-bg"></div>
<div id="app" class="app">
<!-- Login View -->
<div id="loginView" style="display:none;width:100%;height:100%">
<div class="login-wrap">
<div class="login-card">
<h2>NODAX License Server</h2>
<div class="field"><label>Логин</label><input id="loginUser" value="admin"/></div>
<div class="field"><label>Пароль</label><input id="loginPass" type="password" placeholder="Пароль"/></div>
<button id="btnLogin" type="button" class="btn">Войти</button>
<div id="loginMsg" class="msg"></div>
</div>
</div>
</div>
<!-- Main View -->
<div id="mainView" style="display:none;width:100%;height:100%;display:flex">
<!-- Sidebar -->
<div class="sidebar">
<div class="sidebar-header">
<img class="sidebar-logo" src="/assets/logo" alt="NODAX"/>
</div>
<div class="sidebar-nav">
<div class="nav-section">License Server</div>
<button class="nav-item active" data-tab="licenses"><span class="nav-icon">🔑</span>Лицензии</button>
<button class="nav-item" data-tab="finance"><span class="nav-icon">💰</span>Финансы</button>
<button class="nav-item" data-tab="audit"><span class="nav-icon">📋</span>Журнал</button>
<button class="nav-item" data-tab="settings"><span class="nav-icon">⚙️</span>Настройки</button>
</div>
<div class="sidebar-user">
<span class="sidebar-user-name">admin</span>
<button id="btnLogout" class="sidebar-logout" title="Выйти">→</button>
</div>
</div>
<!-- Main Content -->
<div class="main-content">
<h1>Управление лицензиями</h1>
<div id="msg" class="msg"></div>
<!-- Licenses Tab -->
<div id="tab-licenses" class="tab-content active">
<div class="card">
<h2 style="margin-top:0">Создать лицензию</h2>
<div class="create-grid">
<div class="field"><label>Клиент</label><input id="customer" placeholder="Имя"/></div>
<div class="field"><label>Компания</label><input id="custCompany" placeholder="ООО"/></div>
<div class="field"><label>Email</label><input id="custEmail" type="email" placeholder="email"/></div>
<div class="field"><label>Telegram</label><input id="custTg" placeholder="@username"/></div>
<div class="field"><label>Телефон</label><input id="custPhone" placeholder="+7"/></div>
<div class="field"><label>Тариф</label><select id="plan"><option value="basic">basic</option><option value="pro">pro</option><option value="enterprise">enterprise</option></select></div>
<div class="field"><label>Лимит</label><input id="maxAgents" type="number" min="0" value="10"/></div>
<div class="field"><label>Дней</label><input id="validDays" type="number" min="1" value="365"/></div>
<div class="field"><label>Trial</label><select id="isTrial"><option value="0">Нет</option><option value="1">Да</option></select></div>
<div class="field"><label>Комментарий</label><input id="notes" placeholder="Контракт"/></div>
<div></div>
<button id="btnCreate" type="button" class="btn">Создать</button>
</div>
</div>
<div class="card">
<div class="row" style="justify-content:space-between;margin-bottom:8px">
<h2 style="margin:0">Лицензии</h2>
<div class="row">
<div class="export-wrap"><button id="btnExportToggle" type="button" class="btn-ghost btn-sm">Экспорт ▾</button>
<div id="exportMenu" class="export-menu">
<a data-fmt="csv"><span>📄</span>CSV</a>
<a data-fmt="xlsx"><span>📊</span>Excel</a>
<a data-fmt="html"><span>🌐</span>HTML</a>
</div></div>
<button id="btnRefresh" type="button" class="btn-ghost btn-sm">Обновить</button>
</div>
</div>
<div class="filter-bar">
<input id="searchInput" placeholder="Поиск по клиенту / ключу..." style="flex:1;min-width:200px"/>
<select id="filterStatus"><option value="">Все статусы</option><option value="active">active</option><option value="revoked">revoked</option><option value="expired">expired</option></select>
<select id="filterPlan"><option value="">Все тарифы</option><option value="basic">basic</option><option value="pro">pro</option><option value="enterprise">enterprise</option></select>
</div>
<table><thead><tr><th>Клиент / Компания</th><th>Email</th><th>Telegram</th><th>Телефон</th><th>Ключ</th><th>План</th><th>Статус</th><th>Истекает</th><th>Хост</th><th>Действия</th></tr></thead>
<tbody id="licensesBody"></tbody></table>
<div class="pager"><button id="pgPrev" type="button" class="btn-ghost btn-sm">Назад</button><span id="pgInfo">-</span><button id="pgNext" type="button" class="btn-ghost btn-sm">Вперед</button></div>
</div>
</div>
<!-- Finance Tab -->
<div id="tab-finance" class="tab-content">
<div class="card">
<h2 style="margin-top:0">Финансовая аналитика</h2>
<div class="finance-grid">
<div class="kpi"><div class="label">Активных</div><div id="kpiActive" class="val">0</div></div>
<div class="kpi"><div class="label">MRR</div><div id="kpiMRR" class="val">0</div></div>
<div class="kpi"><div class="label">ARR</div><div id="kpiARR" class="val">0</div></div>
<div class="kpi"><div class="label">Доход/лиц</div><div id="kpiARPL" class="val">0</div></div>
</div>
<h2>Цены тарифов</h2>
<div class="price-grid">
<div class="field"><label>basic/год</label><input id="priceBasic" type="number" min="0" step="0.01" value="0"/></div>
<div class="field"><label>pro/год</label><input id="pricePro" type="number" min="0" step="0.01" value="0"/></div>
<div class="field"><label>enterprise/год</label><input id="priceEnterprise" type="number" min="0" step="0.01" value="0"/></div>
<div class="field"><label>Валюта</label><input id="priceCurrency" value="RUB"/></div>
<button id="btnSaveFinance" type="button" class="btn-ghost">Сохранить</button>
</div>
<div class="chart-row">
<div class="chart-box"><h3>Тарифы</h3><canvas id="chartDonut" width="260" height="160"></canvas></div>
<div class="chart-box"><h3>Статусы</h3><canvas id="chartStatus" width="260" height="160"></canvas></div>
</div>
</div>
</div>
<!-- Audit Tab -->
<div id="tab-audit" class="tab-content">
<div class="card">
<div class="row" style="justify-content:space-between;margin-bottom:8px"><h2 style="margin:0">Журнал аудита</h2><button id="btnRefreshAudit" type="button" class="btn-ghost btn-sm">Обновить</button></div>
<table><thead><tr><th>Время</th><th>Действие</th><th>License ID</th><th>Актор</th><th>Детали</th></tr></thead>
<tbody id="auditBody"></tbody></table>
<div class="pager"><button id="auditPrev" type="button" class="btn-ghost btn-sm">Назад</button><span id="auditInfo">-</span><button id="auditNext" type="button" class="btn-ghost btn-sm">Вперед</button></div>
</div>
</div>
<!-- Settings Tab -->
<div id="tab-settings" class="tab-content">
<div class="settings-grid">
<div class="card"><h2 style="margin-top:0">Смена пароля</h2>
<div class="field"><label>Текущий</label><input id="oldPass" type="password"/></div>
<div class="field"><label>Новый</label><input id="newPass" type="password"/></div>
<div class="field"><label>Подтверждение</label><input id="newPass2" type="password"/></div>
<button id="btnChangePass" type="button" class="btn">Сменить</button>
</div>
<div class="card"><h2 style="margin-top:0">Telegram</h2>
<div class="field"><label>Bot Token</label><input id="tgToken" placeholder="123456:ABC..."/></div>
<div class="field"><label>Chat ID (авто из бота)</label><input id="tgChat" placeholder="-100123..."/></div>
<div class="field"><label>Уведомлять за (дней)</label><input id="tgDays" type="number" min="1" value="7"/></div>
<div class="field"><label>Webhook URL</label><input id="whUrl" placeholder="https://example.com/webhook"/></div>
<div class="field"><label>Общее сообщение клиентам</label><textarea id="tgBroadcastMsg" rows="3" placeholder="Введите текст рассылки клиентам..."></textarea></div>
<div class="row"><button id="btnSaveTg" type="button" class="btn-ghost btn-sm">Сохранить</button><button id="btnTestTg" type="button" class="btn-ghost btn-sm">Тест</button></div>
<div class="row"><button id="btnBroadcastClients" type="button" class="btn-ghost btn-sm">Отправить всем клиентам</button></div>
</div>
<div class="card"><h2 style="margin-top:0">API-ключи</h2>
<div class="row" style="margin-bottom:8px">
<input id="akName" placeholder="Название" style="flex:1"/><select id="akRole"><option value="readonly">readonly</option><option value="full">full</option></select>
<button id="btnCreateAK" type="button" class="btn btn-sm">Создать</button>
</div>
<div id="apiKeysList"></div>
</div>
<div class="card"><h2 style="margin-top:0">Бэкап</h2>
<div class="row"><button id="btnBackup" type="button" class="btn-ghost btn-sm">Скачать</button>
<button id="btnRestoreBtn" type="button" class="btn-ghost btn-sm">Восстановить</button>
<input id="restoreFile" type="file" accept=".json" style="display:none"/></div>
</div>
</div>
</div>
</div>
</div>
</div>
<!-- Modal -->
<div id="confirmModal" class="modal-overlay">
<div class="modal confirm-modal">
<div id="confirmIcon" class="confirm-icon warn">⚠️</div>
<h3 id="confirmTitle">Подтверждение</h3>
<p id="confirmText">Вы уверены?</p>
<div class="row" style="justify-content:center">
<button id="confirmNo" type="button" class="btn-ghost">Отмена</button>
<button id="confirmYes" type="button" class="btn">Подтвердить</button>
</div>
</div>
</div>
<div id="editModal" class="modal-overlay">
<div class="modal">
<h2>Редактировать лицензию</h2>
<input id="edId" type="hidden"/>
<div class="row">
<div class="field"><label>Клиент</label><input id="edCustomer"/></div>
<div class="field"><label>Компания</label><input id="edCompany"/></div>
<div class="field"><label>Email</label><input id="edEmail" type="email"/></div>
<div class="field"><label>Telegram</label><input id="edTg"/></div>
<div class="field"><label>Телефон</label><input id="edPhone"/></div>
<div class="field"><label>План</label><select id="edPlan"><option value="basic">basic</option><option value="pro">pro</option><option value="enterprise">enterprise</option></select></div>
<div class="field"><label>Лимит</label><input id="edMaxAgents" type="number" min="0"/></div>
<div class="field"><label>Комментарий</label><input id="edNotes"/></div>
</div>
<div class="row" style="justify-content:flex-end;margin-top:12px">
<button id="btnEdCancel" type="button" class="btn-ghost">Отмена</button>
<button id="btnEdSave" type="button" class="btn">Сохранить</button>
</div>
</div>
</div>
<script>
const $=id=>document.getElementById(id);const msg=$('msg');const loginMsg=$('loginMsg');
let allItems=[],lastItems=[],curPage=0,pageSize=20,auditItems=[],auditPage=0;

function showMsg(t,e){if(msg){msg.textContent=t||'';msg.style.color=e?'#b91c1c':'#0f766e';}}
function showLoginMsg(t,e){if(loginMsg){loginMsg.textContent=t||'';loginMsg.style.color=e?'#b91c1c':'#0f766e';}}
function esc(v){return(v==null?'':String(v)).replaceAll('&','&amp;').replaceAll('<','&lt;').replaceAll('>','&gt;');}

let _confirmResolve=null;
function askConfirm(title,text,type){return new Promise(resolve=>{
  _confirmResolve=resolve;
  const icons={danger:'❌',warn:'⚠️',info:'ℹ️'};
  $('confirmIcon').textContent=icons[type]||icons.warn;
  $('confirmIcon').className='confirm-icon '+(type||'warn');
  $('confirmTitle').textContent=title||'Подтверждение';
  $('confirmText').textContent=text||'Вы уверены?';
  const yb=$('confirmYes');
  yb.className=type==='danger'?'btn-danger':'btn';
  yb.textContent=type==='danger'?'Удалить':'Подтвердить';
  $('confirmModal').classList.add('show');
});}
function closeConfirm(val){$('confirmModal').classList.remove('show');if(_confirmResolve){_confirmResolve(val);_confirmResolve=null;}}
function switchView(a){$('loginView').style.display=a?'none':'block';$('mainView').style.display=a?'flex':'none';}

async function checkAuth(){try{const r=await fetch('/api/v1/auth/me');const d=await r.json().catch(()=>({}));return!!d.authenticated;}catch(_){return false;}}

async function doLogin(){
  const u=($('loginUser')?.value||'').trim(),p=$('loginPass')?.value||'';
  if(!u||!p){showLoginMsg('Заполните логин и пароль',true);return;}
  try{const r=await fetch('/api/v1/auth/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({username:u,password:p})});
  const d=await r.json().catch(()=>({}));if(!r.ok)throw new Error(d.error||'Ошибка');showLoginMsg('');switchView(true);loadAll();}catch(e){showLoginMsg(e.message,true);}
}
async function doLogout(){await fetch('/api/v1/auth/logout',{method:'POST'}).catch(()=>{});switchView(false);}

async function doChangePassword(){
  const o=$('oldPass')?.value||'',n=$('newPass')?.value||'',n2=$('newPass2')?.value||'';
  if(!o||!n){showMsg('Заполните поля',true);return;}if(n!==n2){showMsg('Пароли не совпадают',true);return;}
  if(n.length<4){showMsg('Мин. 4 символа',true);return;}
  try{const r=await fetch('/api/v1/auth/change-password',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({oldPassword:o,newPassword:n})});
  const d=await r.json().catch(()=>({}));if(!r.ok)throw new Error(d.error||'Ошибка');showMsg('Пароль изменен',false);$('oldPass').value='';$('newPass').value='';$('newPass2').value='';}catch(e){showMsg(e.message,true);}
}

function planLim(p){p=String(p||'').toLowerCase();return p==='pro'?30:p==='enterprise'?0:10;}
function applyPlanDef(){const pi=$('plan'),ma=$('maxAgents');if(pi&&ma)ma.value=String(planLim(pi.value));
  const tr=$('isTrial');if(tr&&tr.value==='1'){$('validDays').value='14';}}
function fmtExp(v){if(!v)return'';const t=Date.parse(v);if(!Number.isFinite(t))return esc(v);const d=Math.ceil((t-Date.now())/864e5);
  if(d>0)return esc(v.slice(0,10))+' <span class="muted">('+d+'д)</span>';if(d===0)return esc(v.slice(0,10))+' <span class="muted">(сегодня)</span>';
  return esc(v.slice(0,10))+' <span style="color:#dc2626">('+Math.abs(d)+'д назад)</span>';}

function getFiltered(){
  const q=($('searchInput')?.value||'').toLowerCase(),st=$('filterStatus')?.value||'',pl=$('filterPlan')?.value||'';
  return allItems.filter(x=>{
    if(st&&String(x.status||'').toLowerCase()!==st)return false;
    if(pl&&String(x.plan||'').toLowerCase()!==pl)return false;
    if(q&&!(x.customerName||'').toLowerCase().includes(q)&&!(x.licenseKey||'').toLowerCase().includes(q)&&!(x.notes||'').toLowerCase().includes(q))return false;
    return true;
  });
}

function renderLicenses(){
  const filtered=getFiltered();lastItems=filtered;const total=filtered.length;const pages=Math.max(1,Math.ceil(total/pageSize));
  if(curPage>=pages)curPage=pages-1;if(curPage<0)curPage=0;
  const start=curPage*pageSize,slice=filtered.slice(start,start+pageSize);
  const body=$('licensesBody');
  body.innerHTML=slice.map(x=>{
    const sc='s-'+((x.status||'unknown').toLowerCase());const rev=String(x.status||'').toLowerCase()==='revoked';
    const hostName=x.lastHostname?esc(x.lastHostname):'-';
    const hostIP=x.lastIP?esc(x.lastIP):'';
    const host='<div class="host-meta"><span class="host-name">'+hostName+'</span>'+(hostIP?'<span class="host-ip">'+hostIP+'</span>':'')+'</div>';
    const ab=rev?'<button type="button" class="icon-btn restore" title="Восстановить" data-action="restore" data-id="'+esc(x.id)+'">↺</button>'
      :'<button type="button" class="icon-btn revoke" title="Отозвать" data-action="revoke" data-id="'+esc(x.id)+'">⛔</button>';
    const trial=x.isTrial?' <span class="tag">trial</span>':'';
    const cname=esc(x.customerName)+(x.customerCompany?' <span class="muted">('+esc(x.customerCompany)+')</span>':'');
    const email=x.customerEmail?esc(x.customerEmail):'<span class="muted">-</span>';
    const tg=x.customerTelegram?esc(x.customerTelegram):'<span class="muted">-</span>';
    const phone=x.customerPhone?esc(x.customerPhone):'<span class="muted">-</span>';
    return '<tr><td>'+cname+trial+'</td><td>'+email+'</td><td>'+tg+'</td><td>'+phone+'</td><td><code>'+esc(x.licenseKey)+'</code></td><td>'+esc(x.plan)+'</td><td><span class="status '+sc+'">'+esc(x.status)+'</span></td><td>'+fmtExp(x.expiresAt)+'</td><td>'+host+'</td><td><div class="action-row"><button type="button" class="icon-btn edit" title="Редактировать" data-action="edit" data-id="'+esc(x.id)+'">✎</button><button type="button" class="icon-btn extend" title="Продлить на 30 дней" data-action="extend" data-id="'+esc(x.id)+'">⏱</button>'+ab+'<button type="button" class="icon-btn delete" title="Удалить" data-action="delete" data-id="'+esc(x.id)+'">🗑</button></div></td></tr>';
  }).join('');
  $('pgInfo').textContent='Стр. '+(curPage+1)+'/'+pages+' ('+total+')';
  recomputeFinance(allItems);
}

async function loadLicenses(){
  try{const r=await fetch('/api/v1/licenses');if(r.status===401){switchView(false);return;}
  const d=await r.json().catch(()=>({}));if(!r.ok)throw new Error(d.error||'HTTP '+r.status);
  allItems=(d.items||[]).slice().sort((a,b)=>(Date.parse(b?.createdAt||'')||0)-(Date.parse(a?.createdAt||'')||0));
  renderLicenses();showMsg('Лицензий: '+allItems.length,false);}catch(e){showMsg(e.message,true);}
}

async function createLicense(){
  try{applyPlanDef();const rawD=parseInt(($('validDays')?.value||'').trim(),10);
  const vd=Number.isFinite(rawD)&&rawD>0?rawD:365;const ea=new Date(Date.now()+vd*864e5).toISOString();
  const trial=$('isTrial')?.value==='1';
  const pl={customerName:$('customer').value.trim(),customerEmail:$('custEmail').value.trim(),customerTelegram:$('custTg').value.trim(),customerPhone:$('custPhone').value.trim(),customerCompany:$('custCompany').value.trim(),plan:$('plan').value,maxAgents:Number($('maxAgents').value||0),validDays:trial?14:vd,expiresAt:trial?new Date(Date.now()+14*864e5).toISOString():ea,notes:$('notes').value.trim()};
  if(!pl.customerName)throw new Error('Укажите клиента');
  const r=await fetch('/api/v1/licenses',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(pl)});
  const d=await r.json().catch(()=>({}));if(!r.ok)throw new Error(d.error||'HTTP '+r.status);
  showMsg('Создана: '+(d.licenseKey||''),false);await loadLicenses();}catch(e){showMsg(e.message,true);}
}

async function licAction(id,action){
  if(action==='revoke'){if(!await askConfirm('Отозвать лицензию','Лицензия будет деактивирована. Можно вернуть позже.','warn'))return;}
  if(action==='restore'){if(!await askConfirm('Вернуть лицензию','Лицензия снова станет активной.','info'))return;}
  if(action==='delete'){if(!await askConfirm('Удалить лицензию','Лицензия будет удалена безвозвратно. Это действие нельзя отменить.','danger'))return;
    try{const r=await fetch('/api/v1/licenses/'+encodeURIComponent(id),{method:'DELETE'});const d=await r.json().catch(()=>({}));if(!r.ok)throw new Error(d.error||'Err');showMsg('Удалена',false);await loadLicenses();}catch(e){showMsg(e.message,true);}return;}
  if(action==='edit'){openEditModal(id);return;}
  try{const opts={method:'POST',headers:{'Content-Type':'application/json'}};
  if(action==='extend')opts.body=JSON.stringify({days:30});
  const r=await fetch('/api/v1/licenses/'+encodeURIComponent(id)+'/'+action,opts);
  const d=await r.json().catch(()=>({}));if(!r.ok)throw new Error(d.error||'HTTP '+r.status);
  showMsg(action==='extend'?'Продлена':action==='revoke'?'Отозвана':'Возвращена',false);await loadLicenses();}catch(e){showMsg(e.message,true);}
}

function openEditModal(id){
  const lic=allItems.find(x=>x.id===id);if(!lic)return;
  $('edId').value=id;$('edCustomer').value=lic.customerName||'';$('edCompany').value=lic.customerCompany||'';
  $('edEmail').value=lic.customerEmail||'';$('edTg').value=lic.customerTelegram||'';$('edPhone').value=lic.customerPhone||'';
  $('edPlan').value=lic.plan||'basic';$('edMaxAgents').value=String(lic.maxAgents||0);$('edNotes').value=lic.notes||'';
  $('editModal').classList.add('show');
}
async function saveEdit(){
  const id=$('edId').value;if(!id)return;
  try{const r=await fetch('/api/v1/licenses/'+encodeURIComponent(id),{method:'PATCH',headers:{'Content-Type':'application/json'},
    body:JSON.stringify({customerName:$('edCustomer').value.trim(),customerCompany:$('edCompany').value.trim(),customerEmail:$('edEmail').value.trim(),customerTelegram:$('edTg').value.trim(),customerPhone:$('edPhone').value.trim(),plan:$('edPlan').value,maxAgents:Number($('edMaxAgents').value||0),notes:$('edNotes').value.trim()})});
  const d=await r.json().catch(()=>({}));if(!r.ok)throw new Error(d.error||'Ошибка');
  $('editModal').classList.remove('show');showMsg('Обновлено',false);await loadLicenses();}catch(e){showMsg(e.message,true);}
}

function finCfg(){const b=Number($('priceBasic')?.value||0),p=Number($('pricePro')?.value||0),e=Number($('priceEnterprise')?.value||0);
  const c=String($('priceCurrency')?.value||'RUB').trim().toUpperCase()||'RUB';
  return{basic:b>=0?b:0,pro:p>=0?p:0,enterprise:e>=0?e:0,currency:c};}
function saveFin(){localStorage.setItem('license_finance_cfg',JSON.stringify(finCfg()));recomputeFinance(allItems);showMsg('Цены сохранены',false);}
function loadFin(){const raw=localStorage.getItem('license_finance_cfg');if(!raw){if($('priceBasic'))$('priceBasic').value='1000';if($('pricePro'))$('pricePro').value='3000';if($('priceEnterprise'))$('priceEnterprise').value='5000';if($('priceCurrency'))$('priceCurrency').value='RUB';return;}try{const c=JSON.parse(raw);
  if($('priceBasic'))$('priceBasic').value=String(c.basic??0);if($('pricePro'))$('pricePro').value=String(c.pro??0);
  if($('priceEnterprise'))$('priceEnterprise').value=String(c.enterprise??0);
  const cur=String(c.currency||'').trim().toUpperCase();const m=!cur||cur==='USD'||cur==='$'?'RUB':cur;
  if($('priceCurrency'))$('priceCurrency').value=m;if(m!==cur){c.currency=m;localStorage.setItem('license_finance_cfg',JSON.stringify(c));}}catch(_){}}
function money(v,c){return new Intl.NumberFormat('ru-RU',{style:'currency',currency:c,maximumFractionDigits:0}).format(v);}
function recomputeFinance(items){
  const cfg=finCfg();const active=(items||[]).filter(x=>String(x?.status||'').toLowerCase()==='active');
  let arr=0;for(const x of active){const p=String(x?.plan||'').toLowerCase();arr+=p==='pro'?cfg.pro:p==='enterprise'?cfg.enterprise:cfg.basic;}
  const cnt=active.length,mrr=arr/12,arpl=cnt>0?(arr/cnt):0;
  if($('kpiActive'))$('kpiActive').textContent=String(cnt);if($('kpiMRR'))$('kpiMRR').textContent=money(mrr,cfg.currency);
  if($('kpiARR'))$('kpiARR').textContent=money(arr,cfg.currency);if($('kpiARPL'))$('kpiARPL').textContent=money(arpl,cfg.currency);
  drawCharts(items);
}

function drawCharts(items){
  const plans={basic:0,pro:0,enterprise:0};const statuses={active:0,revoked:0,expired:0};
  for(const x of(items||[])){const p=String(x.plan||'basic').toLowerCase();plans[p]=(plans[p]||0)+1;
    let st=String(x.status||'').toLowerCase();if(st==='active'){const exp=Date.parse(x.expiresAt||'');if(exp&&exp<Date.now())st='expired';}
    statuses[st]=(statuses[st]||0)+1;}
  drawDonut($('chartDonut'),plans,{basic:'#0891b2',pro:'#0f766e',enterprise:'#6366f1'});
  drawDonut($('chartStatus'),statuses,{active:'#16a34a',revoked:'#dc2626',expired:'#d97706'});
}
function drawDonut(canvas,data,colors){
  if(!canvas)return;const ctx=canvas.getContext('2d');const w=canvas.width,h=canvas.height;ctx.clearRect(0,0,w,h);
  const cx=80,cy=h/2,r=55,ir=32;const entries=Object.entries(data).filter(e=>e[1]>0);const total=entries.reduce((s,e)=>s+e[1],0);
  if(!total){ctx.fillStyle='#94a3b8';ctx.font='12px sans-serif';ctx.fillText('Нет данных',cx-25,cy+4);return;}
  let angle=-Math.PI/2;for(const[k,v]of entries){const slice=v/total*Math.PI*2;
    ctx.beginPath();ctx.moveTo(cx,cy);ctx.arc(cx,cy,r,angle,angle+slice);ctx.closePath();ctx.fillStyle=colors[k]||'#94a3b8';ctx.fill();angle+=slice;}
  ctx.beginPath();ctx.arc(cx,cy,ir,0,Math.PI*2);ctx.fillStyle='#f8fbff';ctx.fill();
  ctx.fillStyle='#0f172a';ctx.font='bold 16px sans-serif';ctx.textAlign='center';ctx.fillText(String(total),cx,cy+6);ctx.textAlign='start';
  let ly=20;for(const[k,v]of entries){ctx.fillStyle=colors[k]||'#94a3b8';ctx.fillRect(160,ly-8,10,10);
    ctx.fillStyle='#334155';ctx.font='12px sans-serif';ctx.fillText(k+': '+v+' ('+Math.round(v/total*100)+'%)',175,ly);ly+=20;}
}

async function loadAudit(){
  try{const r=await fetch('/api/v1/audit');const d=await r.json().catch(()=>({}));if(!r.ok)throw new Error(d.error||'Err');
  auditItems=d.items||[];renderAudit();}catch(e){showMsg(e.message,true);}
}
function renderAudit(){
  const total=auditItems.length;const pages=Math.max(1,Math.ceil(total/pageSize));
  if(auditPage>=pages)auditPage=pages-1;if(auditPage<0)auditPage=0;
  const s=auditPage*pageSize,slice=auditItems.slice(s,s+pageSize);
  $('auditBody').innerHTML=slice.map(x=>'<tr><td>'+esc((x.createdAt||'').slice(0,19).replace('T',' '))+'</td><td><b>'+esc(x.action)+'</b></td><td><code>'+esc((x.licenseId||'').slice(0,8))+'</code></td><td>'+esc(x.actor)+'</td><td class="muted">'+esc(x.details)+'</td></tr>').join('');
  $('auditInfo').textContent='Стр. '+(auditPage+1)+'/'+pages+' ('+total+')';
}

async function loadSettings(){
  try{const r=await fetch('/api/v1/settings');const d=await r.json().catch(()=>({}));
  if($('tgToken'))$('tgToken').value=d.telegram_bot_token||'';if($('tgChat'))$('tgChat').value=d.telegram_chat_id||'';
  if($('tgDays'))$('tgDays').value=d.notify_days_before||'7';if($('whUrl'))$('whUrl').value=d.webhook_url||'';}catch(_){}
}
async function saveSettings(obj){
  try{const r=await fetch('/api/v1/settings',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(obj)});
  const d=await r.json().catch(()=>({}));if(!r.ok)throw new Error(d.error||'Err');showMsg('Сохранено',false);}catch(e){showMsg(e.message,true);}
}
async function loadAPIKeys(){
  try{const r=await fetch('/api/v1/api-keys');const d=await r.json().catch(()=>({}));
  const keys=d.items||[];$('apiKeysList').innerHTML=keys.length?keys.map(k=>
    '<div class="row" style="margin-bottom:6px;font-size:12px"><b>'+esc(k.name)+'</b> <code>'+esc(k.key)+'</code> <span class="tag">'+esc(k.role)+'</span> <button type="button" class="btn-danger btn-xs" data-delkey="'+esc(k.id)+'">X</button></div>'
  ).join(''):'<div class="muted">Нет API-ключей</div>';}catch(_){}
}
async function createAPIKey(){
  const name=($('akName')?.value||'').trim(),role=$('akRole')?.value||'readonly';
  if(!name){showMsg('Укажите имя ключа',true);return;}
  try{const r=await fetch('/api/v1/api-keys',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({name,role})});
  const d=await r.json().catch(()=>({}));if(!r.ok)throw new Error(d.error||'Err');showMsg('Ключ создан: '+d.key,false);$('akName').value='';loadAPIKeys();}catch(e){showMsg(e.message,true);}
}
async function deleteAPIKey(id){
  if(!await askConfirm('Удалить API-ключ','Ключ будет удалён и перестанет работать.','danger'))return;
  try{await fetch('/api/v1/api-keys/'+encodeURIComponent(id),{method:'DELETE'});loadAPIKeys();}catch(_){}
}

function loadAll(){loadFin();loadLicenses();loadAudit();loadSettings();loadAPIKeys();}

// Event listeners
document.querySelectorAll('.tab').forEach(tab=>tab.addEventListener('click',()=>{
  document.querySelectorAll('.tab').forEach(t=>t.classList.remove('active'));
  document.querySelectorAll('.tab-content').forEach(t=>t.classList.remove('active'));
  tab.classList.add('active');const tgt=$('tab-'+tab.getAttribute('data-tab'));if(tgt)tgt.classList.add('active');
}));
document.querySelectorAll('.nav-item').forEach(btn=>btn.addEventListener('click',()=>{
  const tabName=btn.getAttribute('data-tab');
  document.querySelectorAll('.nav-item').forEach(b=>b.classList.remove('active'));
  document.querySelectorAll('.tab-content').forEach(t=>t.classList.remove('active'));
  btn.classList.add('active');
  const tgt=$('tab-'+tabName);if(tgt)tgt.classList.add('active');
}));

$('btnLogin')?.addEventListener('click',doLogin);
$('loginPass')?.addEventListener('keydown',e=>{if(e.key==='Enter')doLogin();});
$('btnLogout')?.addEventListener('click',doLogout);
$('btnCreate')?.addEventListener('click',createLicense);
$('btnRefresh')?.addEventListener('click',loadLicenses);
$('btnExportToggle')?.addEventListener('click',e=>{e.stopPropagation();$('exportMenu').classList.toggle('show');});
document.addEventListener('click',()=>$('exportMenu')?.classList.remove('show'));
$('exportMenu')?.addEventListener('click',e=>{const a=e.target.closest('[data-fmt]');if(!a)return;const fmt=a.getAttribute('data-fmt');window.open('/api/v1/licenses/export?format='+fmt,'_blank');$('exportMenu').classList.remove('show');});
$('btnSaveFinance')?.addEventListener('click',saveFin);
$('btnChangePass')?.addEventListener('click',doChangePassword);
$('btnRefreshAudit')?.addEventListener('click',loadAudit);
$('plan')?.addEventListener('change',applyPlanDef);
$('isTrial')?.addEventListener('change',applyPlanDef);
$('searchInput')?.addEventListener('input',()=>{curPage=0;renderLicenses();});
$('filterStatus')?.addEventListener('change',()=>{curPage=0;renderLicenses();});
$('filterPlan')?.addEventListener('change',()=>{curPage=0;renderLicenses();});
$('pgPrev')?.addEventListener('click',()=>{curPage--;renderLicenses();});
$('pgNext')?.addEventListener('click',()=>{curPage++;renderLicenses();});
$('auditPrev')?.addEventListener('click',()=>{auditPage--;renderAudit();});
$('auditNext')?.addEventListener('click',()=>{auditPage++;renderAudit();});
$('btnEdCancel')?.addEventListener('click',()=>$('editModal').classList.remove('show'));
$('btnEdSave')?.addEventListener('click',saveEdit);
$('editModal')?.addEventListener('click',e=>{if(e.target===$('editModal'))$('editModal').classList.remove('show');});
$('confirmYes')?.addEventListener('click',()=>closeConfirm(true));
$('confirmNo')?.addEventListener('click',()=>closeConfirm(false));
$('confirmModal')?.addEventListener('click',e=>{if(e.target===$('confirmModal'))closeConfirm(false);});
$('btnCreateAK')?.addEventListener('click',createAPIKey);
$('apiKeysList')?.addEventListener('click',e=>{const btn=e.target.closest('[data-delkey]');if(btn)deleteAPIKey(btn.getAttribute('data-delkey'));});
$('btnSaveTg')?.addEventListener('click',async()=>{
  const payload={telegram_bot_token:$('tgToken').value.trim(),notify_days_before:$('tgDays').value.trim(),webhook_url:$('whUrl')?.value.trim()||''};
  const chat=($('tgChat')?.value||'').trim();
  if(chat)payload.telegram_chat_id=chat;
  await saveSettings(payload);
  await loadSettings();
});
$('btnTestTg')?.addEventListener('click',async()=>{try{const r=await fetch('/api/v1/test-telegram',{method:'POST'});const d=await r.json().catch(()=>({}));if(!r.ok)throw new Error(d.error||'Err');showMsg('Telegram OK',false);await loadSettings();}catch(e){showMsg(e.message,true);}});
$('btnBroadcastClients')?.addEventListener('click',async()=>{
  const message=($('tgBroadcastMsg')?.value||'').trim();
  if(!message){showMsg('Введите текст сообщения для клиентов',true);return;}
  if(!await askConfirm('Рассылка клиентам','Отправить сообщение всем привязанным клиентам?','warn'))return;
  try{
    const r=await fetch('/api/v1/broadcast-clients',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({message})});
    const d=await r.json().catch(()=>({}));
    if(!r.ok)throw new Error(d.error||'Err');
    showMsg('Отправлено: '+(d.sent||0)+', ошибок: '+(d.failed||0),false);
  }catch(e){showMsg(e.message,true);}
});
$('btnBackup')?.addEventListener('click',()=>{window.open('/api/v1/backup','_blank');});
$('btnRestoreBtn')?.addEventListener('click',()=>$('restoreFile').click());
$('restoreFile')?.addEventListener('change',async e=>{
  const f=e.target.files[0];if(!f)return;if(!await askConfirm('Восстановить БД','Все текущие данные будут перезаписаны из файла бэкапа. Продолжить?','danger'))return;
  try{const body=await f.text();const r=await fetch('/api/v1/restore',{method:'POST',headers:{'Content-Type':'application/json'},body});
  const d=await r.json().catch(()=>({}));if(!r.ok)throw new Error(d.error||'Err');showMsg('БД восстановлена',false);loadAll();}catch(e2){showMsg(e2.message,true);}
  $('restoreFile').value='';
});

$('licensesBody')?.addEventListener('click',evt=>{
  const btn=evt.target.closest('button[data-action]');if(btn){const id=btn.getAttribute('data-id'),act=btn.getAttribute('data-action');if(id&&act)licAction(id,act);return;}
});

applyPlanDef();
(async()=>{const a=await checkAuth();switchView(a);if(a)loadAll();})();
</script>
</body>
</html>` + ""

const clientPageHTML = `<!doctype html>
<html lang="ru">
<head>
<meta charset="UTF-8"/>
<meta name="viewport" content="width=device-width,initial-scale=1.0"/>
<title>NODAX Client License</title>
<style>
*{box-sizing:border-box}body{margin:0;font-family:Inter,Segoe UI,sans-serif;background:linear-gradient(135deg,#2d6a4f 0%,#52b69a 60%,#1a8a8a 100%);min-height:100vh}
.wrap{max-width:920px;margin:28px auto;padding:0 14px}.card{background:#fff;border-radius:14px;padding:18px 20px;box-shadow:0 12px 28px rgba(15,23,42,.18);margin-bottom:14px}
h1{margin:0 0 12px;color:#fff;font-size:24px;text-shadow:0 1px 6px rgba(0,0,0,.35)}h2{margin:0 0 10px;font-size:16px}
.brand{display:flex;align-items:center;gap:12px;margin:0 0 12px}
.brand img{height:56px;width:auto;object-fit:contain}
.row{display:flex;gap:10px;flex-wrap:wrap}.field{display:flex;flex-direction:column;gap:6px;flex:1;min-width:220px}
label{font-size:12px;font-weight:600;color:#475569}input{border:1px solid #dbe1e8;border-radius:8px;padding:9px 10px;font-size:13px}
button{border:none;border-radius:8px;padding:9px 13px;font-weight:600;cursor:pointer}.btn{background:#0f766e;color:#fff}.btn2{background:#e2e8f0;color:#334155}
.kv{display:grid;grid-template-columns:200px 1fr;gap:8px;font-size:13px}.muted{color:#64748b}.status{display:inline-block;padding:3px 9px;border-radius:999px;font-size:11px;font-weight:700}
.s-active{background:#dcfce7;color:#166534}.s-revoked{background:#fee2e2;color:#991b1b}.s-expired{background:#fef3c7;color:#92400e}.s-unknown{background:#e2e8f0;color:#334155}
.msg{font-size:12px;margin-top:8px;color:#0f766e}.msg.err{color:#b91c1c}
.tip{font-size:12px;color:#475569;background:#f8fafc;border:1px solid #e2e8f0;border-radius:8px;padding:10px 12px;margin-top:12px}
.tip .row{margin-top:8px}
</style>
</head>
<body>
<div class="wrap">
<div class="brand"><img src="/assets/logo" alt="NODAX"/></div>
<h1>Кабинет лицензии</h1>
<div id="loginCard" class="card">
  <h2>Вход</h2>
  <div class="row">
    <div class="field"><label>License Key</label><input id="lk" placeholder="NDX-..."/></div>
    <div class="field"><label>Email</label><input id="em" placeholder="you@company.com"/></div>
  </div>
  <div class="row" style="margin-top:10px"><button id="btnLoginClient" class="btn">Войти</button></div>
  <div id="loginMsgClient" class="msg"></div>
</div>
<div id="appCard" class="card" style="display:none">
  <div class="row" style="justify-content:space-between;align-items:center">
    <h2 style="margin:0">Моя лицензия</h2><button id="btnLogoutClient" class="btn2">Выйти</button>
  </div>
  <div id="licView" class="kv" style="margin-top:12px"></div>
  <div class="tip">
    Привязка Telegram: если в лицензии уже указан ваш Telegram (@username), бот привяжется автоматически после любого сообщения.
    Если не привязалось, используйте команду <code id="bindCmd" style="margin-left:6px"></code>
    <div class="row">
      <a id="bindBotBtn" class="btn" target="_blank" rel="noopener">Открыть Telegram и привязать</a>
    </div>
  </div>
  <h2 style="margin-top:16px">Контакты</h2>
  <div class="row">
    <div class="field"><label>Email</label><input id="cEmail"/></div>
    <div class="field"><label>Telegram</label><input id="cTg"/></div>
    <div class="field"><label>Телефон</label><input id="cPhone"/></div>
  </div>
  <div class="row" style="margin-top:10px"><button id="btnSaveClient" class="btn">Сохранить</button></div>
  <div id="appMsgClient" class="msg"></div>
</div>
</div>
<script>
const $=id=>document.getElementById(id);
function msg(el,t,e){if(!el)return;el.textContent=t||'';el.className='msg'+(e?' err':'');}
function clsStatus(s){s=String(s||'unknown').toLowerCase();if(s==='active'||s==='revoked'||s==='expired')return 's-'+s;return 's-unknown';}
function fmt(v){if(!v)return '-';const t=Date.parse(v);if(!Number.isFinite(t))return v;return new Date(t).toLocaleString('ru-RU');}
let botUsername='';
function render(d){
  const v=$('licView');if(!v)return;
  v.innerHTML='<div class="muted">Ключ</div><div><code>'+((d.licenseKey||'-'))+'</code></div>'
   +'<div class="muted">Клиент</div><div>'+(d.customerName||'-')+'</div>'
   +'<div class="muted">Компания</div><div>'+(d.customerCompany||'-')+'</div>'
   +'<div class="muted">План</div><div>'+(d.plan||'-')+'</div>'
   +'<div class="muted">Статус</div><div><span class="status '+clsStatus(d.status)+'">'+(d.status||'unknown')+'</span></div>'
   +'<div class="muted">Истекает</div><div>'+fmt(d.expiresAt)+'</div>'
   +'<div class="muted">Последний хост</div><div>'+(d.lastHostname||'-')+(d.lastIP?(' <span class="muted">('+d.lastIP+')</span>'):'')+'</div>'
   +'<div class="muted">Последняя проверка</div><div>'+fmt(d.lastCheckAt)+'</div>';
  $('cEmail').value=d.customerEmail||'';$('cTg').value=d.customerTelegram||'';$('cPhone').value=d.customerPhone||'';
  const cmd='/link '+(d.licenseKey||'')+' '+(d.customerEmail||'');
  if($('bindCmd'))$('bindCmd').textContent=cmd.trim();
  const bb=$('bindBotBtn');
  if(bb){
    if(botUsername&&d.id){
      bb.href='https://t.me/'+encodeURIComponent(botUsername)+'?start='+encodeURIComponent('bind_'+d.id);
      bb.style.pointerEvents='auto';bb.style.opacity='1';
    }else{
      bb.removeAttribute('href');
      bb.style.pointerEvents='none';bb.style.opacity='.55';
    }
  }
}
function setAuth(a){$('loginCard').style.display=a?'none':'block';$('appCard').style.display=a?'block':'none';}
async function api(u,o){const r=await fetch(u,o);const d=await r.json().catch(()=>({}));if(!r.ok)throw new Error(d.error||('HTTP '+r.status));return d;}
async function check(){try{const d=await api('/api/v1/client/auth/me');if(d.authenticated){botUsername=d.botUsername||'';setAuth(true);render(d.license);}else setAuth(false);}catch(_){setAuth(false);}}
$('btnLoginClient').addEventListener('click',async()=>{try{const d=await api('/api/v1/client/auth/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({licenseKey:$('lk').value.trim(),email:$('em').value.trim()})});botUsername=d.botUsername||'';setAuth(true);render(d.license);msg($('loginMsgClient'),'');}catch(e){msg($('loginMsgClient'),e.message,true);}});
$('btnLogoutClient').addEventListener('click',async()=>{await fetch('/api/v1/client/auth/logout',{method:'POST'}).catch(()=>{});setAuth(false);});
$('btnSaveClient').addEventListener('click',async()=>{try{const d=await api('/api/v1/client/license',{method:'PATCH',headers:{'Content-Type':'application/json'},body:JSON.stringify({customerEmail:$('cEmail').value.trim(),customerTelegram:$('cTg').value.trim(),customerPhone:$('cPhone').value.trim()})});botUsername=d.botUsername||botUsername;render(d.license);msg($('appMsgClient'),'Сохранено');}catch(e){msg($('appMsgClient'),e.message,true);}});
check();
</script>
</body>
</html>`

func (s *Server) handleLicenses(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := s.store.ListLicenses()
		if err != nil {
			httpErr(w, err, 500)
			return
		}
		respondJSON(w, 200, map[string]any{"items": list})
	case http.MethodPost:
		var req struct {
			CustomerName     string `json:"customerName"`
			CustomerEmail    string `json:"customerEmail"`
			CustomerTelegram string `json:"customerTelegram"`
			CustomerPhone    string `json:"customerPhone"`
			CustomerCompany  string `json:"customerCompany"`
			Plan             string `json:"plan"`
			MaxAgents        int    `json:"maxAgents"`
			ValidDays        int    `json:"validDays"`
			ExpiresAt        string `json:"expiresAt"`
			Notes            string `json:"notes"`
		}
		if err := decodeJSON(r, &req); err != nil {
			httpErr(w, fmt.Errorf("invalid body: %w", err), 400)
			return
		}

		if strings.TrimSpace(req.CustomerName) == "" {
			httpErr(w, fmt.Errorf("customerName is required"), 400)
			return
		}
		if strings.TrimSpace(req.Plan) == "" {
			req.Plan = "basic"
		}
		req.Plan = strings.ToLower(strings.TrimSpace(req.Plan))
		req.MaxAgents = defaultMaxAgentsByPlan(req.Plan)

		expires := time.Now().UTC().AddDate(0, 0, 365)
		if req.ValidDays > 0 {
			expires = time.Now().UTC().AddDate(0, 0, req.ValidDays)
		}
		if strings.TrimSpace(req.ExpiresAt) != "" {
			t, err := time.Parse(time.RFC3339, req.ExpiresAt)
			if err != nil {
				httpErr(w, fmt.Errorf("expiresAt must be RFC3339"), 400)
				return
			}
			expires = t.UTC()
		}

		now := time.Now().UTC().Format(time.RFC3339)
		lic := &License{
			ID:               randomHex(16),
			LicenseKey:       generateLicenseKey(),
			CustomerName:     strings.TrimSpace(req.CustomerName),
			CustomerEmail:    strings.TrimSpace(req.CustomerEmail),
			CustomerTelegram: strings.TrimSpace(req.CustomerTelegram),
			CustomerPhone:    strings.TrimSpace(req.CustomerPhone),
			CustomerCompany:  strings.TrimSpace(req.CustomerCompany),
			Plan:             req.Plan,
			MaxAgents:        req.MaxAgents,
			ExpiresAt:        expires.Format(time.RFC3339),
			Status:           "active",
			Notes:            strings.TrimSpace(req.Notes),
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if err := s.store.CreateLicense(lic); err != nil {
			httpErr(w, err, 500)
			return
		}
		_ = s.store.AddAudit(AuditEvent{
			ID:        randomHex(16),
			LicenseID: lic.ID,
			Action:    "create",
			Actor:     "admin",
			Details:   fmt.Sprintf("plan=%s maxAgents=%d", lic.Plan, lic.MaxAgents),
			CreatedAt: now,
		})
		respondJSON(w, 201, lic)
	default:
		http.Error(w, "Method not allowed", 405)
	}
}

func (s *Server) handleLicenseExtend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		httpErr(w, fmt.Errorf("license id required"), 400)
		return
	}
	lic, err := s.store.GetLicenseByID(id)
	if err != nil {
		if errors.Is(err, errLicenseNotFound) {
			httpErr(w, err, 404)
			return
		}
		httpErr(w, err, 500)
		return
	}

	var req struct {
		Days      int    `json:"days"`
		ExpiresAt string `json:"expiresAt"`
	}
	if err := decodeJSON(r, &req); err != nil {
		httpErr(w, fmt.Errorf("invalid body: %w", err), 400)
		return
	}

	base, err := time.Parse(time.RFC3339, lic.ExpiresAt)
	if err != nil || base.Before(time.Now().UTC()) {
		base = time.Now().UTC()
	}
	if strings.TrimSpace(req.ExpiresAt) != "" {
		t, err := time.Parse(time.RFC3339, req.ExpiresAt)
		if err != nil {
			httpErr(w, fmt.Errorf("expiresAt must be RFC3339"), 400)
			return
		}
		base = t.UTC()
	} else {
		if req.Days <= 0 {
			req.Days = 30
		}
		base = base.AddDate(0, 0, req.Days)
	}

	lic.ExpiresAt = base.Format(time.RFC3339)
	lic.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := s.store.UpdateLicense(lic); err != nil {
		httpErr(w, err, 500)
		return
	}
	_ = s.store.AddAudit(AuditEvent{
		ID:        randomHex(16),
		LicenseID: lic.ID,
		Action:    "extend",
		Actor:     "admin",
		Details:   lic.ExpiresAt,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	respondJSON(w, 200, lic)
}

func (s *Server) handleLicenseRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		httpErr(w, fmt.Errorf("license id required"), 400)
		return
	}
	lic, err := s.store.GetLicenseByID(id)
	if err != nil {
		if errors.Is(err, errLicenseNotFound) {
			httpErr(w, err, 404)
			return
		}
		httpErr(w, err, 500)
		return
	}
	lic.Status = "active"
	lic.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := s.store.UpdateLicense(lic); err != nil {
		httpErr(w, err, 500)
		return
	}
	_ = s.store.AddAudit(AuditEvent{
		ID:        randomHex(16),
		LicenseID: lic.ID,
		Action:    "restore",
		Actor:     "admin",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	respondJSON(w, 200, lic)
}

func (s *Server) handleLicenseRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		httpErr(w, fmt.Errorf("license id required"), 400)
		return
	}
	lic, err := s.store.GetLicenseByID(id)
	if err != nil {
		if errors.Is(err, errLicenseNotFound) {
			httpErr(w, err, 404)
			return
		}
		httpErr(w, err, 500)
		return
	}
	lic.Status = "revoked"
	lic.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := s.store.UpdateLicense(lic); err != nil {
		httpErr(w, err, 500)
		return
	}
	_ = s.store.AddAudit(AuditEvent{
		ID:        randomHex(16),
		LicenseID: lic.ID,
		Action:    "revoke",
		Actor:     "admin",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	respondJSON(w, 200, lic)
}

func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}
	var req validateRequest
	if err := decodeJSON(r, &req); err != nil {
		httpErr(w, fmt.Errorf("invalid body: %w", err), 400)
		return
	}
	if strings.TrimSpace(req.LicenseKey) == "" {
		httpErr(w, fmt.Errorf("licenseKey is required"), 400)
		return
	}

	payload := signedValidatePayload{
		Status:     "invalid",
		Valid:      false,
		GraceDays:  s.graceDays,
		ServerTime: time.Now().UTC().Format(time.RFC3339),
		InstanceID: strings.TrimSpace(req.InstanceID),
	}

	lic, err := s.store.GetLicenseByKey(strings.TrimSpace(req.LicenseKey))
	if err != nil {
		payload.Reason = "license_not_found"
		respondSignedPayload(w, payload, s.signKey)
		return
	}

	payload.LicenseID = lic.ID
	payload.Plan = lic.Plan
	payload.MaxAgents = lic.MaxAgents
	payload.ExpiresAt = lic.ExpiresAt
	payload.LicenseKey = lic.LicenseKey
	payload.CustomerName = lic.CustomerName

	now := time.Now().UTC()
	expiresAt, err := time.Parse(time.RFC3339, lic.ExpiresAt)
	if err != nil {
		payload.Reason = "invalid_expiration"
		payload.Status = "invalid"
		respondSignedPayload(w, payload, s.signKey)
		return
	}

	switch strings.ToLower(strings.TrimSpace(lic.Status)) {
	case "active":
		// continue
	case "suspended":
		payload.Status = "suspended"
		payload.Reason = "suspended"
		respondSignedPayload(w, payload, s.signKey)
		return
	default:
		payload.Status = "revoked"
		payload.Reason = "revoked"
		respondSignedPayload(w, payload, s.signKey)
		return
	}

	if now.After(expiresAt) {
		payload.Status = "expired"
		payload.Reason = "expired"
		respondSignedPayload(w, payload, s.signKey)
		return
	}
	if lic.MaxAgents > 0 && req.AgentCount > lic.MaxAgents {
		payload.Status = "over_limit"
		payload.Reason = "agent_limit"
		respondSignedPayload(w, payload, s.signKey)
		return
	}

	lic.LastInstanceID = strings.TrimSpace(req.InstanceID)
	lic.LastHostname = strings.TrimSpace(req.Hostname)
	lic.LastIP = requestClientIP(r)
	lic.LastCheckAt = now.Format(time.RFC3339)
	_ = s.store.UpdateLicense(lic)

	payload.Status = "active"
	payload.Valid = true
	respondSignedPayload(w, payload, s.signKey)
}

func (s *Server) getSessionID(r *http.Request) string {
	c, err := r.Cookie("session")
	if err != nil {
		return ""
	}
	return c.Value
}

func (s *Server) withAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if sid := s.getSessionID(r); sid != "" && s.store.ValidateSession(sid) {
			next(w, r)
			return
		}
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		want := "Bearer " + s.adminToken
		if auth == want {
			next(w, r)
			return
		}
		if strings.HasPrefix(auth, "Bearer ") {
			apiKey := strings.TrimPrefix(auth, "Bearer ")
			if role, ok := s.store.ValidateAPIKey(apiKey); ok && (role == "full" || role == "readonly") {
				if role == "readonly" && r.Method != http.MethodGet {
					httpErr(w, fmt.Errorf("readonly API key"), 403)
					return
				}
				next(w, r)
				return
			}
		}
		httpErr(w, fmt.Errorf("unauthorized"), 401)
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		httpErr(w, fmt.Errorf("invalid body"), 400)
		return
	}
	if strings.TrimSpace(req.Username) != "admin" || !s.store.CheckPassword(req.Password) {
		httpErr(w, fmt.Errorf("неверный логин или пароль"), 401)
		return
	}
	sess, err := s.store.CreateAdminSession()
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    sess.ID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400,
	})
	respondJSON(w, 200, map[string]any{"ok": true, "username": "admin"})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}
	if sid := s.getSessionID(r); sid != "" {
		_ = s.store.DeleteSession(sid)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	respondJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", 405)
		return
	}
	if sid := s.getSessionID(r); sid != "" && s.store.ValidateSession(sid) {
		respondJSON(w, 200, map[string]any{"authenticated": true, "username": "admin"})
		return
	}
	respondJSON(w, 200, map[string]any{"authenticated": false})
}

func (s *Server) getClientSessionID(r *http.Request) string {
	c, err := r.Cookie("client_session")
	if err != nil {
		return ""
	}
	return c.Value
}

type clientLicenseView struct {
	ID               string `json:"id"`
	LicenseKey       string `json:"licenseKey"`
	CustomerName     string `json:"customerName"`
	CustomerCompany  string `json:"customerCompany,omitempty"`
	CustomerEmail    string `json:"customerEmail,omitempty"`
	CustomerTelegram string `json:"customerTelegram,omitempty"`
	CustomerPhone    string `json:"customerPhone,omitempty"`
	Plan             string `json:"plan"`
	MaxAgents        int    `json:"maxAgents"`
	Status           string `json:"status"`
	ExpiresAt        string `json:"expiresAt"`
	LastHostname     string `json:"lastHostname,omitempty"`
	LastIP           string `json:"lastIP,omitempty"`
	ClientChatBound  bool   `json:"clientChatBound"`
	LastCheckAt      string `json:"lastCheckAt,omitempty"`
	IsTrial          bool   `json:"isTrial,omitempty"`
}

func toClientLicenseView(lic *License) clientLicenseView {
	if lic == nil {
		return clientLicenseView{}
	}
	return clientLicenseView{
		ID:               lic.ID,
		LicenseKey:       lic.LicenseKey,
		CustomerName:     lic.CustomerName,
		CustomerCompany:  lic.CustomerCompany,
		CustomerEmail:    lic.CustomerEmail,
		CustomerTelegram: lic.CustomerTelegram,
		CustomerPhone:    lic.CustomerPhone,
		Plan:             lic.Plan,
		MaxAgents:        lic.MaxAgents,
		Status:           lic.Status,
		ExpiresAt:        lic.ExpiresAt,
		LastHostname:     lic.LastHostname,
		LastIP:           lic.LastIP,
		ClientChatBound:  strings.TrimSpace(lic.ClientChatID) != "",
		LastCheckAt:      lic.LastCheckAt,
		IsTrial:          lic.IsTrial,
	}
}

func (s *Server) clientLicenseFromRequest(r *http.Request) (*License, error) {
	sid := s.getClientSessionID(r)
	licenseID, ok := s.store.ValidateClientSession(sid)
	if !ok {
		return nil, errUnauthorized
	}
	lic, err := s.store.GetLicenseByID(licenseID)
	if err != nil {
		return nil, errUnauthorized
	}
	return lic, nil
}

func (s *Server) handleClientLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}
	var req struct {
		LicenseKey string `json:"licenseKey"`
		Email      string `json:"email"`
	}
	if err := decodeJSON(r, &req); err != nil {
		httpErr(w, fmt.Errorf("invalid body"), 400)
		return
	}
	key := strings.TrimSpace(req.LicenseKey)
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if key == "" || email == "" {
		httpErr(w, fmt.Errorf("license key and email are required"), 400)
		return
	}
	lic, err := s.store.GetLicenseByKey(key)
	if err != nil {
		httpErr(w, fmt.Errorf("license not found"), 401)
		return
	}
	if strings.ToLower(strings.TrimSpace(lic.CustomerEmail)) != email {
		httpErr(w, fmt.Errorf("email does not match license"), 401)
		return
	}
	sess, err := s.store.CreateClientSession(lic.ID)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "client_session",
		Value:    sess.ID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400,
	})
	respondJSON(w, 200, map[string]any{
		"ok":          true,
		"license":     toClientLicenseView(lic),
		"botUsername": strings.TrimSpace(s.store.GetSetting("telegram_bot_username")),
	})
}

func (s *Server) handleClientLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}
	if sid := s.getClientSessionID(r); sid != "" {
		_ = s.store.DeleteSession(sid)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "client_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	respondJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleClientAuthMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", 405)
		return
	}
	lic, err := s.clientLicenseFromRequest(r)
	if err != nil {
		respondJSON(w, 200, map[string]any{"authenticated": false})
		return
	}
	respondJSON(w, 200, map[string]any{
		"authenticated": true,
		"license":       toClientLicenseView(lic),
		"botUsername":   strings.TrimSpace(s.store.GetSetting("telegram_bot_username")),
	})
}

func (s *Server) handleClientLicense(w http.ResponseWriter, r *http.Request) {
	lic, err := s.clientLicenseFromRequest(r)
	if err != nil {
		httpErr(w, fmt.Errorf("unauthorized"), 401)
		return
	}
	switch r.Method {
	case http.MethodGet:
		respondJSON(w, 200, map[string]any{
			"license":     toClientLicenseView(lic),
			"botUsername": strings.TrimSpace(s.store.GetSetting("telegram_bot_username")),
		})
	case http.MethodPatch, http.MethodPut:
		var req struct {
			CustomerEmail    *string `json:"customerEmail"`
			CustomerTelegram *string `json:"customerTelegram"`
			CustomerPhone    *string `json:"customerPhone"`
		}
		if err := decodeJSON(r, &req); err != nil {
			httpErr(w, fmt.Errorf("invalid body"), 400)
			return
		}
		if req.CustomerEmail != nil {
			lic.CustomerEmail = strings.TrimSpace(*req.CustomerEmail)
		}
		if req.CustomerTelegram != nil {
			lic.CustomerTelegram = strings.TrimSpace(*req.CustomerTelegram)
		}
		if req.CustomerPhone != nil {
			lic.CustomerPhone = strings.TrimSpace(*req.CustomerPhone)
		}
		lic.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := s.store.UpdateLicense(lic); err != nil {
			httpErr(w, err, 500)
			return
		}
		_ = s.store.AddAudit(AuditEvent{
			ID:        randomHex(16),
			LicenseID: lic.ID,
			Action:    "client_contact_update",
			Actor:     "client",
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		})
		respondJSON(w, 200, map[string]any{
			"license":     toClientLicenseView(lic),
			"botUsername": strings.TrimSpace(s.store.GetSetting("telegram_bot_username")),
		})
	default:
		http.Error(w, "Method not allowed", 405)
	}
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}
	var req struct {
		OldPassword string `json:"oldPassword"`
		NewPassword string `json:"newPassword"`
	}
	if err := decodeJSON(r, &req); err != nil {
		httpErr(w, fmt.Errorf("invalid body"), 400)
		return
	}
	if !s.store.CheckPassword(req.OldPassword) {
		httpErr(w, fmt.Errorf("неверный текущий пароль"), 403)
		return
	}
	if len(strings.TrimSpace(req.NewPassword)) < 4 {
		httpErr(w, fmt.Errorf("пароль минимум 4 символа"), 400)
		return
	}
	if err := s.store.ChangePassword(req.NewPassword); err != nil {
		httpErr(w, err, 500)
		return
	}
	respondJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", 405)
		return
	}
	items, err := s.store.ListAudit()
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	respondJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) handleLicenseByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		httpErr(w, fmt.Errorf("license id required"), 400)
		return
	}

	if r.Method == http.MethodDelete {
		if err := s.store.DeleteLicense(id); err != nil {
			if errors.Is(err, errLicenseNotFound) {
				httpErr(w, err, 404)
				return
			}
			httpErr(w, err, 500)
			return
		}
		_ = s.store.AddAudit(AuditEvent{
			ID:        randomHex(16),
			LicenseID: id,
			Action:    "delete",
			Actor:     "admin",
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		})
		s.fireWebhook("license.delete", map[string]string{"id": id})
		respondJSON(w, 200, map[string]any{"ok": true})
		return
	}

	if r.Method != http.MethodPatch && r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", 405)
		return
	}
	lic, err := s.store.GetLicenseByID(id)
	if err != nil {
		if errors.Is(err, errLicenseNotFound) {
			httpErr(w, err, 404)
			return
		}
		httpErr(w, err, 500)
		return
	}
	var req struct {
		CustomerName     *string `json:"customerName"`
		CustomerEmail    *string `json:"customerEmail"`
		CustomerTelegram *string `json:"customerTelegram"`
		CustomerPhone    *string `json:"customerPhone"`
		CustomerCompany  *string `json:"customerCompany"`
		Plan             *string `json:"plan"`
		MaxAgents        *int    `json:"maxAgents"`
		Notes            *string `json:"notes"`
	}
	if err := decodeJSON(r, &req); err != nil {
		httpErr(w, fmt.Errorf("invalid body: %w", err), 400)
		return
	}
	changed := []string{}
	if req.CustomerName != nil {
		lic.CustomerName = strings.TrimSpace(*req.CustomerName)
		changed = append(changed, "customerName")
	}
	if req.CustomerEmail != nil {
		lic.CustomerEmail = strings.TrimSpace(*req.CustomerEmail)
		changed = append(changed, "customerEmail")
	}
	if req.CustomerTelegram != nil {
		lic.CustomerTelegram = strings.TrimSpace(*req.CustomerTelegram)
		changed = append(changed, "customerTelegram")
	}
	if req.CustomerPhone != nil {
		lic.CustomerPhone = strings.TrimSpace(*req.CustomerPhone)
		changed = append(changed, "customerPhone")
	}
	if req.CustomerCompany != nil {
		lic.CustomerCompany = strings.TrimSpace(*req.CustomerCompany)
		changed = append(changed, "customerCompany")
	}
	if req.Plan != nil {
		lic.Plan = strings.ToLower(strings.TrimSpace(*req.Plan))
		changed = append(changed, "plan")
	}
	if req.MaxAgents != nil {
		lic.MaxAgents = *req.MaxAgents
		changed = append(changed, "maxAgents")
	}
	if req.Notes != nil {
		lic.Notes = strings.TrimSpace(*req.Notes)
		changed = append(changed, "notes")
	}
	lic.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := s.store.UpdateLicense(lic); err != nil {
		httpErr(w, err, 500)
		return
	}
	_ = s.store.AddAudit(AuditEvent{
		ID:        randomHex(16),
		LicenseID: lic.ID,
		Action:    "edit",
		Actor:     "admin",
		Details:   strings.Join(changed, ","),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	s.fireWebhook("license.edit", lic)
	respondJSON(w, 200, lic)
}

func (s *Server) handleLicensesExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", 405)
		return
	}
	list, err := s.store.ListLicenses()
	if err != nil {
		httpErr(w, err, 500)
		return
	}

	headers := []string{"ID", "Ключ", "Клиент", "Компания", "Email", "Telegram", "Телефон", "Тариф", "Лимит", "Истекает", "Статус", "Комментарий", "Хост", "IP", "Последний чек", "Создана"}
	rows := make([][]string, 0, len(list))
	for _, l := range list {
		rows = append(rows, []string{l.ID, l.LicenseKey, l.CustomerName, l.CustomerCompany, l.CustomerEmail, l.CustomerTelegram, l.CustomerPhone, l.Plan, strconv.Itoa(l.MaxAgents), l.ExpiresAt, l.Status, l.Notes, l.LastHostname, l.LastIP, l.LastCheckAt, l.CreatedAt})
	}

	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	switch format {
	case "xlsx":
		s.exportXLSX(w, headers, rows)
	case "html":
		s.exportHTML(w, headers, rows)
	default:
		s.exportCSV(w, headers, rows)
	}
}

func (s *Server) exportCSV(w http.ResponseWriter, headers []string, rows [][]string) {
	var buf bytes.Buffer
	buf.Write([]byte{0xEF, 0xBB, 0xBF})
	cw := csv.NewWriter(&buf)
	_ = cw.Write(headers)
	for _, r := range rows {
		_ = cw.Write(r)
	}
	cw.Flush()
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=licenses.csv")
	_, _ = w.Write(buf.Bytes())
}

func xmlEsc(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

func (s *Server) exportXLSX(w http.ResponseWriter, headers []string, rows [][]string) {
	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	buf.WriteString(`<?mso-application progid="Excel.Sheet"?>` + "\n")
	buf.WriteString(`<Workbook xmlns="urn:schemas-microsoft-com:office:spreadsheet" xmlns:ss="urn:schemas-microsoft-com:office:spreadsheet">` + "\n")
	buf.WriteString(`<Styles>`)
	buf.WriteString(`<Style ss:ID="hdr"><Font ss:Bold="1" ss:Size="11" ss:Color="#FFFFFF"/><Interior ss:Color="#0f766e" ss:Pattern="Solid"/><Alignment ss:Horizontal="Center" ss:Vertical="Center"/><Borders><Border ss:Position="Bottom" ss:LineStyle="Continuous" ss:Weight="1" ss:Color="#0d6660"/></Borders></Style>`)
	buf.WriteString(`<Style ss:ID="cell"><Font ss:Size="11"/><Alignment ss:Vertical="Center" ss:WrapText="1"/><Borders><Border ss:Position="Bottom" ss:LineStyle="Continuous" ss:Weight="1" ss:Color="#e2e8f0"/></Borders></Style>`)
	buf.WriteString(`<Style ss:ID="alt"><Font ss:Size="11"/><Interior ss:Color="#f8fafc" ss:Pattern="Solid"/><Alignment ss:Vertical="Center" ss:WrapText="1"/><Borders><Border ss:Position="Bottom" ss:LineStyle="Continuous" ss:Weight="1" ss:Color="#e2e8f0"/></Borders></Style>`)
	buf.WriteString(`</Styles>`)
	buf.WriteString(`<Worksheet ss:Name="Лицензии"><Table>` + "\n")
	for i := range headers {
		w := 80
		switch i {
		case 0:
			w = 50
		case 1:
			w = 200
		case 2, 3:
			w = 150
		case 4, 5, 6:
			w = 130
		case 9, 13, 14:
			w = 160
		}
		buf.WriteString(fmt.Sprintf(`<Column ss:Width="%d"/>`, w))
	}
	buf.WriteString("\n<Row ss:Height=\"28\">")
	for _, h := range headers {
		buf.WriteString(`<Cell ss:StyleID="hdr"><Data ss:Type="String">` + xmlEsc(h) + `</Data></Cell>`)
	}
	buf.WriteString("</Row>\n")
	for ri, row := range rows {
		style := "cell"
		if ri%2 == 1 {
			style = "alt"
		}
		buf.WriteString(`<Row ss:Height="22">`)
		for _, cell := range row {
			buf.WriteString(`<Cell ss:StyleID="` + style + `"><Data ss:Type="String">` + xmlEsc(cell) + `</Data></Cell>`)
		}
		buf.WriteString("</Row>\n")
	}
	buf.WriteString("</Table></Worksheet></Workbook>")
	w.Header().Set("Content-Type", "application/vnd.ms-excel; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=licenses.xls")
	_, _ = w.Write(buf.Bytes())
}

func (s *Server) exportHTML(w http.ResponseWriter, headers []string, rows [][]string) {
	var buf bytes.Buffer
	buf.WriteString(`<!doctype html><html lang="ru"><head><meta charset="UTF-8"/><title>NODAX Лицензии</title>
<style>
*{box-sizing:border-box}
body{margin:0;padding:24px;font-family:'Segoe UI',Arial,sans-serif;background:#f1f5f9;color:#0f172a}
h1{font-size:22px;margin:0 0 16px;color:#0f766e}
.info{font-size:12px;color:#64748b;margin-bottom:16px}
table{width:100%;border-collapse:collapse;background:#fff;border-radius:12px;overflow:hidden;box-shadow:0 4px 24px rgba(15,23,42,.08)}
th{background:linear-gradient(135deg,#0f766e,#0891b2);color:#fff;font-size:11px;text-transform:uppercase;letter-spacing:.5px;padding:12px 10px;text-align:left}
td{padding:10px;font-size:12px;border-bottom:1px solid #f1f5f9}
tr:nth-child(even) td{background:#f8fafc}
tr:hover td{background:#f0f7ff}
.s-active{color:#166534;font-weight:700}.s-revoked{color:#991b1b;font-weight:700}.s-expired{color:#92400e;font-weight:700}
code{background:#f1f5f9;border:1px solid #e2e8f0;border-radius:4px;padding:1px 5px;font-size:11px;font-family:monospace}
.footer{margin-top:16px;font-size:11px;color:#94a3b8;text-align:center}
@media print{body{padding:8px}table{box-shadow:none}h1{font-size:16px}}
</style></head><body>
<h1>NODAX — Лицензии</h1>
<div class="info">Экспорт: ` + time.Now().Format("02.01.2006 15:04") + ` | Всего: ` + strconv.Itoa(len(rows)) + `</div>
<table><thead><tr>`)
	for _, h := range headers {
		buf.WriteString("<th>" + xmlEsc(h) + "</th>")
	}
	buf.WriteString("</tr></thead><tbody>\n")
	for _, row := range rows {
		buf.WriteString("<tr>")
		for ci, cell := range row {
			if ci == 1 {
				buf.WriteString("<td><code>" + xmlEsc(cell) + "</code></td>")
			} else if ci == 10 {
				cls := "s-" + strings.ToLower(cell)
				buf.WriteString(`<td class="` + cls + `">` + xmlEsc(cell) + `</td>`)
			} else {
				buf.WriteString("<td>" + xmlEsc(cell) + "</td>")
			}
		}
		buf.WriteString("</tr>\n")
	}
	buf.WriteString(`</tbody></table>
<div class="footer">NODAX License Server</div>
</body></html>`)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=licenses.html")
	_, _ = w.Write(buf.Bytes())
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		respondJSON(w, 200, s.store.GetAllSettings())
	case http.MethodPost, http.MethodPut:
		var req map[string]string
		if err := decodeJSON(r, &req); err != nil {
			httpErr(w, fmt.Errorf("invalid body"), 400)
			return
		}
		for k, v := range req {
			if k == "telegram_chat_id" && strings.TrimSpace(v) == "" && strings.TrimSpace(s.store.GetSetting("telegram_chat_id")) != "" {
				// Do not wipe auto-captured chat ID with stale empty UI value.
				continue
			}
			_ = s.store.SetSetting(k, v)
		}
		respondJSON(w, 200, map[string]any{"ok": true})
	default:
		http.Error(w, "Method not allowed", 405)
	}
}

func (s *Server) handleAPIKeys(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		keys, err := s.store.ListAPIKeys()
		if err != nil {
			httpErr(w, err, 500)
			return
		}
		respondJSON(w, 200, map[string]any{"items": keys})
	case http.MethodPost:
		var req struct {
			Name string `json:"name"`
			Role string `json:"role"`
		}
		if err := decodeJSON(r, &req); err != nil {
			httpErr(w, fmt.Errorf("invalid body"), 400)
			return
		}
		if strings.TrimSpace(req.Name) == "" {
			httpErr(w, fmt.Errorf("name is required"), 400)
			return
		}
		if req.Role != "full" && req.Role != "readonly" {
			req.Role = "readonly"
		}
		ak := &APIKey{
			ID:        randomHex(16),
			Name:      strings.TrimSpace(req.Name),
			Key:       "ndxk_" + randomHex(20),
			Role:      req.Role,
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		}
		if err := s.store.CreateAPIKey(ak); err != nil {
			httpErr(w, err, 500)
			return
		}
		_ = s.store.AddAudit(AuditEvent{
			ID:        randomHex(16),
			Action:    "api_key_create",
			Actor:     "admin",
			Details:   ak.Name + " role=" + ak.Role,
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		})
		respondJSON(w, 201, ak)
	default:
		http.Error(w, "Method not allowed", 405)
	}
}

func (s *Server) handleAPIKeyDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", 405)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		httpErr(w, fmt.Errorf("id required"), 400)
		return
	}
	if err := s.store.DeleteAPIKey(id); err != nil {
		httpErr(w, err, 500)
		return
	}
	_ = s.store.AddAudit(AuditEvent{
		ID:        randomHex(16),
		Action:    "api_key_delete",
		Actor:     "admin",
		Details:   id,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	respondJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", 405)
		return
	}
	data, err := s.store.BackupDB()
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=license-server-backup.json")
	_, _ = w.Write(data)
}

func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 50<<20))
	if err != nil {
		httpErr(w, fmt.Errorf("read body: %w", err), 400)
		return
	}
	if err := s.store.RestoreDB(body); err != nil {
		httpErr(w, err, 400)
		return
	}
	_ = s.store.AddAudit(AuditEvent{
		ID:        randomHex(16),
		Action:    "restore",
		Actor:     "admin",
		Details:   fmt.Sprintf("%d bytes", len(body)),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	respondJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleTestTelegram(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}
	token := s.store.GetSetting("telegram_bot_token")
	chatID := s.store.GetSetting("telegram_chat_id")
	if token == "" || chatID == "" {
		httpErr(w, fmt.Errorf("telegram_bot_token и telegram_chat_id не настроены"), 400)
		return
	}
	err := sendTelegram(token, chatID, "✅ Тестовое сообщение от NODAX License Server")
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	respondJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleBroadcastClients(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}
	token := strings.TrimSpace(s.store.GetSetting("telegram_bot_token"))
	if token == "" {
		httpErr(w, fmt.Errorf("telegram_bot_token не настроен"), 400)
		return
	}
	var req struct {
		Message string `json:"message"`
	}
	if err := decodeJSON(r, &req); err != nil {
		httpErr(w, fmt.Errorf("invalid body"), 400)
		return
	}
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		httpErr(w, fmt.Errorf("message is required"), 400)
		return
	}

	list, err := s.store.ListLicenses()
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	targets := map[string]bool{}
	for _, lic := range list {
		chat := strings.TrimSpace(lic.ClientChatID)
		if chat != "" {
			targets[chat] = true
		}
	}
	if len(targets) == 0 {
		httpErr(w, fmt.Errorf("нет привязанных клиентов (client_chat_id пустой)"), 400)
		return
	}

	sent := 0
	failed := 0
	for chat := range targets {
		if err := sendTelegram(token, chat, msg); err != nil {
			failed++
			continue
		}
		sent++
	}

	_ = s.store.AddAudit(AuditEvent{
		ID:        randomHex(16),
		Action:    "broadcast_clients",
		Actor:     "admin",
		Details:   fmt.Sprintf("sent=%d failed=%d", sent, failed),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	respondJSON(w, 200, map[string]any{"ok": true, "sent": sent, "failed": failed, "targets": len(targets)})
}

func (s *Server) handleTestWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}
	url := s.store.GetSetting("webhook_url")
	if url == "" {
		httpErr(w, fmt.Errorf("webhook_url не настроен"), 400)
		return
	}
	err := sendWebhook(url, "test", map[string]any{"message": "test from NODAX License Server"})
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	respondJSON(w, 200, map[string]any{"ok": true})
}

type telegramUpdateResponse struct {
	OK     bool `json:"ok"`
	Result []struct {
		UpdateID int64 `json:"update_id"`
		Message  *struct {
			Chat struct {
				ID int64 `json:"id"`
			} `json:"chat"`
			From *struct {
				Username string `json:"username"`
			} `json:"from"`
			Text string `json:"text"`
		} `json:"message"`
	} `json:"result"`
}

func (s *Server) telegramBindingLoop() {
	for {
		time.Sleep(2 * time.Second)
		token := strings.TrimSpace(s.store.GetSetting("telegram_bot_token"))
		if token == "" {
			time.Sleep(20 * time.Second)
			continue
		}
		if strings.TrimSpace(s.store.GetSetting("telegram_bot_username")) == "" {
			s.ensureTelegramBotUsername(token)
		}

		offset := int64(0)
		if raw := strings.TrimSpace(s.store.GetSetting("telegram_update_offset")); raw != "" {
			if n, err := strconv.ParseInt(raw, 10, 64); err == nil && n > 0 {
				offset = n
			}
		}

		u := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?timeout=25&offset=%d", token, offset)
		resp, err := (&http.Client{Timeout: 35 * time.Second}).Get(u)
		if err != nil {
			time.Sleep(5 * time.Second)
			continue
		}
		var upd telegramUpdateResponse
		_ = json.NewDecoder(resp.Body).Decode(&upd)
		resp.Body.Close()
		if !upd.OK {
			time.Sleep(5 * time.Second)
			continue
		}

		maxID := offset
		for _, it := range upd.Result {
			if it.UpdateID >= maxID {
				maxID = it.UpdateID + 1
			}
			if it.Message == nil {
				continue
			}
			username := ""
			if it.Message.From != nil {
				username = strings.TrimSpace(it.Message.From.Username)
			}
			s.processTelegramCommand(token, it.Message.Chat.ID, username, strings.TrimSpace(it.Message.Text))
		}
		if maxID > offset {
			_ = s.store.SetSetting("telegram_update_offset", strconv.FormatInt(maxID, 10))
		}
	}
}

func (s *Server) ensureTelegramBotUsername(botToken string) {
	u := fmt.Sprintf("https://api.telegram.org/bot%s/getMe", botToken)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Get(u)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	var out struct {
		OK     bool `json:"ok"`
		Result struct {
			Username string `json:"username"`
		} `json:"result"`
	}
	if json.NewDecoder(resp.Body).Decode(&out) != nil || !out.OK {
		return
	}
	if strings.TrimSpace(out.Result.Username) != "" {
		_ = s.store.SetSetting("telegram_bot_username", strings.TrimSpace(out.Result.Username))
	}
}

func normalizeTelegramHandle(v string) string {
	x := strings.TrimSpace(strings.ToLower(v))
	x = strings.TrimPrefix(x, "@")
	return x
}

func (s *Server) tryAutoBindClientChat(botToken string, chatID int64, username string) bool {
	u := normalizeTelegramHandle(username)
	if u == "" {
		return false
	}
	list, err := s.store.ListLicenses()
	if err != nil {
		return false
	}
	chat := strconv.FormatInt(chatID, 10)
	matched := 0
	updated := 0
	for i := range list {
		lu := normalizeTelegramHandle(list[i].CustomerTelegram)
		if lu == "" || lu != u {
			continue
		}
		matched++
		if strings.TrimSpace(list[i].ClientChatID) == chat {
			continue
		}
		lic := list[i]
		lic.ClientChatID = chat
		lic.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := s.store.UpdateLicense(&lic); err != nil {
			continue
		}
		updated++
		_ = s.store.AddAudit(AuditEvent{
			ID:        randomHex(16),
			LicenseID: lic.ID,
			Action:    "telegram_client_autobind",
			Actor:     "bot",
			Details:   "username:@" + u,
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		})
	}
	if matched == 0 {
		return false
	}
	if updated == 0 {
		return true
	}
	_ = sendTelegram(botToken, chat, fmt.Sprintf("✅ Telegram автоматически привязан к %d лицензиям.", updated))
	return true
}

func (s *Server) processTelegramCommand(botToken string, chatID int64, username, text string) {
	if text == "" {
		return
	}
	chat := strconv.FormatInt(chatID, 10)
	lower := strings.ToLower(strings.TrimSpace(text))
	if strings.HasPrefix(lower, "/start ") {
		payload := strings.TrimSpace(text[len("/start "):])
		if strings.HasPrefix(payload, "bind_") {
			licenseID := strings.TrimSpace(strings.TrimPrefix(payload, "bind_"))
			if lic, err := s.store.GetLicenseByID(licenseID); err == nil {
				chat := strconv.FormatInt(chatID, 10)
				lic.ClientChatID = chat
				lic.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
				if err := s.store.UpdateLicense(lic); err == nil {
					_ = s.store.AddAudit(AuditEvent{
						ID:        randomHex(16),
						LicenseID: lic.ID,
						Action:    "telegram_client_bind_start",
						Actor:     "bot",
						Details:   chat,
						CreatedAt: time.Now().UTC().Format(time.RFC3339),
					})
					_ = sendTelegram(botToken, chat, "✅ Лицензия успешно привязана к вашему Telegram.")
					return
				}
			}
		}
	}

	// Auto-capture admin chat ID on first contact with the bot.
	if strings.TrimSpace(s.store.GetSetting("telegram_chat_id")) == "" {
		_ = s.store.SetSetting("telegram_chat_id", chat)
		_ = sendTelegram(botToken, chat, "✅ Chat ID автоматически привязан для админ-уведомлений.")
	}

	if strings.HasPrefix(lower, "/admin ") {
		arg := strings.TrimSpace(text[len("/admin "):])
		if arg == s.adminToken {
			_ = s.store.SetSetting("telegram_chat_id", chat)
			_ = sendTelegram(botToken, chat, "✅ Админ-уведомления подключены. Будут приходить события по всем лицензиям.")
		} else {
			_ = sendTelegram(botToken, chat, "❌ Неверный admin token.")
		}
		return
	}

	// Auto-bind client by Telegram username if uniquely matched in licenses.
	_ = s.tryAutoBindClientChat(botToken, chatID, username)

	if strings.HasPrefix(lower, "/link ") {
		payload := strings.TrimSpace(text[len("/link "):])
		parts := strings.Fields(payload)
		if len(parts) < 2 {
			_ = sendTelegram(botToken, chat, "Формат: /link <LICENSE_KEY> <EMAIL>")
			return
		}
		key := strings.TrimSpace(parts[0])
		email := strings.ToLower(strings.TrimSpace(parts[1]))
		lic, err := s.store.GetLicenseByKey(key)
		if err != nil {
			_ = sendTelegram(botToken, chat, "❌ Лицензия не найдена.")
			return
		}
		if strings.ToLower(strings.TrimSpace(lic.CustomerEmail)) != email {
			_ = sendTelegram(botToken, chat, "❌ Email не совпадает с лицензией.")
			return
		}
		lic.ClientChatID = chat
		lic.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := s.store.UpdateLicense(lic); err != nil {
			_ = sendTelegram(botToken, chat, "❌ Не удалось сохранить привязку.")
			return
		}
		_ = s.store.AddAudit(AuditEvent{
			ID:        randomHex(16),
			LicenseID: lic.ID,
			Action:    "telegram_client_bind",
			Actor:     "bot",
			Details:   chat,
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		})
		_ = sendTelegram(botToken, chat, "✅ Telegram привязан к лицензии "+lic.LicenseKey)
		return
	}

	if lower == "/start" || lower == "/help" {
		help := "Команды:\n/admin <ADMIN_TOKEN> — привязать админ-уведомления\n/link <LICENSE_KEY> <EMAIL> — привязать лицензию клиента"
		_ = sendTelegram(botToken, chat, help)
	}
}

func sendTelegram(botToken, chatID, text string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
	body, _ := json.Marshal(map[string]string{"chat_id": chatID, "text": text, "parse_mode": "HTML"})
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		rb, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("telegram: %d %s", resp.StatusCode, string(rb))
	}
	return nil
}

func sendWebhook(url, event string, data any) error {
	body, _ := json.Marshal(map[string]any{"event": event, "data": data, "time": time.Now().UTC().Format(time.RFC3339)})
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		rb, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("webhook: %d %s", resp.StatusCode, string(rb))
	}
	return nil
}

func (s *Server) fireWebhook(event string, data any) {
	url := s.store.GetSetting("webhook_url")
	if url == "" {
		return
	}
	go func() { _ = sendWebhook(url, event, data) }()
}

func (s *Server) expirationNotifier() {
	for {
		time.Sleep(6 * time.Hour)
		token := s.store.GetSetting("telegram_bot_token")
		adminChatID := s.store.GetSetting("telegram_chat_id")
		daysStr := s.store.GetSetting("notify_days_before")
		if token == "" {
			continue
		}
		daysBefore := 7
		if n, err := strconv.Atoi(daysStr); err == nil && n > 0 {
			daysBefore = n
		}
		list, err := s.store.ListLicenses()
		if err != nil {
			continue
		}
		now := time.Now().UTC()
		for _, lic := range list {
			if strings.ToLower(lic.Status) != "active" {
				continue
			}
			exp, err := time.Parse(time.RFC3339, lic.ExpiresAt)
			if err != nil {
				continue
			}
			daysLeft := int(exp.Sub(now).Hours() / 24)
			if daysLeft >= 0 && daysLeft <= daysBefore {
				adminMsg := fmt.Sprintf("⚠️ Лицензия <b>%s</b> (%s) истекает через <b>%d дн.</b>\nКлюч: <code>%s</code>", lic.CustomerName, lic.Plan, daysLeft, lic.LicenseKey)
				if strings.TrimSpace(adminChatID) != "" {
					_ = sendTelegram(token, adminChatID, adminMsg)
				}
				clientChat := strings.TrimSpace(lic.ClientChatID)
				if clientChat != "" {
					clientMsg := fmt.Sprintf("⚠️ Ваша лицензия (%s) истекает через <b>%d дн.</b>\nКлюч: <code>%s</code>", lic.Plan, daysLeft, lic.LicenseKey)
					_ = sendTelegram(token, clientChat, clientMsg)
				}
				s.fireWebhook("license.expiring", map[string]any{"license": lic, "daysLeft": daysLeft})
			}
		}
	}
}

func respondSignedPayload(w http.ResponseWriter, payload signedValidatePayload, priv ed25519.PrivateKey) {
	body, _ := json.Marshal(payload)
	sig := ed25519.Sign(priv, body)
	respondJSON(w, 200, map[string]any{
		"payload":   payload,
		"signature": base64.StdEncoding.EncodeToString(sig),
		"algorithm": "ed25519",
	})
}

func respondJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func httpErr(w http.ResponseWriter, err error, code int) {
	respondJSON(w, code, map[string]string{"error": err.Error()})
}

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("unexpected trailing data")
	}
	return nil
}

func randomHex(bytesLen int) string {
	b := make([]byte, bytesLen)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func requestClientIP(r *http.Request) string {
	xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			if ip != "" {
				return ip
			}
		}
	}
	if xrip := strings.TrimSpace(r.Header.Get("X-Real-IP")); xrip != "" {
		return xrip
	}
	remote := strings.TrimSpace(r.RemoteAddr)
	if remote == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(remote)
	if err == nil {
		return host
	}
	return remote
}

func generateLicenseKey() string {
	parts := make([]string, 4)
	for i := 0; i < len(parts); i++ {
		chunk := strings.ToUpper(randomHex(3))
		parts[i] = chunk
	}
	return "NDX-" + strings.Join(parts, "-")
}

func defaultMaxAgentsByPlan(plan string) int {
	p := strings.ToLower(strings.TrimSpace(plan))
	if p == "pro" {
		return 30
	}
	if p == "enterprise" {
		return 0 // unlimited
	}
	return 10
}

func loadOrCreateSigningKey() (ed25519.PrivateKey, ed25519.PublicKey, error) {
	path := strings.TrimSpace(os.Getenv("LICENSE_SIGN_KEY_PATH"))
	if path == "" {
		path = resolveDataFilePath("license-sign.key")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, nil, err
	}

	if raw, err := os.ReadFile(path); err == nil {
		key := ed25519.PrivateKey(raw)
		if len(key) != ed25519.PrivateKeySize {
			return nil, nil, fmt.Errorf("invalid private key size")
		}
		pub := key.Public().(ed25519.PublicKey)
		return key, pub, nil
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	if err := os.WriteFile(path, priv, 0600); err != nil {
		return nil, nil, err
	}
	return priv, pub, nil
}

func resolveDataFilePath(fileName string) string {
	if dir := strings.TrimSpace(os.Getenv("LICENSE_DATA_DIR")); dir != "" {
		return filepath.Join(dir, fileName)
	}
	ex, err := os.Executable()
	if err != nil {
		return fileName
	}
	return filepath.Join(filepath.Dir(ex), fileName)
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
