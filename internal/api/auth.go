package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"nodax-central/internal/models"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var jwtSecret []byte

func init() {
	b := make([]byte, 32)
	rand.Read(b)
	jwtSecret = b
}

func defaultRolePolicies() map[string][]models.UserHostPermission {
	// Bootstrap defaults for first run.
	return map[string][]models.UserHostPermission{
		"admin":    nil,
		"engineer": []models.UserHostPermission{},
		"user":     []models.UserHostPermission{},
	}
}

func normalizeRolePolicies(input map[string][]models.UserHostPermission) map[string][]models.UserHostPermission {
	if input == nil {
		return defaultRolePolicies()
	}
	out := map[string][]models.UserHostPermission{"admin": nil}
	for k, perms := range input {
		r := normalizeRole(k)
		if r == "" {
			continue
		}
		if r == "admin" {
			out[r] = nil
			continue
		}
		out[r] = cleanHostPermissions(perms)
	}
	return out
}

func fullSectionPolicy() models.RoleSectionPolicy {
	return models.RoleSectionPolicy{
		Overview:   true,
		Statistics: true,
		Storage:    true,
		Settings:   true,
		Security:   true,
	}
}

func defaultRoleSections() map[string]models.RoleSectionPolicy {
	return map[string]models.RoleSectionPolicy{
		"admin":    fullSectionPolicy(),
		"engineer": {},
		"user":     {},
	}
}

func normalizeRoleSections(input map[string]models.RoleSectionPolicy) map[string]models.RoleSectionPolicy {
	if input == nil {
		return defaultRoleSections()
	}
	out := map[string]models.RoleSectionPolicy{"admin": fullSectionPolicy()}
	for k, sec := range input {
		r := normalizeRole(k)
		if r == "" {
			continue
		}
		if r == "admin" {
			out[r] = fullSectionPolicy()
			continue
		}
		out[r] = sec
	}
	return out
}

func sectionAllowed(policy models.RoleSectionPolicy, section string) bool {
	switch section {
	case "overview":
		return policy.Overview
	case "statistics":
		return policy.Statistics
	case "storage":
		return policy.Storage
	case "settings":
		return policy.Settings
	case "security":
		return policy.Security
	default:
		return false
	}
}

func canAccessSection(cfg *models.CentralConfig, role, section string) bool {
	r := normalizeRole(role)
	if r == "admin" {
		return true
	}
	if r == "" || cfg == nil {
		return false
	}
	sec := normalizeRoleSections(cfg.RoleSections)
	p, ok := sec[r]
	if !ok {
		return false
	}
	return sectionAllowed(p, section)
}

func roleExists(cfg *models.CentralConfig, role string) bool {
	r := normalizeRole(role)
	if r == "" {
		return false
	}
	if r == "admin" {
		return true
	}
	if cfg == nil {
		cfg = &models.CentralConfig{}
	}
	pol := normalizeRolePolicies(cfg.RolePolicies)
	_, ok := pol[r]
	return ok
}

func ensureRolePoliciesNotInUse(newPolicies map[string][]models.UserHostPermission, users []models.User) error {
	for _, u := range users {
		r := normalizeRole(u.Role)
		if r == "" || r == "admin" {
			continue
		}
		if _, ok := newPolicies[r]; !ok {
			return fmt.Errorf("group '%s' is assigned to user '%s'", r, u.Username)
		}
	}
	return nil
}

func permissionsByRole(cfg *models.CentralConfig, role string) []models.UserHostPermission {
	r := normalizeRole(role)
	if r == "" {
		return []models.UserHostPermission{}
	}
	if r == "admin" {
		return nil
	}
	if cfg == nil {
		return []models.UserHostPermission{}
	}
	pol := normalizeRolePolicies(cfg.RolePolicies)
	if perms, ok := pol[r]; ok {
		return perms
	}
	return []models.UserHostPermission{}
}

func validGroupName(role string) bool {
	r := strings.ToLower(strings.TrimSpace(role))
	if r == "" {
		return false
	}
	for _, ch := range r {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-' {
			continue
		}
		return false
	}
	return true
}

