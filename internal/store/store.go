package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"nodax-central/internal/models"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.etcd.io/bbolt"
	_ "modernc.org/sqlite"
)

const (
	BucketAgents    = "Agents"
	BucketConfig    = "Config"
	BucketUsers     = "Users"
	BucketLogs      = "Logs"
	BucketAgentData = "AgentData"
	BucketMetrics   = "Metrics"
	KeyCentral      = "central"
)

// Store manages persistent storage for the central server
type Store struct {
	db             *bbolt.DB
	sqlDB          *sql.DB
	readFromSQLite bool
}

// New creates a new store instance
func New() (*Store, error) {
	baseDir, err := resolveDataDir()
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(baseDir, "nodax-central.db")
	sqlitePath := filepath.Join(baseDir, "nodax-central.sqlite")

	db, err := bbolt.Open(dbPath, 0600, &bbolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("failed to open db: %w", err)
	}

	err = db.Update(func(tx *bbolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists([]byte(BucketAgents)); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists([]byte(BucketAgentData)); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists([]byte(BucketUsers)); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists([]byte(BucketLogs)); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists([]byte(BucketMetrics)); err != nil {
			return err
		}
		_, err := tx.CreateBucketIfNotExists([]byte(BucketConfig))
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("failed to init buckets: %w", err)
	}

	sqlDB, err := sql.Open("sqlite", sqlitePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite: %w", err)
	}
	if _, err := sqlDB.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL; PRAGMA busy_timeout=5000;`); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to init sqlite pragmas: %w", err)
	}
	if err := initSQLiteSchema(sqlDB); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to init sqlite schema: %w", err)
	}

	readFromSQLite := false
	if v := os.Getenv("NODAX_DB_READ_SQLITE"); v != "" {
		if b, e := strconv.ParseBool(v); e == nil {
			readFromSQLite = b
		}
	}

	return &Store{db: db, sqlDB: sqlDB, readFromSQLite: readFromSQLite}, nil
}

func resolveDataDir() (string, error) {
	if v := strings.TrimSpace(os.Getenv("NODAX_DATA_DIR")); v != "" {
		if err := os.MkdirAll(v, 0o755); err != nil {
			return "", fmt.Errorf("failed to create data dir %s: %w", v, err)
		}
		return v, nil
	}

	wd, err := os.Getwd()
	if err == nil && strings.TrimSpace(wd) != "" {
		return wd, nil
	}

	ex, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to resolve data dir (wd/exe): %w", err)
	}
	return filepath.Dir(ex), nil
}

func initSQLiteSchema(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS agents (id TEXT PRIMARY KEY, data TEXT NOT NULL);`,
		`CREATE TABLE IF NOT EXISTS config (k TEXT PRIMARY KEY, data TEXT NOT NULL);`,
		`CREATE TABLE IF NOT EXISTS users (id TEXT PRIMARY KEY, username TEXT UNIQUE, password TEXT, role TEXT, created_at TEXT, data TEXT NOT NULL);`,
		`CREATE TABLE IF NOT EXISTS logs (id TEXT PRIMARY KEY, ts TEXT NOT NULL, agent_id TEXT, agent_name TEXT, type TEXT, status TEXT, target_vm TEXT, data TEXT NOT NULL);`,
		`CREATE INDEX IF NOT EXISTS idx_logs_ts ON logs(ts);`,
		`CREATE INDEX IF NOT EXISTS idx_logs_agent_ts ON logs(agent_id, ts);`,
		`CREATE TABLE IF NOT EXISTS agent_data (agent_id TEXT PRIMARY KEY, fetched_at TEXT, data TEXT NOT NULL);`,
		`CREATE TABLE IF NOT EXISTS metrics (agent_id TEXT NOT NULL, ts TEXT NOT NULL, data TEXT NOT NULL, PRIMARY KEY(agent_id, ts));`,
		`CREATE INDEX IF NOT EXISTS idx_metrics_agent_ts ON metrics(agent_id, ts);`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

// Close closes the database
func (s *Store) Close() error {
	if s.sqlDB != nil {
		_ = s.sqlDB.Close()
	}
	return s.db.Close()
}

// SaveAgent creates or updates an agent
func (s *Store) SaveAgent(agent *models.Agent) error {
	agent.UpdatedAt = time.Now()
	if agent.CreatedAt.IsZero() {
		agent.CreatedAt = time.Now()
	}
	raw, err := json.Marshal(agent)
	if err != nil {
		return err
	}

	err = s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketAgents))
		return b.Put([]byte(agent.ID), raw)
	})
	if err != nil {
		return err
	}
	if s.sqlDB != nil {
		_, _ = s.sqlDB.Exec(`INSERT INTO agents(id, data) VALUES(?, ?) ON CONFLICT(id) DO UPDATE SET data=excluded.data`, agent.ID, string(raw))
	}
	return nil
}

