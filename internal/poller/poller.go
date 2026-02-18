package poller

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"nodax-central/internal/netutil"
	"nodax-central/internal/models"
	"nodax-central/internal/store"
	"sync"
	"time"
)

const maxHistoryPoints = 720 // ~3 hours at 15s interval

// Poller periodically polls all registered agents for their data
type Poller struct {
	store    *store.Store
	client   *http.Client
	mu       sync.RWMutex
	cache    map[string]*models.AgentData    // agentID -> cached data
	history  map[string][]models.MetricPoint // agentID -> metric history
	interval time.Duration
	stopCh   chan struct{}
}

// New creates a new Poller
func New(s *store.Store, interval time.Duration) *Poller {
	return &Poller{
		store: s,
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
		cache:    make(map[string]*models.AgentData),
		history:  make(map[string][]models.MetricPoint),
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

func (p *Poller) loadHistoryFromStore() {
	agents, err := p.store.GetAllAgents()
	if err != nil || len(agents) == 0 {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	for _, a := range agents {
		pts, err := p.store.GetMetricHistory(a.ID)
		if err != nil || len(pts) == 0 {
			continue
		}
		if len(pts) > maxHistoryPoints {
			pts = pts[len(pts)-maxHistoryPoints:]
		}
		cp := make([]models.MetricPoint, len(pts))
		copy(cp, pts)
		p.history[a.ID] = cp
	}
}

func (p *Poller) loadCacheFromStore() {
	all, err := p.store.GetAllAgentData()
	if err != nil || len(all) == 0 {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	for id, d := range all {
		if d == nil {
			continue
		}
		cp := *d
		p.cache[id] = &cp
	}
}

// Start begins the polling loop
func (p *Poller) Start() {
	p.loadCacheFromStore()
	p.loadHistoryFromStore()
	go func() {
		p.pollAll()
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				p.pollAll()
			case <-p.stopCh:
				return
			}
		}
	}()
}

// Stop stops the polling loop
func (p *Poller) Stop() {
	close(p.stopCh)
}

// GetAgentData returns cached data for an agent
func (p *Poller) GetAgentData(agentID string) *models.AgentData {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cache[agentID]
}

// GetAllData returns cached data for all agents
func (p *Poller) GetAllData() map[string]*models.AgentData {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make(map[string]*models.AgentData, len(p.cache))
	for k, v := range p.cache {
		result[k] = v
	}
	return result
}

// PollAgent polls a single agent immediately
func (p *Poller) PollAgent(agent models.Agent) *models.AgentData {
	data := &models.AgentData{
		AgentID:   agent.ID,
		FetchedAt: time.Now(),
	}

	// Poll status
	var status models.StatusInfo
	if err := p.fetchJSON(agent, "/api/v1/status", &status); err != nil {
		data.Error = fmt.Sprintf("status: %v", err)
		_ = p.store.UpdateAgentStatus(agent.ID, "offline")
		p.mu.Lock()
		p.cache[agent.ID] = data
		p.mu.Unlock()
		_ = p.store.SaveAgentData(agent.ID, data)
		return data
	}
	data.Status = &status
	_ = p.store.UpdateAgentStatus(agent.ID, "online")

	// Poll host info
	var hostInfo models.HostInfo
	if err := p.fetchJSON(agent, "/api/v1/host/info", &hostInfo); err == nil {
		data.HostInfo = &hostInfo
	} else {
		data.Error = fmt.Sprintf("host/info: %v", err)
	}

	// Poll VMs
	var vms []models.VM
	if err := p.fetchJSON(agent, "/api/v1/vms", &vms); err == nil {
		data.VMs = vms
	}

	// Poll health
	var health models.HealthReport
	if err := p.fetchJSON(agent, "/api/v1/health", &health); err == nil {
		data.Health = &health
	}

	p.mu.Lock()
	p.cache[agent.ID] = data
	// Record history point if we have host info
	var pointToPersist *models.MetricPoint
	if data.HostInfo != nil {
		var diskPct float64
		if len(data.HostInfo.Disks) > 0 {
			var totalGB, usedGB float64
			for _, d := range data.HostInfo.Disks {
				totalGB += d.TotalGB
				usedGB += d.TotalGB - d.FreeGB
			}
			if totalGB > 0 {
				diskPct = (usedGB / totalGB) * 100
			}
		}
		pt := models.MetricPoint{
			Timestamp: time.Now(),
			CPU:       data.HostInfo.CPUUsage,
			RAMPct:    data.HostInfo.RAMUsePct,
			RAMUsedGB: float64(data.HostInfo.UsedRAM) / (1024 * 1024 * 1024),
			DiskPct:   diskPct,
			VMRunning: data.HostInfo.VMRunning,
			VMTotal:   data.HostInfo.VMCount,
		}
		h := p.history[agent.ID]
		h = append(h, pt)
		if len(h) > maxHistoryPoints {
			h = h[len(h)-maxHistoryPoints:]
		}
		p.history[agent.ID] = h
		pointToPersist = &pt
	}
	// Poll logs and store centrally
	var rawLogs []struct {
		Timestamp string `json:"Timestamp"`
		Type      string `json:"Type"`
		TargetVM  string `json:"TargetVM"`
		Status    string `json:"Status"`
		Message   string `json:"Message"`
	}
	if err := p.fetchJSON(agent, "/api/v1/logs?limit=100", &rawLogs); err == nil && len(rawLogs) > 0 {
		centralLogs := make([]models.CentralLog, 0, len(rawLogs))
		for _, e := range rawLogs {
			ts, _ := parseFlexTime(e.Timestamp)
			if ts.IsZero() {
				ts = time.Now()
			}
			centralLogs = append(centralLogs, models.CentralLog{
				AgentID:   agent.ID,
				AgentName: agent.Name,
				Timestamp: ts,
				Type:      e.Type,
				TargetVM:  e.TargetVM,
				Status:    e.Status,
				Message:   e.Message,
			})
		}
		p.store.SaveLogs(centralLogs)
	}

	p.mu.Unlock()
	_ = p.store.SaveAgentData(agent.ID, data)

	if pointToPersist != nil {
		_ = p.store.AppendMetricPoint(agent.ID, *pointToPersist, maxHistoryPoints)
	}

	return data
}

// GetHistory returns the metrics history for an agent
func (p *Poller) GetHistory(agentID string) []models.MetricPoint {
	p.mu.RLock()
	defer p.mu.RUnlock()
	pts := p.history[agentID]
	if pts == nil {
		return []models.MetricPoint{}
	}
	result := make([]models.MetricPoint, len(pts))
	copy(result, pts)
	return result
}

// pollAll polls all registered agents
func (p *Poller) pollAll() {
	agents, err := p.store.GetAllAgents()
	if err != nil || len(agents) == 0 {
		return
	}

	var wg sync.WaitGroup
	for _, agent := range agents {
		wg.Add(1)
		go func(a models.Agent) {
			defer wg.Done()
			p.PollAgent(a)
		}(agent)
	}
	wg.Wait()

	// Purge logs older than retention period (default 30 days)
	p.store.PurgeLogs(30 * 24 * time.Hour)
}

// fetchJSON makes an authenticated GET request to an agent endpoint
func (p *Poller) fetchJSON(agent models.Agent, path string, result interface{}) error {
	base := netutil.NormalizeAgentBaseURL(agent.URL)
	url := base + path

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	if agent.APIKey != "" {
		req.Header.Set("X-API-Key", agent.APIKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return json.NewDecoder(resp.Body).Decode(result)
}

func parseFlexTime(v string) (time.Time, bool) {
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
