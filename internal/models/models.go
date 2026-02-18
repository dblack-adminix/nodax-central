package models

import "time"

// Agent represents a registered Hyper-V host running nodax-server
type Agent struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`     // Display name (e.g. "HV-SERVER-01")
	URL       string    `json:"url"`      // Base URL (e.g. "http://192.168.1.10:9000")
	APIKey    string    `json:"apiKey"`   // X-API-Key for authentication
	Status    string    `json:"status"`   // online / offline / error
	LastSeen  time.Time `json:"lastSeen"` // Last successful poll
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// AgentData holds cached data from an agent
type AgentData struct {
	AgentID   string        `json:"agentId"`
	Status    *StatusInfo   `json:"status,omitempty"`
	HostInfo  *HostInfo     `json:"hostInfo,omitempty"`
	VMs       []VM          `json:"vms,omitempty"`
	Health    *HealthReport `json:"health,omitempty"`
	FetchedAt time.Time     `json:"fetchedAt"`
	Error     string        `json:"error,omitempty"`
}

// StatusInfo from /api/v1/status
type StatusInfo struct {
	App     string `json:"app"`
	Version string `json:"version"`
	Host    string `json:"host"`
	Status  string `json:"status"`
}

// HostInfo from /api/v1/host/info
type HostInfo struct {
	ComputerName  string     `json:"computerName"`
	OSName        string     `json:"osName"`
	CPUUsage      float64    `json:"cpuUsage"`
	TotalRAM      int64      `json:"totalRAM"`
	UsedRAM       int64      `json:"usedRAM"`
	RAMUsePct     float64    `json:"ramUsePct"`
	Uptime        string     `json:"uptime"`
	UptimeSeconds float64    `json:"uptimeSeconds"`
	VMCount       int        `json:"vmCount"`
	VMRunning     int        `json:"vmRunning"`
	Disks         []DiskInfo `json:"disks"`
}

// DiskInfo from host info
type DiskInfo struct {
	Drive   string  `json:"drive"`
	TotalGB float64 `json:"totalGB"`
	FreeGB  float64 `json:"freeGB"`
	UsePct  float64 `json:"usePct"`
}

// VM from /api/v1/vms
type VM struct {
	Name           string  `json:"name"`
	State          string  `json:"state"`
	CPUUsage       float64 `json:"cpuUsage"`
	MemoryAssigned int64   `json:"memoryAssigned"`
}

// HealthReport from /api/v1/health
type HealthReport struct {
	Timestamp string         `json:"timestamp"`
	Overall   string         `json:"overall"`
	Checks    []HealthStatus `json:"checks"`
}

// HealthStatus single check
type HealthStatus struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Value   string `json:"value"`
}

// CentralConfig holds central server settings
type CentralConfig struct {
	PollIntervalSec int                             `json:"pollIntervalSec"`
	Port            string                          `json:"port"`
	CaddyDomain     string                          `json:"caddyDomain"`
	LicenseKey      string                          `json:"licenseKey,omitempty"`
	LicenseServer   string                          `json:"licenseServer,omitempty"`
	LicensePubKey   string                          `json:"licensePubKey,omitempty"`
	LicenseStatus   string                          `json:"licenseStatus,omitempty"`
	LicenseReason   string                          `json:"licenseReason,omitempty"`
	LicenseExpires  string                          `json:"licenseExpires,omitempty"`
	LicenseChecked  string                          `json:"licenseChecked,omitempty"`
	LicenseGraceTo  string                          `json:"licenseGraceTo,omitempty"`
	LicenseLastErr  string                          `json:"licenseLastErr,omitempty"`
	Theme           string                          `json:"theme"`
	Language        string                          `json:"language"`
	RetentionDays   int                             `json:"retentionDays"`
	BgColor         string                          `json:"bgColor"`
	BgImage         string                          `json:"bgImage"`
	RolePolicies    map[string][]UserHostPermission `json:"rolePolicies,omitempty"`
	RoleSections    map[string]RoleSectionPolicy    `json:"roleSections,omitempty"`
	JWTSecret       string                          `json:"jwtSecret,omitempty"`
}

type RoleSectionPolicy struct {
	Overview   bool `json:"overview"`
	Statistics bool `json:"statistics"`
	Storage    bool `json:"storage"`
	Settings   bool `json:"settings"`
	Security   bool `json:"security"`
}

// HostStats per-host statistics for the stats page
type HostStats struct {
	AgentID    string     `json:"agentId"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	CPU        float64    `json:"cpu"`
	RAMPct     float64    `json:"ramPct"`
	RAMUsedGB  float64    `json:"ramUsedGB"`
	RAMTotalGB float64    `json:"ramTotalGB"`
	VMTotal    int        `json:"vmTotal"`
	VMRunning  int        `json:"vmRunning"`
	Disks      []DiskInfo `json:"disks"`
	Uptime     string     `json:"uptime"`
	OS         string     `json:"os"`
}

// AggregatedStats for the statistics page
type AggregatedStats struct {
	Hosts       []HostStats `json:"hosts"`
	TotalHosts  int         `json:"totalHosts"`
	OnlineHosts int         `json:"onlineHosts"`
	TotalVMs    int         `json:"totalVMs"`
	RunningVMs  int         `json:"runningVMs"`
	AvgCPU      float64     `json:"avgCpu"`
	AvgRAM      float64     `json:"avgRam"`
	TotalRAMGB  float64     `json:"totalRamGB"`
	UsedRAMGB   float64     `json:"usedRamGB"`
	TotalDiskGB float64     `json:"totalDiskGB"`
	UsedDiskGB  float64     `json:"usedDiskGB"`
}

// MetricPoint is a single data point in the host metrics history
type MetricPoint struct {
	Timestamp time.Time `json:"t"`
	CPU       float64   `json:"cpu"`
	RAMPct    float64   `json:"ramPct"`
	RAMUsedGB float64   `json:"ramUsedGB"`
	DiskPct   float64   `json:"diskPct"`
	VMRunning int       `json:"vmRunning"`
	VMTotal   int       `json:"vmTotal"`
}

// HostHistory holds historical metric points for one agent
type HostHistory struct {
	AgentID string        `json:"agentId"`
	Name    string        `json:"name"`
	Points  []MetricPoint `json:"points"`
}

// User represents an authenticated user of the central dashboard
type User struct {
	ID              string               `json:"id"`
	Username        string               `json:"username"`
	Password        string               `json:"password,omitempty"` // bcrypt hash, stripped in API responses
	Role            string               `json:"role"`               // admin / engineer / user
	HostPermissions []UserHostPermission `json:"hostPermissions,omitempty"`
	CreatedAt       time.Time            `json:"createdAt"`
}

type UserHostPermission struct {
	AgentID string `json:"agentId"`
	View    bool   `json:"view"`
	Control bool   `json:"control"`
}

// DashboardOverview aggregated data for the overview page
type DashboardOverview struct {
	TotalAgents   int     `json:"totalAgents"`
	OnlineAgents  int     `json:"onlineAgents"`
	TotalVMs      int     `json:"totalVMs"`
	RunningVMs    int     `json:"runningVMs"`
	TotalCPU      float64 `json:"totalCpuAvg"`
	TotalRAMBytes int64   `json:"totalRamBytes"`
	UsedRAMBytes  int64   `json:"usedRamBytes"`
}

// CentralLog represents a log entry collected from an agent and stored centrally
type CentralLog struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agentId"`
	AgentName string    `json:"agentName"`
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"` // Backup, System, etc.
	TargetVM  string    `json:"targetVm"`
	Status    string    `json:"status"`
	Message   string    `json:"message"`
}