func normalizeRole(role string) string {
	r := strings.ToLower(strings.TrimSpace(role))
	switch r {
	case "viewer", "read", "readonly", "read-only":
		return "user"
	}
	if !validGroupName(r) {
		return ""
	}
	return r
}

func cleanHostPermissions(perms []models.UserHostPermission) []models.UserHostPermission {
	if len(perms) == 0 {
		return nil
	}
	out := make([]models.UserHostPermission, 0, len(perms))
	seen := map[string]bool{}
	for _, p := range perms {
		aid := strings.TrimSpace(p.AgentID)
		if aid == "" || seen[aid] {
			continue
		}
		seen[aid] = true
		out = append(out, models.UserHostPermission{AgentID: aid, View: p.View, Control: p.Control})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type registerRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type authResponse struct {
	Token    string `json:"token"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

type userResponse struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	CreatedAt string `json:"createdAt"`
}

type updateUserRequest struct {
	Role string `json:"role"`
}

type rolePoliciesUpdateRequest struct {
	RolePolicies map[string][]models.UserHostPermission `json:"rolePolicies"`
	RoleSections map[string]models.RoleSectionPolicy    `json:"roleSections"`
}

func canViewAgent(user *models.User, agentID string) bool {
	if user == nil {
		return false
	}
	role := normalizeRole(user.Role)
	if role == "admin" {
		return true
	}
	if len(user.HostPermissions) == 0 {
		return false
	}
	for _, p := range user.HostPermissions {
		if p.AgentID == agentID && (p.View || p.Control) {
			return true
		}
	}
	return false
}

func canControlAgent(user *models.User, agentID string) bool {
	if user == nil {
		return false
	}
	role := normalizeRole(user.Role)
	if role == "admin" {
		return true
	}
	if len(user.HostPermissions) == 0 {
		return false
	}
	for _, p := range user.HostPermissions {
		if p.AgentID == agentID {
			return p.Control
		}
	}
	return false
}

func (h *Handler) currentUserFromRequest(r *http.Request) (*models.User, error) {
	uid := strings.TrimSpace(r.Header.Get("X-User-ID"))
	if uid == "" {
		return nil, fmt.Errorf("missing user id")
	}
	u, err := h.store.GetUserByID(uid)
	if err != nil {
		return nil, err
	}
	u.Role = normalizeRole(u.Role)
	cfg, _ := h.store.GetConfig()
	u.HostPermissions = permissionsByRole(cfg, u.Role)
	return u, nil
}

func generateJWT(userID, username, role string) (string, error) {
	claims := jwt.MapClaims{
		"sub":      userID,
		"username": username,
		"role":     role,
		"exp":      time.Now().Add(72 * time.Hour).Unix(),
		"iat":      time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret)
}

func parseJWT(tokenStr string) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return jwtSecret, nil
	})
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid claims")
	}
	return claims, nil
}

func SetJWTSecret(secret string) {
	if secret != "" {
		jwtSecret = []byte(secret)
	}
}

func GenerateRandomSecret() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// AuthMiddleware protects API routes requiring authentication
func (h *Handler) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Public routes: login, setup check, static files
		if path == "/api/auth/login" || path == "/api/auth/setup" {
			next.ServeHTTP(w, r)
			return
		}
		// Public: background images (loaded via CSS url(), no auth header)
		if strings.HasPrefix(path, "/api/backgrounds/") && r.Method == http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		// Public: Loki API for Grafana (no auth needed)
		if strings.HasPrefix(path, "/loki/") {
			next.ServeHTTP(w, r)
			return
		}
		// Public: Prometheus metrics
		if path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		// Public Grafana read endpoint
		if path == "/api/grafana/logs" && r.Method == http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		// Allow first user registration if no users exist
		if path == "/api/auth/register" && r.Method == http.MethodPost {
			if h.store.UserCount() == 0 {
				next.ServeHTTP(w, r)
				return
			}
		}
		// Non-API routes (frontend static files)
		if !strings.HasPrefix(path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		// Check Authorization header
		auth := r.Header.Get("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, `{"error":"unauthorized"}`, 401)
			return
		}
		tokenStr := strings.TrimPrefix(auth, "Bearer ")
		claims, err := parseJWT(tokenStr)
		if err != nil {
			http.Error(w, `{"error":"invalid token"}`, 401)
			return
		}

		cfg, _ := h.store.GetConfig()

		// Section-based route restrictions
		role := normalizeRole(fmt.Sprintf("%v", claims["role"]))
		if role == "" {
			role = "user"
		}
		requiredSection := ""
		switch {
		case path == "/api/overview":
			requiredSection = "overview"
		case path == "/api/stats":
			requiredSection = "statistics"
		case path == "/api/config" || path == "/api/config/backup" || path == "/api/config/restore" || path == "/api/backgrounds" || strings.HasPrefix(path, "/api/backgrounds/"):
			requiredSection = "settings"
		case path == "/api/auth/register" || path == "/api/auth/users" || strings.HasPrefix(path, "/api/auth/users/") || path == "/api/auth/role-policies":
			requiredSection = "security"
		case strings.Contains(path, "/proxy/api/v1/s3/") || strings.Contains(path, "/proxy/api/v1/smb/") || strings.Contains(path, "/proxy/api/v1/webdav/"):
			requiredSection = "storage"
		}
		if requiredSection != "" && !canAccessSection(cfg, role, requiredSection) {
			http.Error(w, `{"error":"forbidden"}`, 403)
			return
		}

		if blocked, reason := h.isWriteBlockedByLicense(path, r.Method); blocked {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":  "license_restricted",
				"reason": reason,
			})
			return
		}

		r.Header.Set("X-User-ID", fmt.Sprintf("%v", claims["sub"]))
		r.Header.Set("X-User-Role", role)
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) handleAuthSetup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	count := h.store.UserCount()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"needsSetup": count == 0,
		"userCount":  count,
	})
}

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, 405)
		return
	}
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, 400)
		return
	}
	user, err := h.store.GetUserByUsername(req.Username)
	if err != nil || !h.store.CheckPassword(user, req.Password) {
		http.Error(w, `{"error":"invalid credentials"}`, 401)
		return
	}
	token, err := generateJWT(user.ID, user.Username, normalizeRole(user.Role))
	if err != nil {
		http.Error(w, `{"error":"token error"}`, 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(authResponse{Token: token, Username: user.Username, Role: normalizeRole(user.Role)})
}

func (h *Handler) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, 405)
		return
	}
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, 400)
		return
	}
	if req.Username == "" || req.Password == "" {
		http.Error(w, `{"error":"username and password required"}`, 400)
		return
	}
	req.Role = normalizeRole(req.Role)
	if req.Role == "" {
		http.Error(w, `{"error":"invalid group"}`, 400)
		return
	}
	// Only admins can create non-admin users (or first user is always admin)
	if h.store.UserCount() == 0 {
		req.Role = "admin"
	} else {
		cfg, _ := h.store.GetConfig()
		if !roleExists(cfg, req.Role) {
			http.Error(w, `{"error":"group not found"}`, 400)
			return
		}
	}
	user, err := h.store.CreateUser(req.Username, req.Password, req.Role, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), 400)
		return
	}
	token, _ := generateJWT(user.ID, user.Username, normalizeRole(user.Role))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(authResponse{Token: token, Username: user.Username, Role: normalizeRole(user.Role)})
}

func (h *Handler) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	user, err := h.store.GetUserByID(userID)
	if err != nil {
		http.Error(w, `{"error":"user not found"}`, 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(userResponse{
		ID: user.ID, Username: user.Username, Role: normalizeRole(user.Role),
		CreatedAt: user.CreatedAt.Format(time.RFC3339),
	})
}

func (h *Handler) handleUsers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		users, _ := h.store.GetAllUsers()
		result := make([]userResponse, 0, len(users))
		for _, u := range users {
			result = append(result, userResponse{
				ID: u.ID, Username: u.Username, Role: normalizeRole(u.Role),
				CreatedAt: u.CreatedAt.Format(time.RFC3339),
			})
		}
		json.NewEncoder(w).Encode(result)

	case http.MethodPut:
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) < 5 {
			http.Error(w, `{"error":"id required"}`, 400)
			return
		}
		id := parts[len(parts)-1]
		if id == "" {
			http.Error(w, `{"error":"id required"}`, 400)
			return
		}

		u, err := h.store.GetUserByID(id)
		if err != nil {
			http.Error(w, `{"error":"user not found"}`, 404)
			return
		}

		var req updateUserRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid body"}`, 400)
			return
		}

		if req.Role != "" {
			nr := normalizeRole(req.Role)
			if nr == "" {
				http.Error(w, `{"error":"invalid group"}`, 400)
				return
			}
			cfg, _ := h.store.GetConfig()
			if !roleExists(cfg, nr) {
				http.Error(w, `{"error":"group not found"}`, 400)
				return
			}
			u.Role = nr
		}
		u.HostPermissions = nil

		if err := h.store.SaveUser(u); err != nil {
			http.Error(w, `{"error":"save failed"}`, 500)
			return
		}

		json.NewEncoder(w).Encode(userResponse{
			ID:        u.ID,
			Username:  u.Username,
			Role:      normalizeRole(u.Role),
			CreatedAt: u.CreatedAt.Format(time.RFC3339),
		})

	case http.MethodDelete:
		// /api/auth/users/{id}
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) < 5 {
			http.Error(w, `{"error":"id required"}`, 400)
			return
		}
		id := parts[len(parts)-1]
		callerID := r.Header.Get("X-User-ID")
		if id == callerID {
			http.Error(w, `{"error":"cannot delete yourself"}`, 400)
			return
		}
		if err := h.store.DeleteUser(id); err != nil {
			http.Error(w, `{"error":"delete failed"}`, 500)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})

	default:
		http.Error(w, `{"error":"method not allowed"}`, 405)
	}
}

