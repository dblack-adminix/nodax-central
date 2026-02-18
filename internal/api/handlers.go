package api

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"nodax-central/internal/models"
	"nodax-central/internal/netutil"
	"nodax-central/internal/poller"
	"nodax-central/internal/store"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Handler holds dependencies for HTTP handlers
type Handler struct {
	store      *store.Store
	poller     *poller.Poller
	proxy      *http.Client
	dataDir    string
	instanceID string
	licenseMu  sync.Mutex
}

// handleConfigBackup exports full central config as JSON file
func (h *Handler) handleConfigBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", 405)
		return
	}
	user, err := h.currentUserFromRequest(r)
	if err != nil {
		httpErr(w, fmt.Errorf("unauthorized"), 401)
		return
	}
	if normalizeRole(user.Role) != "admin" {
		httpErr(w, fmt.Errorf("forbidden"), 403)
		return
	}

	cfg, err := h.store.GetConfig()
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	agents, err := h.store.GetAllAgents()
	if err != nil {
		httpErr(w, err, 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=nodax-central-config-%s.json", time.Now().Format("20060102-150405")))
	_ = json.NewEncoder(w).Encode(map[string]any{
		"version":    2,
		"config":     cfg,
		"agents":     agents,
		"exportedAt": time.Now().UTC().Format(time.RFC3339),
	})
}

// handleConfigRestore imports full central config from JSON body
func (h *Handler) handleConfigRestore(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}
	user, err := h.currentUserFromRequest(r)
	if err != nil {
		httpErr(w, fmt.Errorf("unauthorized"), 401)
		return
	}
	if normalizeRole(user.Role) != "admin" {
		httpErr(w, fmt.Errorf("forbidden"), 403)
		return
	}

	existing, _ := h.store.GetConfig()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		httpErr(w, fmt.Errorf("invalid body: %w", err), 400)
		return
	}
	var payload struct {
		Config models.CentralConfig `json:"config"`
		Agents *[]models.Agent      `json:"agents"`
	}
	var cfg models.CentralConfig
	if err := json.Unmarshal(body, &payload); err == nil && (payload.Agents != nil || payload.Config.Port != "" || payload.Config.Theme != "" || payload.Config.Language != "" || payload.Config.PollIntervalSec != 0 || payload.Config.RetentionDays != 0 || payload.Config.CaddyDomain != "") {
		cfg = payload.Config
	} else {
		// Backward compatibility: old backups were plain CentralConfig JSON
		if err := json.Unmarshal(body, &cfg); err != nil {
			httpErr(w, fmt.Errorf("invalid body: %w", err), 400)
			return
		}
		payload.Agents = nil
	}

	if cfg.PollIntervalSec < 5 {
		cfg.PollIntervalSec = 5
	}
	if strings.TrimSpace(cfg.Port) == "" {
		cfg.Port = "8080"
	}
	if cfg.RetentionDays < 1 {
		cfg.RetentionDays = 30
	}
	if strings.TrimSpace(cfg.Theme) == "" {
		cfg.Theme = "light"
	}
	if strings.TrimSpace(cfg.Language) == "" {
		cfg.Language = "ru"
	}
	if strings.TrimSpace(cfg.JWTSecret) == "" && existing != nil {
		cfg.JWTSecret = existing.JWTSecret
	}

	if payload.Agents != nil {
		replaceMode := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("mode")), "replace")
		if replaceMode {
			curAgents, err := h.store.GetAllAgents()
			if err != nil {
				httpErr(w, err, 500)
				return
			}
			for _, a := range curAgents {
				_ = h.store.DeleteAgent(a.ID)
			}
		}
		for i := range *payload.Agents {
			a := (*payload.Agents)[i]
			if strings.TrimSpace(a.ID) == "" {
				a.ID = fmt.Sprintf("agent_%d", time.Now().UnixNano()+int64(i))
			}
			a.URL = netutil.NormalizeAgentBaseURL(a.URL)
			if a.URL == "" {
				continue
			}
			_ = h.store.SaveAgent(&a)
		}
	}

	if err := h.store.SaveConfig(&cfg); err != nil {
		httpErr(w, err, 500)
		return
	}

	cfg.JWTSecret = ""
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"config": cfg,
	})
}