// GetAgent returns an agent by ID
func (s *Store) GetAgent(id string) (*models.Agent, error) {
	if s.readFromSQLite && s.sqlDB != nil {
		var raw string
		err := s.sqlDB.QueryRow(`SELECT data FROM agents WHERE id=?`, id).Scan(&raw)
		if err == nil {
			var agent models.Agent
			if json.Unmarshal([]byte(raw), &agent) == nil {
				return &agent, nil
			}
		}
	}
	var agent models.Agent
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketAgents))
		data := b.Get([]byte(id))
		if data == nil {
			return fmt.Errorf("agent not found: %s", id)
		}
		return json.Unmarshal(data, &agent)
	})
	return &agent, err
}

// GetAllAgents returns all registered agents
func (s *Store) GetAllAgents() ([]models.Agent, error) {
	if s.readFromSQLite && s.sqlDB != nil {
		rows, err := s.sqlDB.Query(`SELECT data FROM agents`)
		if err == nil {
			defer rows.Close()
			var out []models.Agent
			for rows.Next() {
				var raw string
				if rows.Scan(&raw) != nil {
					continue
				}
				var a models.Agent
				if json.Unmarshal([]byte(raw), &a) == nil {
					out = append(out, a)
				}
			}
			return out, nil
		}
	}
	var agents []models.Agent
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketAgents))
		return b.ForEach(func(k, v []byte) error {
			var agent models.Agent
			if err := json.Unmarshal(v, &agent); err != nil {
				return nil
			}
			agents = append(agents, agent)
			return nil
		})
	})
	return agents, err
}

// DeleteAgent removes an agent by ID
func (s *Store) DeleteAgent(id string) error {
	err := s.db.Update(func(tx *bbolt.Tx) error {
		agents := tx.Bucket([]byte(BucketAgents))
		if err := agents.Delete([]byte(id)); err != nil {
			return err
		}
		agentData := tx.Bucket([]byte(BucketAgentData))
		if agentData != nil {
			_ = agentData.Delete([]byte(id))
		}
		metrics := tx.Bucket([]byte(BucketMetrics))
		if metrics != nil {
			_ = metrics.Delete([]byte(id))
		}
		return nil
	})
	if err != nil {
		return err
	}
	if s.sqlDB != nil {
		_, _ = s.sqlDB.Exec(`DELETE FROM agents WHERE id=?`, id)
		_, _ = s.sqlDB.Exec(`DELETE FROM agent_data WHERE agent_id=?`, id)
		_, _ = s.sqlDB.Exec(`DELETE FROM metrics WHERE agent_id=?`, id)
		_, _ = s.sqlDB.Exec(`DELETE FROM logs WHERE agent_id=?`, id)
	}
	return nil
}

// SaveAgentData persists latest cached payload for an agent.
func (s *Store) SaveAgentData(agentID string, data *models.AgentData) error {
	if agentID == "" || data == nil {
		return nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	err = s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketAgentData))
		if b == nil {
			return nil
		}
		return b.Put([]byte(agentID), raw)
	})
	if err != nil {
		return err
	}
	if s.sqlDB != nil {
		_, _ = s.sqlDB.Exec(`INSERT INTO agent_data(agent_id, fetched_at, data) VALUES(?, ?, ?) ON CONFLICT(agent_id) DO UPDATE SET fetched_at=excluded.fetched_at, data=excluded.data`, agentID, data.FetchedAt.UTC().Format(time.RFC3339Nano), string(raw))
	}
	return nil
}

