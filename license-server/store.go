package main

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.etcd.io/bbolt"
	"golang.org/x/crypto/bcrypt"
)

const (
	bucketLicenses     = "licenses"
	bucketLicenseByKey = "license_by_key"
	bucketAudit        = "audit"
	bucketAdmin        = "admin"
	bucketSessions     = "sessions"
	bucketAPIKeys      = "api_keys"
	bucketSettings     = "settings"
	adminUserKey       = "admin_user"
)

var (
	errLicenseNotFound = errors.New("license not found")
	errUnauthorized    = errors.New("unauthorized")
)

type License struct {
	ID               string `json:"id"`
	LicenseKey       string `json:"licenseKey"`
	CustomerName     string `json:"customerName"`
	CustomerEmail    string `json:"customerEmail,omitempty"`
	CustomerTelegram string `json:"customerTelegram,omitempty"`
	CustomerPhone    string `json:"customerPhone,omitempty"`
	CustomerCompany  string `json:"customerCompany,omitempty"`
	Plan             string `json:"plan"`
	MaxAgents        int    `json:"maxAgents"`
	ExpiresAt        string `json:"expiresAt"`
	Status           string `json:"status"`
	Notes            string `json:"notes,omitempty"`
	CreatedAt        string `json:"createdAt"`
	UpdatedAt        string `json:"updatedAt"`
	LastInstanceID   string `json:"lastInstanceId,omitempty"`
	LastHostname     string `json:"lastHostname,omitempty"`
	LastIP           string `json:"lastIP,omitempty"`
	ClientChatID     string `json:"clientChatId,omitempty"`
	LastCheckAt      string `json:"lastCheckAt,omitempty"`
	IsTrial          bool   `json:"isTrial,omitempty"`
}

type APIKey struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Key       string `json:"key"`
	Role      string `json:"role"`
	CreatedAt string `json:"createdAt"`
}

type AdminUser struct {
	Username     string `json:"username"`
	PasswordHash string `json:"passwordHash"`
}

type Session struct {
	ID        string `json:"id"`
	Kind      string `json:"kind,omitempty"` // admin | client
	LicenseID string `json:"licenseId,omitempty"`
	CreatedAt string `json:"createdAt"`
	ExpiresAt string `json:"expiresAt"`
}

type AuditEvent struct {
	ID        string `json:"id"`
	LicenseID string `json:"licenseId,omitempty"`
	Action    string `json:"action"`
	Actor     string `json:"actor"`
	Details   string `json:"details,omitempty"`
	CreatedAt string `json:"createdAt"`
}

type Store struct {
	db *bbolt.DB
}

func NewStore(path string) (*Store, error) {
	if path == "" {
		ex, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("resolve executable path: %w", err)
		}
		path = filepath.Join(filepath.Dir(ex), "license-server.db")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	db, err := bbolt.Open(path, 0600, &bbolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	err = db.Update(func(tx *bbolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists([]byte(bucketLicenses)); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists([]byte(bucketLicenseByKey)); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists([]byte(bucketAudit)); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists([]byte(bucketAdmin)); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists([]byte(bucketSessions)); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists([]byte(bucketAPIKeys)); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists([]byte(bucketSettings)); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("init buckets: %w", err)
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) CreateLicense(lic *License) error {
	if lic == nil {
		return fmt.Errorf("license is nil")
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		byKey := tx.Bucket([]byte(bucketLicenseByKey))
		if byKey.Get([]byte(lic.LicenseKey)) != nil {
			return fmt.Errorf("license key already exists")
		}
		buf, err := json.Marshal(lic)
		if err != nil {
			return err
		}
		if err := tx.Bucket([]byte(bucketLicenses)).Put([]byte(lic.ID), buf); err != nil {
			return err
		}
		return byKey.Put([]byte(lic.LicenseKey), []byte(lic.ID))
	})
}

func (s *Store) DeleteLicense(id string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketLicenses))
		raw := b.Get([]byte(id))
		if raw == nil {
			return errLicenseNotFound
		}
		var lic License
		if err := json.Unmarshal(raw, &lic); err != nil {
			return err
		}
		if err := b.Delete([]byte(id)); err != nil {
			return err
		}
		return tx.Bucket([]byte(bucketLicenseByKey)).Delete([]byte(lic.LicenseKey))
	})
}