func (h *Handler) handleCaddyRecheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}

	user, err := h.currentUserFromRequest(r)
	if err != nil {
		httpErr(w, fmt.Errorf("unauthorized"), 401)
		return
	}
	if normalizeRole(user.Role) != "admin" {
		httpErr(w, fmt.Errorf("forbidden"), 403)
		return
	}

	cfg, err := h.store.GetConfig()
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	domain := strings.TrimSpace(cfg.CaddyDomain)
	if domain == "" {
		httpErr(w, fmt.Errorf("caddyDomain is empty in central config"), 400)
		return
	}

	port := strings.TrimSpace(cfg.Port)
	if port == "" {
		port = "8080"
	}

	if err := applyCaddyConfig(domain, port); err != nil {
		httpErr(w, err, 500)
		return
	}

	checkURL := fmt.Sprintf("https://%s", domain)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Get(checkURL)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]any{
			"status":  "reloaded",
			"domain":  domain,
			"message": fmt.Sprintf("Caddy reloaded. Certificate check pending: %v", err),
		})
		return
	}
	defer resp.Body.Close()

	issuer := ""
	notAfter := ""
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		cert := resp.TLS.PeerCertificates[0]
		issuer = cert.Issuer.String()
		notAfter = cert.NotAfter.Format(time.RFC3339)
	}

	json.NewEncoder(w).Encode(map[string]any{
		"status":   "ok",
		"domain":   domain,
		"httpCode": resp.StatusCode,
		"issuer":   issuer,
		"notAfter": notAfter,
		"message":  "Caddy reloaded and HTTPS checked",
	})
}

func resolveCaddyConfigPath() string {
	caddyFile := os.Getenv("CADDY_CONFIG_PATH")
	if caddyFile != "" {
		return caddyFile
	}
	if runtime.GOOS == "windows" {
		return `C:\ProgramData\Caddy\Caddyfile`
	}
	return "/etc/caddy/Caddyfile"
}

func applyCaddyConfig(domain, port string) error {
	domain = strings.TrimSpace(domain)
	port = strings.TrimSpace(port)
	if domain == "" {
		return fmt.Errorf("caddyDomain is empty in central config")
	}
	if port == "" {
		port = "8080"
	}

	caddyFile := resolveCaddyConfigPath()
	content := strings.Join([]string{
		fmt.Sprintf("%s {", domain),
		"    encode gzip zstd",
		"",
		fmt.Sprintf("    reverse_proxy 127.0.0.1:%s", port),
		"}",
		"",
	}, "\n")
	if err := os.WriteFile(caddyFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write Caddyfile: %w", err)
	}

	if runtime.GOOS == "windows" {
		_ = exec.Command("sc.exe", "stop", "Caddy").Run() // stop may fail if already stopped
		if err := exec.Command("sc.exe", "start", "Caddy").Run(); err != nil {
			return fmt.Errorf("failed to reload caddy: %w", err)
		}
		return nil
	}
	if err := exec.Command("systemctl", "restart", "caddy").Run(); err != nil {
		return fmt.Errorf("failed to reload caddy: %w", err)
	}
	return nil
}

// NewHandler creates a new Handler
func NewHandler(s *store.Store, p *poller.Poller) *Handler {
	ex, _ := os.Executable()
	dataDir := filepath.Join(filepath.Dir(ex), "backgrounds")
	instanceID, _ := os.Hostname()
	if strings.TrimSpace(instanceID) == "" {
		instanceID = "nodax-central"
	}
	os.MkdirAll(dataDir, 0755)
	return &Handler{
		store:      s,
		poller:     p,
		dataDir:    dataDir,
		instanceID: instanceID,
		proxy: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
	}
}