func (h *Handler) handleRolePolicies(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		cfg, _ := h.store.GetConfig()
		json.NewEncoder(w).Encode(map[string]any{
			"rolePolicies": normalizeRolePolicies(cfg.RolePolicies),
			"roleSections": normalizeRoleSections(cfg.RoleSections),
		})

	case http.MethodPut:
		cfg, _ := h.store.GetConfig()
		var req rolePoliciesUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid body"}`, 400)
			return
		}
		nextPolicies := normalizeRolePolicies(req.RolePolicies)
		nextSections := normalizeRoleSections(req.RoleSections)
		for role := range nextPolicies {
			if role == "admin" {
				continue
			}
			if _, ok := nextSections[role]; !ok {
				nextSections[role] = models.RoleSectionPolicy{}
			}
		}
		for role := range nextSections {
			if role == "admin" {
				continue
			}
			if _, ok := nextPolicies[role]; !ok {
				nextPolicies[role] = []models.UserHostPermission{}
			}
		}
		users, _ := h.store.GetAllUsers()
		if err := ensureRolePoliciesNotInUse(nextPolicies, users); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), 400)
			return
		}
		cfg.RolePolicies = nextPolicies
		cfg.RoleSections = nextSections
		if err := h.store.SaveConfig(cfg); err != nil {
			http.Error(w, `{"error":"save failed"}`, 500)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"rolePolicies": cfg.RolePolicies,
			"roleSections": cfg.RoleSections,
		})

	default:
		http.Error(w, `{"error":"method not allowed"}`, 405)
	}
}

func (h *Handler) RegisterAuthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/auth/setup", h.handleAuthSetup)
	mux.HandleFunc("/api/auth/login", h.handleLogin)
	mux.HandleFunc("/api/auth/register", h.handleRegister)
	mux.HandleFunc("/api/auth/me", h.handleAuthMe)
	mux.HandleFunc("/api/auth/users", h.handleUsers)
	mux.HandleFunc("/api/auth/users/", h.handleUsers)
	mux.HandleFunc("/api/auth/role-policies", h.handleRolePolicies)
}
