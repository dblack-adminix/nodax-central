package api

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"nodax-central/internal/models"
)

type licenseValidatePayload struct {
	Status    string `json:"status"`
	Valid     bool   `json:"valid"`
	Reason    string `json:"reason"`
	ExpiresAt string `json:"expiresAt"`
	GraceDays int    `json:"graceDays"`
}

type licenseValidateResponse struct {
	Payload   json.RawMessage `json:"payload"`
	Signature string `json:"signature"`
	Algorithm string `json:"algorithm"`
}

var licenseWriteExempt = map[string]bool{
	"/api/license/status":  true,
	"/api/license/recheck": true,
	"/api/config":          true,
}

func (h *Handler) StartLicenseLoop(stop <-chan struct{}) {
	go func() {
		_ = h.refreshLicenseStatus()
		ticker := time.NewTicker(12 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = h.refreshLicenseStatus()
			case <-stop:
				return
			}
		}
	}()
}

func (h *Handler) handleLicenseStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", 405)
		return
	}
	cfg, err := h.store.GetConfig()
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{
		"status":       strings.TrimSpace(cfg.LicenseStatus),
		"reason":       strings.TrimSpace(cfg.LicenseReason),
		"expiresAt":    strings.TrimSpace(cfg.LicenseExpires),
		"checkedAt":    strings.TrimSpace(cfg.LicenseChecked),
		"graceUntil":   strings.TrimSpace(cfg.LicenseGraceTo),
		"lastError":    strings.TrimSpace(cfg.LicenseLastErr),
		"publicKey":    strings.TrimSpace(cfg.LicensePubKey),
		"server":       strings.TrimSpace(cfg.LicenseServer),
		"configured":   strings.TrimSpace(cfg.LicenseKey) != "" && strings.TrimSpace(cfg.LicenseServer) != "",
		"writeEnabled": isWriteAllowedByLicense(cfg),
	})
}

func (h *Handler) handleLicenseRecheck(w http.ResponseWriter, r *http.Request) {
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
	if err := h.refreshLicenseStatus(); err != nil {
		httpErr(w, err, 500)
		return
	}
	cfg, err := h.store.GetConfig()
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{
		"status":     strings.TrimSpace(cfg.LicenseStatus),
		"reason":     strings.TrimSpace(cfg.LicenseReason),
		"expiresAt":  strings.TrimSpace(cfg.LicenseExpires),
		"checkedAt":  strings.TrimSpace(cfg.LicenseChecked),
		"graceUntil": strings.TrimSpace(cfg.LicenseGraceTo),
		"lastError":  strings.TrimSpace(cfg.LicenseLastErr),
		"publicKey":  strings.TrimSpace(cfg.LicensePubKey),
		"server":     strings.TrimSpace(cfg.LicenseServer),
	})
}

func (h *Handler) isWriteBlockedByLicense(path, method string) (bool, string) {
	if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
		return false, ""
	}
	if licenseWriteExempt[path] {
		return false, ""
	}
	cfg, err := h.store.GetConfig()
	if err != nil {
		return true, "license_status_unavailable"
	}
	now := time.Now().UTC()

	// Keep enforcement strict: refresh stale license state before write operations.
	if shouldRefreshLicenseStatus(cfg, now) {
		_ = h.refreshLicenseStatus()
		if refreshed, rerr := h.store.GetConfig(); rerr == nil {
			cfg = refreshed
		}
	}

	if isWriteAllowedByLicenseAt(cfg, now) {
		return false, ""
	}
	status := strings.TrimSpace(cfg.LicenseStatus)
	if status == "" {
		status = "unconfigured"
	}
	return true, status
}

func isWriteAllowedByLicense(cfg *models.CentralConfig) bool {
	return isWriteAllowedByLicenseAt(cfg, time.Now().UTC())
}

func isWriteAllowedByLicenseAt(cfg *models.CentralConfig, now time.Time) bool {
	if cfg == nil {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(cfg.LicenseStatus))
	switch status {
	case "active":
		expRaw := strings.TrimSpace(cfg.LicenseExpires)
		if expRaw == "" {
			return true
		}
		exp, err := time.Parse(time.RFC3339, expRaw)
		if err != nil {
			return false
		}
		return exp.After(now)
	case "grace":
		graceRaw := strings.TrimSpace(cfg.LicenseGraceTo)
		if graceRaw == "" {
			return false
		}
		grace, err := time.Parse(time.RFC3339, graceRaw)
		if err != nil {
			return false
		}
		return grace.After(now)
	default:
		return false
	}
}