// RegisterRoutes registers all API routes
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/agents", h.handleAgents)
	mux.HandleFunc("/api/agents/", h.handleAgent)
	mux.HandleFunc("/api/overview", h.handleOverview)
	mux.HandleFunc("/api/config", h.handleConfig)
	mux.HandleFunc("/api/config/backup", h.handleConfigBackup)
	mux.HandleFunc("/api/config/restore", h.handleConfigRestore)
	mux.HandleFunc("/api/caddy/recheck", h.handleCaddyRecheck)
	mux.HandleFunc("/api/license/status", h.handleLicenseStatus)
	mux.HandleFunc("/api/license/recheck", h.handleLicenseRecheck)
	mux.HandleFunc("/api/license-server/", h.handleLicenseServerProxy)
	mux.HandleFunc("/api/stats", h.handleStats)
	mux.HandleFunc("/api/grafana/logs", h.handleGrafanaLogs)
	mux.HandleFunc("/api/backgrounds", h.handleBackgrounds)
	mux.HandleFunc("/api/backgrounds/", h.handleBackgroundFile)
	mux.HandleFunc("/api/agents/{id}/data", h.handleAgentData)
	mux.HandleFunc("/api/agents/{id}/history", h.handleAgentHistory)
	mux.HandleFunc("/api/agents/{id}/proxy/", h.handleProxy)
	mux.HandleFunc("/metrics", h.handlePrometheusMetrics)

	// Loki-compatible API for Grafana
	h.registerLokiRoutes(mux)
}

func (h *Handler) filterAgentsByAccess(r *http.Request, agents []models.Agent) []models.Agent {
	user, err := h.currentUserFromRequest(r)
	if err != nil || normalizeRole(user.Role) == "admin" {
		return agents
	}
	out := make([]models.Agent, 0, len(agents))
	for _, a := range agents {
		if canViewAgent(user, a.ID) {
			out = append(out, a)
		}
	}
	return out
}

// --- Agent CRUD ---

func (h *Handler) handleAgents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		user, err := h.currentUserFromRequest(r)
		if err != nil {
			httpErr(w, fmt.Errorf("unauthorized"), 401)
			return
		}
		agents, err := h.store.GetAllAgents()
		if err != nil {
			httpErr(w, err, 500)
			return
		}
		if normalizeRole(user.Role) != "admin" {
			agents = h.filterAgentsByAccess(r, agents)
		}
		if agents == nil {
			agents = []models.Agent{}
		}
		json.NewEncoder(w).Encode(agents)

	case http.MethodPost:
		user, err := h.currentUserFromRequest(r)
		if err != nil {
			httpErr(w, fmt.Errorf("unauthorized"), 401)
			return
		}
		if normalizeRole(user.Role) != "admin" {
			httpErr(w, fmt.Errorf("forbidden"), 403)
			return
		}
		var agent models.Agent
		if err := json.NewDecoder(r.Body).Decode(&agent); err != nil {
			httpErr(w, fmt.Errorf("invalid body: %w", err), 400)
			return
		}
		if agent.URL == "" {
			httpErr(w, fmt.Errorf("url is required"), 400)
			return
		}
		agent.URL = netutil.NormalizeAgentBaseURL(agent.URL)
		if agent.ID == "" {
			agent.ID = fmt.Sprintf("agent_%d", time.Now().UnixNano())
		}
		agent.Status = "pending"
		agent.CreatedAt = time.Now()

		// Auto-fetch hostname from agent /api/v1/status
		if agent.Name == "" {
			req, _ := http.NewRequest("GET", agent.URL+"/api/v1/status", nil)
			if agent.APIKey != "" {
				req.Header.Set("X-API-Key", agent.APIKey)
			}
			if resp, err := h.proxy.Do(req); err == nil {
				defer resp.Body.Close()
				var status struct {
					Host string `json:"host"`
				}
				if json.NewDecoder(resp.Body).Decode(&status) == nil && status.Host != "" {
					agent.Name = status.Host
				}
			}
			if agent.Name == "" {
				agent.Name = agent.URL
			}
		}

		if err := h.store.SaveAgent(&agent); err != nil {
			httpErr(w, err, 500)
			return
		}

		// Immediately poll the new agent
		go h.poller.PollAgent(agent)

		json.NewEncoder(w).Encode(agent)

	default:
		http.Error(w, "Method not allowed", 405)
	}
}