// GetAgentData returns latest persisted cached payload for an agent.
func (s *Store) GetAgentData(agentID string) (*models.AgentData, error) {
	if agentID == "" {
		return nil, nil
	}
	if s.readFromSQLite && s.sqlDB != nil {
		var raw string
		err := s.sqlDB.QueryRow(`SELECT data FROM agent_data WHERE agent_id=?`, agentID).Scan(&raw)
		if err == nil {
			var d models.AgentData
			if json.Unmarshal([]byte(raw), &d) == nil {
				return &d, nil
			}
		}
	}
	var out *models.AgentData
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketAgentData))
		if b == nil {
			return nil
		}
		raw := b.Get([]byte(agentID))
		if raw == nil {
			return nil
		}
		var d models.AgentData
		if err := json.Unmarshal(raw, &d); err != nil {
			return nil
		}
		out = &d
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetAllAgentData returns persisted cached payloads keyed by agent ID.
func (s *Store) GetAllAgentData() (map[string]*models.AgentData, error) {
	if s.readFromSQLite && s.sqlDB != nil {
		rows, err := s.sqlDB.Query(`SELECT agent_id, data FROM agent_data`)
		if err == nil {
			defer rows.Close()
			out := make(map[string]*models.AgentData)
			for rows.Next() {
				var id, raw string
				if rows.Scan(&id, &raw) != nil {
					continue
				}
				var d models.AgentData
				if json.Unmarshal([]byte(raw), &d) == nil {
					cp := d
					out[id] = &cp
				}
			}
			return out, nil
		}
	}
	out := make(map[string]*models.AgentData)
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketAgentData))
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			var d models.AgentData
			if err := json.Unmarshal(v, &d); err != nil {
				return nil
			}
			id := string(k)
			cp := d
			out[id] = &cp
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// AppendMetricPoint appends a metric point for agent and keeps only latest maxPoints points.
func (s *Store) AppendMetricPoint(agentID string, pt models.MetricPoint, maxPoints int) error {
	if agentID == "" || maxPoints <= 0 {
		return nil
	}
	err := s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketMetrics))
		if b == nil {
			return nil
		}

		var history []models.MetricPoint
		if raw := b.Get([]byte(agentID)); raw != nil {
			_ = json.Unmarshal(raw, &history)
		}

		history = append(history, pt)
		if len(history) > maxPoints {
			history = history[len(history)-maxPoints:]
		}

		data, err := json.Marshal(history)
		if err != nil {
			return err
		}
		return b.Put([]byte(agentID), data)
	})
	if err != nil {
		return err
	}
	if s.sqlDB != nil {
		raw, _ := json.Marshal(pt)
		_, _ = s.sqlDB.Exec(`INSERT INTO metrics(agent_id, ts, data) VALUES(?, ?, ?) ON CONFLICT(agent_id, ts) DO UPDATE SET data=excluded.data`, agentID, pt.Timestamp.UTC().Format(time.RFC3339Nano), string(raw))
		_, _ = s.sqlDB.Exec(`DELETE FROM metrics WHERE agent_id = ? AND ts NOT IN (SELECT ts FROM metrics WHERE agent_id = ? ORDER BY ts DESC LIMIT ?)`, agentID, agentID, maxPoints)
	}
	return nil
}

// GetMetricHistory returns persisted metric history for agent.
func (s *Store) GetMetricHistory(agentID string) ([]models.MetricPoint, error) {
	if agentID == "" {
		return []models.MetricPoint{}, nil
	}
	if s.readFromSQLite && s.sqlDB != nil {
		rows, err := s.sqlDB.Query(`SELECT data FROM metrics WHERE agent_id=? ORDER BY ts ASC`, agentID)
		if err == nil {
			defer rows.Close()
			result := []models.MetricPoint{}
			for rows.Next() {
				var raw string
				if rows.Scan(&raw) != nil {
					continue
				}
				var p models.MetricPoint
				if json.Unmarshal([]byte(raw), &p) == nil {
					result = append(result, p)
				}
			}
			return result, nil
		}
	}
	result := []models.MetricPoint{}
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketMetrics))
		if b == nil {
			return nil
		}
		raw := b.Get([]byte(agentID))
		if raw == nil {
			return nil
		}
		return json.Unmarshal(raw, &result)
	})
	if err != nil {
		return []models.MetricPoint{}, err
	}
	return result, nil
}

// GetConfig returns the central config, or defaults if not set
func (s *Store) GetConfig() (*models.CentralConfig, error) {
	if s.readFromSQLite && s.sqlDB != nil {
		var raw string
		err := s.sqlDB.QueryRow(`SELECT data FROM config WHERE k=?`, KeyCentral).Scan(&raw)
		if err == nil {
			cfg := &models.CentralConfig{PollIntervalSec: 15, Port: "8080", Theme: "light", Language: "ru", RetentionDays: 30}
			_ = json.Unmarshal([]byte(raw), cfg)
			return cfg, nil
		}
	}
	cfg := &models.CentralConfig{PollIntervalSec: 15, Port: "8080", Theme: "light", Language: "ru", RetentionDays: 30}
	_ = s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketConfig))
		data := b.Get([]byte(KeyCentral))
		if data != nil {
			json.Unmarshal(data, cfg)
		}
		return nil
	})
	return cfg, nil
}

// SaveConfig persists the central config
func (s *Store) SaveConfig(cfg *models.CentralConfig) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	err = s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketConfig))
		return b.Put([]byte(KeyCentral), raw)
	})
	if err != nil {
		return err
	}
	if s.sqlDB != nil {
		_, _ = s.sqlDB.Exec(`INSERT INTO config(k, data) VALUES(?, ?) ON CONFLICT(k) DO UPDATE SET data=excluded.data`, KeyCentral, string(raw))
	}
	return nil
}

// UpdateAgentStatus updates the status and last seen timestamp
func (s *Store) UpdateAgentStatus(id, status string) error {
	agent, err := s.GetAgent(id)
	if err != nil {
		return err
	}
	agent.Status = status
	if status == "online" {
		agent.LastSeen = time.Now()
	}
	return s.SaveAgent(agent)
}