func (s *Store) UpdateLicense(lic *License) error {
	if lic == nil {
		return fmt.Errorf("license is nil")
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketLicenses))
		cur := b.Get([]byte(lic.ID))
		if cur == nil {
			return errLicenseNotFound
		}

		var prev License
		if err := json.Unmarshal(cur, &prev); err != nil {
			return err
		}

		buf, err := json.Marshal(lic)
		if err != nil {
			return err
		}
		if err := b.Put([]byte(lic.ID), buf); err != nil {
			return err
		}

		if prev.LicenseKey != lic.LicenseKey {
			byKey := tx.Bucket([]byte(bucketLicenseByKey))
			if byKey.Get([]byte(lic.LicenseKey)) != nil {
				return fmt.Errorf("license key already exists")
			}
			if err := byKey.Delete([]byte(prev.LicenseKey)); err != nil {
				return err
			}
			if err := byKey.Put([]byte(lic.LicenseKey), []byte(lic.ID)); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) GetLicenseByID(id string) (*License, error) {
	var lic License
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketLicenses))
		v := b.Get([]byte(id))
		if v == nil {
			return errLicenseNotFound
		}
		return json.Unmarshal(v, &lic)
	})
	if err != nil {
		return nil, err
	}
	return &lic, nil
}

func (s *Store) GetLicenseByKey(key string) (*License, error) {
	var licID string
	err := s.db.View(func(tx *bbolt.Tx) error {
		byKey := tx.Bucket([]byte(bucketLicenseByKey))
		v := byKey.Get([]byte(key))
		if v == nil {
			return errLicenseNotFound
		}
		licID = string(v)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetLicenseByID(licID)
}

func (s *Store) ListLicenses() ([]License, error) {
	out := make([]License, 0)
	err := s.db.View(func(tx *bbolt.Tx) error {
		c := tx.Bucket([]byte(bucketLicenses)).Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var lic License
			if err := json.Unmarshal(v, &lic); err != nil {
				continue
			}
			out = append(out, lic)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt > out[j].CreatedAt
	})
	return out, nil
}

func (s *Store) GetAdmin() (*AdminUser, error) {
	var u AdminUser
	err := s.db.View(func(tx *bbolt.Tx) error {
		v := tx.Bucket([]byte(bucketAdmin)).Get([]byte(adminUserKey))
		if v == nil {
			return fmt.Errorf("no admin user")
		}
		return json.Unmarshal(v, &u)
	})
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Store) SetAdmin(u *AdminUser) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		buf, err := json.Marshal(u)
		if err != nil {
			return err
		}
		return tx.Bucket([]byte(bucketAdmin)).Put([]byte(adminUserKey), buf)
	})
}

func (s *Store) EnsureAdmin(defaultPassword string) error {
	_, err := s.GetAdmin()
	if err == nil {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(defaultPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.SetAdmin(&AdminUser{Username: "admin", PasswordHash: string(hash)})
}

func (s *Store) CheckPassword(password string) bool {
	u, err := s.GetAdmin()
	if err != nil {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) == nil
}

func (s *Store) ChangePassword(newPassword string) error {
	u, err := s.GetAdmin()
	if err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.PasswordHash = string(hash)
	return s.SetAdmin(u)
}

func (s *Store) createSession(kind, licenseID string) (*Session, error) {
	var sess Session
	err := s.db.Update(func(tx *bbolt.Tx) error {
		now := time.Now().UTC()
		sess = Session{
			ID:        randomStoreHex(32),
			Kind:      kind,
			LicenseID: licenseID,
			CreatedAt: now.Format(time.RFC3339),
			ExpiresAt: now.Add(24 * time.Hour).Format(time.RFC3339),
		}
		buf, err := json.Marshal(sess)
		if err != nil {
			return err
		}
		return tx.Bucket([]byte(bucketSessions)).Put([]byte(sess.ID), buf)
	})
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *Store) CreateAdminSession() (*Session, error) {
	return s.createSession("admin", "")
}

func (s *Store) CreateClientSession(licenseID string) (*Session, error) {
	if strings.TrimSpace(licenseID) == "" {
		return nil, fmt.Errorf("license id required")
	}
	return s.createSession("client", strings.TrimSpace(licenseID))
}

func (s *Store) ValidateSession(id string) bool {
	if id == "" {
		return false
	}
	var valid bool
	_ = s.db.View(func(tx *bbolt.Tx) error {
		v := tx.Bucket([]byte(bucketSessions)).Get([]byte(id))
		if v == nil {
			return nil
		}
		var sess Session
		if err := json.Unmarshal(v, &sess); err != nil {
			return nil
		}
		exp, err := time.Parse(time.RFC3339, sess.ExpiresAt)
		if err != nil {
			return nil
		}
		if !(sess.Kind == "" || sess.Kind == "admin") {
			return nil
		}
		valid = time.Now().UTC().Before(exp)
		return nil
	})
	return valid
}

func (s *Store) ValidateClientSession(id string) (string, bool) {
	if id == "" {
		return "", false
	}
	var licenseID string
	var valid bool
	_ = s.db.View(func(tx *bbolt.Tx) error {
		v := tx.Bucket([]byte(bucketSessions)).Get([]byte(id))
		if v == nil {
			return nil
		}
		var sess Session
		if err := json.Unmarshal(v, &sess); err != nil {
			return nil
		}
		exp, err := time.Parse(time.RFC3339, sess.ExpiresAt)
		if err != nil {
			return nil
		}
		if sess.Kind != "client" || strings.TrimSpace(sess.LicenseID) == "" {
			return nil
		}
		if time.Now().UTC().Before(exp) {
			licenseID = strings.TrimSpace(sess.LicenseID)
			valid = true
		}
		return nil
	})
	return licenseID, valid
}

func (s *Store) DeleteSession(id string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket([]byte(bucketSessions)).Delete([]byte(id))
	})
}