func (h *Handler) handleAgent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract ID from path: /api/agents/{id}
	id := strings.TrimPrefix(r.URL.Path, "/api/agents/")
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, "Invalid agent ID", 400)
		return
	}

	switch r.Method {
	case http.MethodGet:
		user, err := h.currentUserFromRequest(r)
		if err != nil {
			httpErr(w, fmt.Errorf("unauthorized"), 401)
			return
		}
		if !canViewAgent(user, id) {
			httpErr(w, fmt.Errorf("forbidden"), 403)
			return
		}
		agent, err := h.store.GetAgent(id)
		if err != nil {
			httpErr(w, err, 404)
			return
		}
		json.NewEncoder(w).Encode(agent)

	case http.MethodPut:
		user, err := h.currentUserFromRequest(r)
		if err != nil {
			httpErr(w, fmt.Errorf("unauthorized"), 401)
			return
		}
		if normalizeRole(user.Role) != "admin" {
			httpErr(w, fmt.Errorf("forbidden"), 403)
			return
		}
		var update models.Agent
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			httpErr(w, fmt.Errorf("invalid body: %w", err), 400)
			return
		}
		existing, err := h.store.GetAgent(id)
		if err != nil {
			httpErr(w, err, 404)
			return
		}
		if update.Name != "" {
			existing.Name = update.Name
		}
		if update.URL != "" {
			existing.URL = netutil.NormalizeAgentBaseURL(update.URL)
		}
		if update.APIKey != "" {
			existing.APIKey = update.APIKey
		}
		if err := h.store.SaveAgent(existing); err != nil {
			httpErr(w, err, 500)
			return
		}
		json.NewEncoder(w).Encode(existing)

	case http.MethodDelete:
		user, err := h.currentUserFromRequest(r)
		if err != nil {
			httpErr(w, fmt.Errorf("unauthorized"), 401)
			return
		}
		if normalizeRole(user.Role) != "admin" {
			httpErr(w, fmt.Errorf("forbidden"), 403)
			return
		}
		if err := h.store.DeleteAgent(id); err != nil {
			httpErr(w, err, 500)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})

	default:
		http.Error(w, "Method not allowed", 405)
	}
}

// --- Data endpoints ---

func (h *Handler) handleAgentData(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	id := r.PathValue("id")
	if id == "" {
		httpErr(w, fmt.Errorf("agent id required"), 400)
		return
	}
	user, err := h.currentUserFromRequest(r)
	if err != nil {
		httpErr(w, fmt.Errorf("unauthorized"), 401)
		return
	}
	if !canViewAgent(user, id) {
		httpErr(w, fmt.Errorf("forbidden"), 403)
		return
	}

	if _, err := h.store.GetAgent(id); err != nil {
		httpErr(w, err, 404)
		return
	}

	data := h.poller.GetAgentData(id)
	if data == nil {
		data = &models.AgentData{
			AgentID:   id,
			FetchedAt: time.Now(),
			Error:     "данные еще не собраны центральным poller",
		}
	}

	json.NewEncoder(w).Encode(data)
}

func (h *Handler) handleAgentHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	id := r.PathValue("id")
	if id == "" {
		httpErr(w, fmt.Errorf("agent id required"), 400)
		return
	}
	user, err := h.currentUserFromRequest(r)
	if err != nil {
		httpErr(w, fmt.Errorf("unauthorized"), 401)
		return
	}
	if !canViewAgent(user, id) {
		httpErr(w, fmt.Errorf("forbidden"), 403)
		return
	}

	agent, err := h.store.GetAgent(id)
	if err != nil {
		httpErr(w, err, 404)
		return
	}
	pts := h.poller.GetHistory(id)
	json.NewEncoder(w).Encode(models.HostHistory{
		AgentID: id,
		Name:    agent.Name,
		Points:  pts,
	})
}

func (h *Handler) handleOverview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	agents, _ := h.store.GetAllAgents()
	agents = h.filterAgentsByAccess(r, agents)
	allData := h.poller.GetAllData()

	overview := models.DashboardOverview{
		TotalAgents: len(agents),
	}

	for _, agent := range agents {
		if agent.Status == "online" {
			overview.OnlineAgents++
		}
		if data, ok := allData[agent.ID]; ok && data.HostInfo != nil {
			overview.TotalVMs += data.HostInfo.VMCount
			overview.RunningVMs += data.HostInfo.VMRunning
			overview.TotalCPU += data.HostInfo.CPUUsage
			overview.TotalRAMBytes += data.HostInfo.TotalRAM
			overview.UsedRAMBytes += data.HostInfo.UsedRAM
		}
	}

	if overview.OnlineAgents > 0 {
		overview.TotalCPU /= float64(overview.OnlineAgents)
	}

	json.NewEncoder(w).Encode(overview)
}