func shouldRefreshLicenseStatus(cfg *models.CentralConfig, now time.Time) bool {
	if cfg == nil {
		return false
	}
	if strings.TrimSpace(cfg.LicenseKey) == "" || strings.TrimSpace(cfg.LicenseServer) == "" {
		return false
	}
	checkedRaw := strings.TrimSpace(cfg.LicenseChecked)
	if checkedRaw == "" {
		return true
	}
	checkedAt, err := time.Parse(time.RFC3339, checkedRaw)
	if err != nil {
		return true
	}
	return now.Sub(checkedAt) > 5*time.Minute
}

func (h *Handler) refreshLicenseStatus() error {
	h.licenseMu.Lock()
	defer h.licenseMu.Unlock()

	cfg, err := h.store.GetConfig()
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	cfg.LicenseChecked = now.Format(time.RFC3339)

	server := strings.TrimSpace(cfg.LicenseServer)
	if server == "" {
		server = strings.TrimSpace(os.Getenv("NODAX_LICENSE_SERVER"))
		if server != "" {
			cfg.LicenseServer = server
		}
	}

	if server != "" && strings.TrimSpace(cfg.LicensePubKey) == "" {
		if fetched, pubErr := fetchLicenseServerPublicKey(server); pubErr == nil && fetched != "" {
			cfg.LicensePubKey = fetched
		}
	}

	if strings.TrimSpace(cfg.LicenseKey) == "" || server == "" {
		cfg.LicenseStatus = "unconfigured"
		cfg.LicenseReason = "license_key_or_server_missing"
		cfg.LicenseLastErr = ""
		return h.store.SaveConfig(cfg)
	}

	agentCount := 0
	if agents, err := h.store.GetAllAgents(); err == nil {
		agentCount = len(agents)
	}

	body, _ := json.Marshal(map[string]any{
		"licenseKey": cfg.LicenseKey,
		"instanceId": h.instanceID,
		"hostname":   h.instanceID,
		"version":    "nodax-central",
		"agentCount": agentCount,
	})
	endpoint := strings.TrimRight(server, "/") + "/api/v1/license/validate"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		cfg.LicenseStatus = "invalid"
		cfg.LicenseReason = "request_build_failed"
		cfg.LicenseLastErr = err.Error()
		return h.store.SaveConfig(cfg)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		cfg.LicenseLastErr = err.Error()
		cfg.LicenseReason = "license_server_unreachable"
		if grace, gErr := time.Parse(time.RFC3339, strings.TrimSpace(cfg.LicenseGraceTo)); gErr == nil && grace.After(now) {
			cfg.LicenseStatus = "grace"
		} else {
			cfg.LicenseStatus = "invalid"
		}
		return h.store.SaveConfig(cfg)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		cfg.LicenseStatus = "invalid"
		cfg.LicenseReason = "license_server_error"
		cfg.LicenseLastErr = fmt.Sprintf("status %d", resp.StatusCode)
		return h.store.SaveConfig(cfg)
	}

	var parsed licenseValidateResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		cfg.LicenseStatus = "invalid"
		cfg.LicenseReason = "invalid_license_response"
		cfg.LicenseLastErr = err.Error()
		return h.store.SaveConfig(cfg)
	}

	// Verify Ed25519 signature
	pubKeyRaw := strings.TrimSpace(cfg.LicensePubKey)
	if pubKeyRaw == "" {
		cfg.LicenseStatus = "invalid"
		cfg.LicenseReason = "missing_public_key"
		cfg.LicenseLastErr = "license server public key not configured"
		return h.store.SaveConfig(cfg)
	}
	pubKey, err := decodeLicensePublicKey(pubKeyRaw)
	if err != nil {
		cfg.LicenseStatus = "invalid"
		cfg.LicenseReason = "invalid_public_key"
		cfg.LicenseLastErr = "failed to decode public key: " + err.Error()
		return h.store.SaveConfig(cfg)
	}

	sig, err := base64.StdEncoding.DecodeString(parsed.Signature)
	if err != nil {
		cfg.LicenseStatus = "invalid"
		cfg.LicenseReason = "invalid_signature_format"
		cfg.LicenseLastErr = "failed to decode signature: " + err.Error()
		return h.store.SaveConfig(cfg)
	}

	if len(parsed.Payload) == 0 {
		cfg.LicenseStatus = "invalid"
		cfg.LicenseReason = "invalid_license_response"
		cfg.LicenseLastErr = "empty payload"
		return h.store.SaveConfig(cfg)
	}
	if !ed25519.Verify(pubKey, parsed.Payload, sig) {
		// Public key could be rotated on license server. Try refresh once and re-verify.
		if fetched, ferr := fetchLicenseServerPublicKey(server); ferr == nil && fetched != "" {
			if fetched != strings.TrimSpace(cfg.LicensePubKey) {
				cfg.LicensePubKey = fetched
			}
			if refreshedKey, derr := decodeLicensePublicKey(fetched); derr == nil && ed25519.Verify(refreshedKey, parsed.Payload, sig) {
				pubKey = refreshedKey
			} else {
				cfg.LicenseStatus = "invalid"
				cfg.LicenseReason = "signature_verification_failed"
				cfg.LicenseLastErr = "license response signature mismatch"
				return h.store.SaveConfig(cfg)
			}
		} else {
			cfg.LicenseStatus = "invalid"
			cfg.LicenseReason = "signature_verification_failed"
			cfg.LicenseLastErr = "license response signature mismatch"
			return h.store.SaveConfig(cfg)
		}
	}

	var payload licenseValidatePayload
	if err := json.Unmarshal(parsed.Payload, &payload); err != nil {
		cfg.LicenseStatus = "invalid"
		cfg.LicenseReason = "invalid_license_response"
		cfg.LicenseLastErr = "invalid payload: " + err.Error()
		return h.store.SaveConfig(cfg)
	}

	cfg.LicenseExpires = strings.TrimSpace(payload.ExpiresAt)
	cfg.LicenseReason = strings.TrimSpace(payload.Reason)
	cfg.LicenseLastErr = ""

	status := strings.ToLower(strings.TrimSpace(payload.Status))
	if payload.Valid && status == "active" {
		cfg.LicenseStatus = "active"
		graceDays := payload.GraceDays
		if graceDays < 0 {
			graceDays = 0
		}
		cfg.LicenseGraceTo = now.AddDate(0, 0, graceDays).Format(time.RFC3339)
	} else {
		if status == "" {
			status = "invalid"
		}
		cfg.LicenseStatus = status
		if status != "grace" {
			cfg.LicenseGraceTo = ""
		}
	}
	return h.store.SaveConfig(cfg)
}