func randomStoreHex(n int) string {
	const hex = "0123456789abcdef"
	raw := make([]byte, n)
	_, _ = rand.Read(raw)
	out := make([]byte, n)
	for i, b := range raw {
		out[i] = hex[b%16]
	}
	return string(out)
}

func (s *Store) AddAudit(ev AuditEvent) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		buf, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		return tx.Bucket([]byte(bucketAudit)).Put([]byte(ev.ID), buf)
	})
}

func (s *Store) ListAudit() ([]AuditEvent, error) {
	out := make([]AuditEvent, 0)
	err := s.db.View(func(tx *bbolt.Tx) error {
		c := tx.Bucket([]byte(bucketAudit)).Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var ev AuditEvent
			if err := json.Unmarshal(v, &ev); err != nil {
				continue
			}
			out = append(out, ev)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt > out[j].CreatedAt
	})
	return out, nil
}

func (s *Store) GetSetting(key string) string {
	var val string
	_ = s.db.View(func(tx *bbolt.Tx) error {
		v := tx.Bucket([]byte(bucketSettings)).Get([]byte(key))
		if v != nil {
			val = string(v)
		}
		return nil
	})
	return val
}

func (s *Store) SetSetting(key, value string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket([]byte(bucketSettings)).Put([]byte(key), []byte(value))
	})
}

func (s *Store) GetAllSettings() map[string]string {
	out := make(map[string]string)
	_ = s.db.View(func(tx *bbolt.Tx) error {
		c := tx.Bucket([]byte(bucketSettings)).Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			out[string(k)] = string(v)
		}
		return nil
	})
	return out
}

func (s *Store) CreateAPIKey(ak *APIKey) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		buf, err := json.Marshal(ak)
		if err != nil {
			return err
		}
		return tx.Bucket([]byte(bucketAPIKeys)).Put([]byte(ak.ID), buf)
	})
}

func (s *Store) ListAPIKeys() ([]APIKey, error) {
	out := make([]APIKey, 0)
	err := s.db.View(func(tx *bbolt.Tx) error {
		c := tx.Bucket([]byte(bucketAPIKeys)).Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var ak APIKey
			if err := json.Unmarshal(v, &ak); err != nil {
				continue
			}
			out = append(out, ak)
		}
		return nil
	})
	return out, err
}

func (s *Store) DeleteAPIKey(id string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket([]byte(bucketAPIKeys)).Delete([]byte(id))
	})
}

func (s *Store) ValidateAPIKey(key string) (string, bool) {
	var role string
	var found bool
	_ = s.db.View(func(tx *bbolt.Tx) error {
		c := tx.Bucket([]byte(bucketAPIKeys)).Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var ak APIKey
			if err := json.Unmarshal(v, &ak); err != nil {
				continue
			}
			if ak.Key == key {
				role = ak.Role
				found = true
				return nil
			}
		}
		return nil
	})
	return role, found
}

func (s *Store) BackupDB() ([]byte, error) {
	var buf []byte
	err := s.db.View(func(tx *bbolt.Tx) error {
		data := make(map[string]map[string]json.RawMessage)
		for _, name := range []string{bucketLicenses, bucketLicenseByKey, bucketAudit, bucketAdmin, bucketSessions, bucketAPIKeys, bucketSettings} {
			b := tx.Bucket([]byte(name))
			if b == nil {
				continue
			}
			m := make(map[string]json.RawMessage)
			c := b.Cursor()
			for k, v := c.First(); k != nil; k, v = c.Next() {
				var raw json.RawMessage
				if json.Valid(v) {
					raw = json.RawMessage(v)
				} else {
					raw, _ = json.Marshal(string(v))
				}
				m[string(k)] = raw
			}
			data[name] = m
		}
		var err error
		buf, err = json.MarshalIndent(data, "", "  ")
		return err
	})
	return buf, err
}

func (s *Store) RestoreDB(data []byte) error {
	var parsed map[string]map[string]json.RawMessage
	if err := json.Unmarshal(data, &parsed); err != nil {
		return fmt.Errorf("invalid backup format: %w", err)
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		for name, entries := range parsed {
			b := tx.Bucket([]byte(name))
			if b == nil {
				continue
			}
			c := b.Cursor()
			for k, _ := c.First(); k != nil; k, _ = c.Next() {
				_ = b.Delete(k)
			}
			for k, v := range entries {
				var str string
				if json.Unmarshal(v, &str) == nil && !json.Valid([]byte(str)) {
					_ = b.Put([]byte(k), []byte(str))
				} else {
					_ = b.Put([]byte(k), []byte(v))
				}
			}
		}
		return nil
	})
}