// handleProxy proxies requests to the agent's API
// Path: /api/agents/{id}/proxy/api/v1/...
func (h *Handler) handleProxy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httpErr(w, fmt.Errorf("agent id required"), 400)
		return
	}

	user, err := h.currentUserFromRequest(r)
	if err != nil {
		httpErr(w, fmt.Errorf("unauthorized"), 401)
		return
	}
	isReadMethod := r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions
	if isReadMethod {
		if !canViewAgent(user, id) {
			httpErr(w, fmt.Errorf("forbidden"), 403)
			return
		}
	} else {
		if !canControlAgent(user, id) {
			httpErr(w, fmt.Errorf("forbidden"), 403)
			return
		}
	}

	agent, err := h.store.GetAgent(id)
	if err != nil {
		httpErr(w, err, 404)
		return
	}

	// Extract the target path after /api/agents/{id}/proxy
	proxyPath := strings.TrimPrefix(r.URL.Path, fmt.Sprintf("/api/agents/%s/proxy", id))
	baseURL := netutil.NormalizeAgentBaseURL(agent.URL)
	targetURL := baseURL + proxyPath
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	// Create proxy request
	var bodyReader io.Reader
	if r.Body != nil {
		bodyBytes, _ := io.ReadAll(r.Body)
		bodyReader = bytes.NewReader(bodyBytes)
	}

	proxyReq, err := http.NewRequest(r.Method, targetURL, bodyReader)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	proxyReq.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	if agent.APIKey != "" {
		proxyReq.Header.Set("X-API-Key", agent.APIKey)
	}

	resp, err := h.proxy.Do(proxyReq)
	if err != nil {
		httpErr(w, fmt.Errorf("agent unreachable: %w", err), 502)
		return
	}
	defer resp.Body.Close()

	// Forward response
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// handleConfig GET/PUT central server config
func (h *Handler) handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	user, err := h.currentUserFromRequest(r)
	if err != nil {
		httpErr(w, fmt.Errorf("unauthorized"), 401)
		return
	}
	switch r.Method {
	case http.MethodGet:
		cfg, _ := h.store.GetConfig()
		cfg.JWTSecret = "" // never expose to frontend
		json.NewEncoder(w).Encode(cfg)
	case http.MethodPut:
		if normalizeRole(user.Role) != "admin" {
			httpErr(w, fmt.Errorf("forbidden"), 403)
			return
		}
		existing, _ := h.store.GetConfig()
		prevLicenseKey := strings.TrimSpace(existing.LicenseKey)
		prevLicenseServer := strings.TrimSpace(existing.LicenseServer)
		prevPort := strings.TrimSpace(existing.Port)
		prevCaddyDomain := strings.TrimSpace(existing.CaddyDomain)
		var cfg models.CentralConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			httpErr(w, fmt.Errorf("invalid body: %w", err), 400)
			return
		}
		if cfg.PollIntervalSec < 5 {
			cfg.PollIntervalSec = 5
		}
		cfg.JWTSecret = existing.JWTSecret // preserve secret
		if strings.TrimSpace(cfg.LicenseKey) == "" {
			cfg.LicenseKey = existing.LicenseKey
		}
		if strings.TrimSpace(cfg.LicenseServer) == "" {
			cfg.LicenseServer = existing.LicenseServer
		}
		if strings.TrimSpace(cfg.LicensePubKey) == "" {
			cfg.LicensePubKey = existing.LicensePubKey
		}
		cfg.LicenseStatus = existing.LicenseStatus
		cfg.LicenseReason = existing.LicenseReason
		cfg.LicenseExpires = existing.LicenseExpires
		cfg.LicenseChecked = existing.LicenseChecked
		cfg.LicenseGraceTo = existing.LicenseGraceTo
		cfg.LicenseLastErr = existing.LicenseLastErr

		newPort := strings.TrimSpace(cfg.Port)
		if newPort == "" {
			newPort = "8080"
			cfg.Port = newPort
		}

		newCaddyDomain := strings.TrimSpace(cfg.CaddyDomain)
		caddyNeedsSync := newCaddyDomain != "" && (newPort != prevPort || newCaddyDomain != prevCaddyDomain)
		if caddyNeedsSync {
			if err := applyCaddyConfig(newCaddyDomain, newPort); err != nil {
				httpErr(w, fmt.Errorf("failed to sync caddy with updated server port/domain: %w", err), 500)
				return
			}
		}

		if err := h.store.SaveConfig(&cfg); err != nil {
			httpErr(w, err, 500)
			return
		}
		if strings.TrimSpace(cfg.LicenseKey) != prevLicenseKey || strings.TrimSpace(cfg.LicenseServer) != prevLicenseServer {
			go h.refreshLicenseStatus()
		}
		cfg.JWTSecret = ""
		json.NewEncoder(w).Encode(cfg)
	default:
		http.Error(w, "Method not allowed", 405)
	}
}