func decodeLicensePublicKey(raw string) (ed25519.PublicKey, error) {
	key := strings.TrimSpace(raw)
	key = strings.TrimPrefix(key, "0x")
	key = strings.TrimPrefix(key, "0X")

	if key == "" {
		return nil, fmt.Errorf("empty")
	}

	if isHexKey(key) {
		b, err := hex.DecodeString(key)
		if err != nil {
			return nil, err
		}
		if len(b) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("invalid public key size: %d", len(b))
		}
		return ed25519.PublicKey(b), nil
	}

	if b, err := base64.StdEncoding.DecodeString(key); err == nil {
		if len(b) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("invalid public key size: %d", len(b))
		}
		return ed25519.PublicKey(b), nil
	}

	if b, err := base64.RawStdEncoding.DecodeString(key); err == nil {
		if len(b) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("invalid public key size: %d", len(b))
		}
		return ed25519.PublicKey(b), nil
	}

	if b, err := base64.RawURLEncoding.DecodeString(key); err == nil {
		if len(b) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("invalid public key size: %d", len(b))
		}
		return ed25519.PublicKey(b), nil
	}

	return nil, fmt.Errorf("unsupported format (expected base64 or hex)")
}

func isHexKey(s string) bool {
	if len(s)%2 != 0 {
		return false
	}
	for _, ch := range s {
		if (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F') {
			continue
		}
		return false
	}
	return true
}

func fetchLicenseServerPublicKey(server string) (string, error) {
	pubEndpoint := strings.TrimRight(server, "/") + "/api/v1/public-key"
	pubResp, pubErr := (&http.Client{Timeout: 10 * time.Second}).Get(pubEndpoint)
	if pubErr != nil {
		return "", pubErr
	}
	defer pubResp.Body.Close()
	if pubResp.StatusCode >= 300 {
		return "", fmt.Errorf("public key endpoint status %d", pubResp.StatusCode)
	}
	var pubData struct {
		PublicKey string `json:"publicKey"`
	}
	if err := json.NewDecoder(pubResp.Body).Decode(&pubData); err != nil {
		return "", err
	}
	return strings.TrimSpace(pubData.PublicKey), nil
}