// handleStats returns aggregated statistics from all hosts
func (h *Handler) handleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	agents, _ := h.store.GetAllAgents()
	agents = h.filterAgentsByAccess(r, agents)
	allData := h.poller.GetAllData()

	stats := models.AggregatedStats{}
	stats.TotalHosts = len(agents)

	for _, agent := range agents {
		hs := models.HostStats{
			AgentID: agent.ID,
			Name:    agent.Name,
			Status:  agent.Status,
		}
		if agent.Status == "online" {
			stats.OnlineHosts++
		}
		if data, ok := allData[agent.ID]; ok && data.HostInfo != nil {
			hi := data.HostInfo
			hs.CPU = hi.CPUUsage
			hs.RAMPct = hi.RAMUsePct
			hs.RAMUsedGB = float64(hi.UsedRAM) / (1024 * 1024 * 1024)
			hs.RAMTotalGB = float64(hi.TotalRAM) / (1024 * 1024 * 1024)
			hs.VMTotal = hi.VMCount
			hs.VMRunning = hi.VMRunning
			hs.Disks = hi.Disks
			hs.Uptime = hi.Uptime
			hs.OS = hi.OSName

			stats.TotalVMs += hi.VMCount
			stats.RunningVMs += hi.VMRunning
			stats.TotalRAMGB += hs.RAMTotalGB
			stats.UsedRAMGB += hs.RAMUsedGB
			stats.AvgCPU += hi.CPUUsage
			stats.AvgRAM += hi.RAMUsePct

			for _, d := range hi.Disks {
				stats.TotalDiskGB += d.TotalGB
				stats.UsedDiskGB += d.TotalGB - d.FreeGB
			}
		}
		stats.Hosts = append(stats.Hosts, hs)
	}

	if stats.OnlineHosts > 0 {
		stats.AvgCPU /= float64(stats.OnlineHosts)
		stats.AvgRAM /= float64(stats.OnlineHosts)
	}

	json.NewEncoder(w).Encode(stats)
}

// handleBackgrounds GET=list, POST=upload
func (h *Handler) handleBackgrounds(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		entries, _ := os.ReadDir(h.dataDir)
		var names []string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp" || ext == ".gif" || ext == ".bmp" {
				names = append(names, e.Name())
			}
		}
		if names == nil {
			names = []string{}
		}
		json.NewEncoder(w).Encode(names)

	case http.MethodPost:
		r.ParseMultipartForm(10 << 20) // 10 MB max
		file, header, err := r.FormFile("file")
		if err != nil {
			httpErr(w, fmt.Errorf("no file: %w", err), 400)
			return
		}
		defer file.Close()

		ext := strings.ToLower(filepath.Ext(header.Filename))
		if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp" && ext != ".gif" && ext != ".bmp" {
			httpErr(w, fmt.Errorf("unsupported format: %s", ext), 400)
			return
		}

		name := fmt.Sprintf("bg_%d%s", time.Now().UnixMilli(), ext)
		dst, err := os.Create(filepath.Join(h.dataDir, name))
		if err != nil {
			httpErr(w, err, 500)
			return
		}
		defer dst.Close()
		io.Copy(dst, file)

		json.NewEncoder(w).Encode(map[string]string{"name": name})

	default:
		http.Error(w, "Method not allowed", 405)
	}
}

// handleBackgroundFile GET=serve file, DELETE=remove file
func (h *Handler) handleBackgroundFile(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/backgrounds/")
	if name == "" || strings.Contains(name, "..") || strings.Contains(name, "/") {
		http.Error(w, "invalid name", 400)
		return
	}
	fpath := filepath.Join(h.dataDir, name)

	switch r.Method {
	case http.MethodGet:
		http.ServeFile(w, r, fpath)
	case http.MethodDelete:
		if err := os.Remove(fpath); err != nil {
			httpErr(w, err, 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	default:
		http.Error(w, "Method not allowed", 405)
	}
}

func httpErr(w http.ResponseWriter, err error, code int) {
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// handleLicenseServerProxy proxies requests to the license server
// Path: /api/license-server/... -> license server /api/v1/...
func (h *Handler) handleLicenseServerProxy(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Only admin can access license server management
	user, err := h.currentUserFromRequest(r)
	if err != nil {
		httpErr(w, fmt.Errorf("unauthorized"), 401)
		return
	}
	if normalizeRole(user.Role) != "admin" {
		httpErr(w, fmt.Errorf("forbidden"), 403)
		return
	}

	cfg, err := h.store.GetConfig()
	if err != nil {
		httpErr(w, fmt.Errorf("config error: %w", err), 500)
		return
	}

	server := strings.TrimSpace(cfg.LicenseServer)
	if server == "" {
		server = strings.TrimSpace(os.Getenv("NODAX_LICENSE_SERVER"))
	}
	if server == "" {
		httpErr(w, fmt.Errorf("license server not configured"), 400)
		return
	}

	// Extract path after /api/license-server/
	proxyPath := strings.TrimPrefix(r.URL.Path, "/api/license-server")
	targetURL := strings.TrimRight(server, "/") + "/api/v1" + proxyPath
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	// Create proxy request
	var bodyReader io.Reader
	if r.Body != nil {
		bodyBytes, _ := io.ReadAll(r.Body)
		bodyReader = bytes.NewReader(bodyBytes)
	}

	proxyReq, err := http.NewRequest(r.Method, targetURL, bodyReader)
	if err != nil {
		httpErr(w, fmt.Errorf("request build failed: %w", err), 500)
		return
	}
	proxyReq.Header.Set("Content-Type", r.Header.Get("Content-Type"))

	// Add admin token from config or env
	adminToken := strings.TrimSpace(os.Getenv("NODAX_LICENSE_ADMIN_TOKEN"))
	if adminToken != "" {
		proxyReq.Header.Set("Authorization", "Bearer "+adminToken)
	}

	resp, err := h.proxy.Do(proxyReq)
	if err != nil {
		httpErr(w, fmt.Errorf("license server unreachable: %w", err), 502)
		return
	}
	defer resp.Body.Close()

	// Forward response
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

type grafanaLogEntry struct {
	AgentID   string `json:"agentId"`
	AgentName string `json:"agentName"`
	Timestamp string `json:"timestamp"`
	UnixMs    int64  `json:"unixMs"`
	Type      string `json:"type"`
	TargetVM  string `json:"targetVm"`
	Status    string `json:"status"`
	Message   string `json:"message"`
}

func (h *Handler) handleGrafanaLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	agentFilter := r.URL.Query().Get("agentId")
	typeFilter := strings.TrimSpace(r.URL.Query().Get("type"))
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	user, userErr := h.currentUserFromRequest(r)
	role := normalizeRole(r.Header.Get("X-User-Role"))
	if userErr == nil {
		role = normalizeRole(user.Role)
	}
	if role == "" {
		role = "user"
	}
	if role != "admin" && strings.TrimSpace(agentFilter) == "" {
		httpErr(w, fmt.Errorf("agentId is required"), http.StatusBadRequest)
		return
	}

	if agentFilter != "" {
		if userErr == nil && !canViewAgent(user, agentFilter) {
			httpErr(w, fmt.Errorf("forbidden"), http.StatusForbidden)
			return
		}
	}

	var fromTime time.Time
	var toTime time.Time
	if v := strings.TrimSpace(r.URL.Query().Get("from")); v != "" {
		if ts, ok := parseFlexibleTime(v); ok {
			fromTime = ts
		}
	}
	if v := strings.TrimSpace(r.URL.Query().Get("to")); v != "" {
		if ts, ok := parseFlexibleTime(v); ok {
			toTime = ts
		}
	}

	limit := 200
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			if parsed < 1 {
				parsed = 1
			}
			if parsed > 5000 {
				parsed = 5000
			}
			limit = parsed
		}
	}

	// Read from local store (centrally collected logs)
	logs, err := h.store.QueryLogs(agentFilter, typeFilter, statusFilter, fromTime, toTime, limit)
	if err != nil {
		httpErr(w, err, http.StatusInternalServerError)
		return
	}

	items := make([]grafanaLogEntry, 0, len(logs))
	for _, l := range logs {
		items = append(items, grafanaLogEntry{
			AgentID:   l.AgentID,
			AgentName: l.AgentName,
			Timestamp: l.Timestamp.Format(time.RFC3339),
			UnixMs:    l.Timestamp.UnixMilli(),
			Type:      l.Type,
			TargetVM:  l.TargetVM,
			Status:    l.Status,
			Message:   l.Message,
		})
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"items": items,
		"count": len(items),
	})
}

func (h *Handler) handlePrometheusMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	agents, _ := h.store.GetAllAgents()
	allData := h.poller.GetAllData()

	var b strings.Builder
	b.WriteString("# HELP nodax_central_agents_total Total registered agents\n")
	b.WriteString("# TYPE nodax_central_agents_total gauge\n")
	b.WriteString(fmt.Sprintf("nodax_central_agents_total %d\n", len(agents)))

	b.WriteString("# HELP nodax_central_agents_online Online agents\n")
	b.WriteString("# TYPE nodax_central_agents_online gauge\n")
	online := 0
	for _, a := range agents {
		if a.Status == "online" {
			online++
		}
	}
	b.WriteString(fmt.Sprintf("nodax_central_agents_online %d\n", online))

	b.WriteString("# HELP nodax_host_cpu_usage_percent Host CPU usage percent\n")
	b.WriteString("# TYPE nodax_host_cpu_usage_percent gauge\n")
	b.WriteString("# HELP nodax_host_ram_usage_percent Host RAM usage percent\n")
	b.WriteString("# TYPE nodax_host_ram_usage_percent gauge\n")
	b.WriteString("# HELP nodax_host_vm_total Host total VM count\n")
	b.WriteString("# TYPE nodax_host_vm_total gauge\n")
	b.WriteString("# HELP nodax_host_vm_running Host running VM count\n")
	b.WriteString("# TYPE nodax_host_vm_running gauge\n")
	b.WriteString("# HELP nodax_host_disk_usage_percent Host disk usage percent\n")
	b.WriteString("# TYPE nodax_host_disk_usage_percent gauge\n")
	b.WriteString("# HELP nodax_host_up Host availability (1 online, 0 offline)\n")
	b.WriteString("# TYPE nodax_host_up gauge\n")
	b.WriteString("# HELP nodax_host_uptime_seconds Host uptime in seconds\n")
	b.WriteString("# TYPE nodax_host_uptime_seconds gauge\n")

	for _, a := range agents {
		labels := fmt.Sprintf("agent_id=\"%s\",agent_name=\"%s\"", escapeLabel(a.ID), escapeLabel(a.Name))
		if a.Status == "online" {
			b.WriteString(fmt.Sprintf("nodax_host_up{%s} 1\n", labels))
		} else {
			b.WriteString(fmt.Sprintf("nodax_host_up{%s} 0\n", labels))
		}

		data := allData[a.ID]
		if data == nil || data.HostInfo == nil {
			continue
		}

		hi := data.HostInfo
		b.WriteString(fmt.Sprintf("nodax_host_cpu_usage_percent{%s} %.3f\n", labels, hi.CPUUsage))
		b.WriteString(fmt.Sprintf("nodax_host_ram_usage_percent{%s} %.3f\n", labels, hi.RAMUsePct))
		b.WriteString(fmt.Sprintf("nodax_host_vm_total{%s} %d\n", labels, hi.VMCount))
		b.WriteString(fmt.Sprintf("nodax_host_vm_running{%s} %d\n", labels, hi.VMRunning))
		b.WriteString(fmt.Sprintf("nodax_host_uptime_seconds{%s} %.0f\n", labels, hi.UptimeSeconds))

		for _, d := range hi.Disks {
			diskLabels := labels + fmt.Sprintf(",drive=\"%s\"", escapeLabel(d.Drive))
			b.WriteString(fmt.Sprintf("nodax_host_disk_usage_percent{%s} %.3f\n", diskLabels, d.UsePct))
		}
	}

	_, _ = w.Write([]byte(b.String()))
}

func escapeLabel(v string) string {
	v = strings.ReplaceAll(v, "\\", "\\\\")
	v = strings.ReplaceAll(v, "\"", "\\\"")
	v = strings.ReplaceAll(v, "\n", "")
	return v
}

func parseFlexibleTime(v string) (time.Time, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, false
	}

	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, v); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
