package store

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"nodax-central/internal/models"
	"time"

	"go.etcd.io/bbolt"
)

// SaveLogs stores log entries with deduplication by hash
func (s *Store) SaveLogs(logs []models.CentralLog) (int, error) {
	saved := 0
	err := s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketLogs))
		for _, log := range logs {
			if log.ID == "" {
				h := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%s|%s",
					log.AgentID, log.Timestamp.Format(time.RFC3339Nano), log.Type, log.TargetVM, log.Message)))
				log.ID = fmt.Sprintf("%x", h[:12])
			}
			data, err := json.Marshal(log)
			if err != nil {
				continue
			}
			// Key: timestamp_nano + id for ordering
			key := fmt.Sprintf("%020d_%s", log.Timestamp.UnixNano(), log.ID)
			if existing := b.Get([]byte(key)); existing != nil {
				continue
			}
			if err := b.Put([]byte(key), data); err != nil {
				return err
			}
			saved++
		}
		return nil
	})
	if err != nil {
		return saved, err
	}

	if s.sqlDB != nil {
		for _, log := range logs {
			if log.ID == "" {
				h := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%s|%s",
					log.AgentID, log.Timestamp.Format(time.RFC3339Nano), log.Type, log.TargetVM, log.Message)))
				log.ID = fmt.Sprintf("%x", h[:12])
			}
			raw, err := json.Marshal(log)
			if err != nil {
				continue
			}
			res, e := s.sqlDB.Exec(`INSERT OR IGNORE INTO logs(id, ts, agent_id, agent_name, type, status, target_vm, data) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
				log.ID,
				log.Timestamp.UTC().Format(time.RFC3339Nano),
				log.AgentID,
				log.AgentName,
				log.Type,
				log.Status,
				log.TargetVM,
				string(raw),
			)
			if e == nil {
				if n, _ := res.RowsAffected(); n > 0 {
					saved++
				}
			}
		}
	}

	return saved, nil
}

// QueryLogs returns logs matching filters, ordered by timestamp desc
func (s *Store) QueryLogs(agentID, logType, status string, from, to time.Time, limit int) ([]models.CentralLog, error) {
	var results []models.CentralLog
	if limit <= 0 {
		limit = 200
	}

	if s.readFromSQLite && s.sqlDB != nil {
		q := `SELECT data FROM logs WHERE 1=1`
		args := []any{}
		if agentID != "" {
			q += ` AND agent_id = ?`
			args = append(args, agentID)
		}
		if logType != "" {
			q += ` AND type = ?`
			args = append(args, logType)
		}
		if status != "" {
			q += ` AND status = ?`
			args = append(args, status)
		}
		if !from.IsZero() {
			q += ` AND ts >= ?`
			args = append(args, from.UTC().Format(time.RFC3339Nano))
		}
		if !to.IsZero() {
			q += ` AND ts <= ?`
			args = append(args, to.UTC().Format(time.RFC3339Nano))
		}
		q += ` ORDER BY ts DESC LIMIT ?`
		args = append(args, limit)
		rows, err := s.sqlDB.Query(q, args...)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var raw string
				if rows.Scan(&raw) != nil {
					continue
				}
				var log models.CentralLog
				if json.Unmarshal([]byte(raw), &log) == nil {
					results = append(results, log)
				}
			}
			return results, nil
		}
	}

	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketLogs))
		c := b.Cursor()

		// If we have a time range, seek to the right position
		var startKey, endKey []byte
		if !from.IsZero() {
			startKey = []byte(fmt.Sprintf("%020d", from.UnixNano()))
		}
		if !to.IsZero() {
			endKey = []byte(fmt.Sprintf("%020d", to.UnixNano()+1))
		}

		// Iterate in reverse (newest first)
		var k, v []byte
		if endKey != nil {
			k, v = c.Seek(endKey)
			if k == nil {
				k, v = c.Last()
			} else {
				k, v = c.Prev()
			}
		} else {
			k, v = c.Last()
		}

		for ; k != nil && len(results) < limit; k, v = c.Prev() {
			if startKey != nil && string(k) < string(startKey) {
				break
			}
			var log models.CentralLog
			if err := json.Unmarshal(v, &log); err != nil {
				continue
			}
			if agentID != "" && log.AgentID != agentID {
				continue
			}
			if logType != "" && log.Type != logType {
				continue
			}
			if status != "" && log.Status != status {
				continue
			}
			results = append(results, log)
		}
		return nil
	})
	return results, err
}

// PurgeLogs removes logs older than the given duration
func (s *Store) PurgeLogs(olderThan time.Duration) (int, error) {
	cutoff := time.Now().Add(-olderThan)
	cutoffKey := []byte(fmt.Sprintf("%020d", cutoff.UnixNano()))
	deleted := 0

	err := s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketLogs))
		c := b.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			if string(k) >= string(cutoffKey) {
				break
			}
			if err := b.Delete(k); err != nil {
				return err
			}
			deleted++
		}
		return nil
	})
	if err != nil {
		return deleted, err
	}

	if s.sqlDB != nil {
		_, _ = s.sqlDB.Exec(`DELETE FROM logs WHERE ts < ?`, cutoff.UTC().Format(time.RFC3339Nano))
	}

	return deleted, nil
}

// LogCount returns total number of stored logs
func (s *Store) LogCount() int {
	if s.readFromSQLite && s.sqlDB != nil {
		var n int
		err := s.sqlDB.QueryRow(`SELECT COUNT(*) FROM logs`).Scan(&n)
		if err == nil {
			return n
		}
	}
	count := 0
	s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketLogs))
		count = b.Stats().KeyN
		return nil
	})
	return count
}

// GetLogLabels returns unique values for a given label across all logs
func (s *Store) GetLogLabels(label string) ([]string, error) {
	seen := make(map[string]bool)

	if s.readFromSQLite && s.sqlDB != nil {
		col := ""
		switch label {
		case "agent":
			col = "agent_name"
		case "agentId":
			col = "agent_id"
		case "type":
			col = "type"
		case "status":
			col = "status"
		case "vm":
			col = "target_vm"
		}
		if col != "" {
			rows, err := s.sqlDB.Query(`SELECT DISTINCT ` + col + ` FROM logs WHERE ` + col + ` IS NOT NULL AND ` + col + ` != ''`)
			if err == nil {
				defer rows.Close()
				result := []string{}
				for rows.Next() {
					var v string
					if rows.Scan(&v) == nil && v != "" {
						result = append(result, v)
					}
				}
				return result, nil
			}
		}
	}

	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketLogs))
		return b.ForEach(func(k, v []byte) error {
			var log models.CentralLog
			if err := json.Unmarshal(v, &log); err != nil {
				return nil
			}
			switch label {
			case "agent":
				if log.AgentName != "" {
					seen[log.AgentName] = true
				}
			case "agentId":
				if log.AgentID != "" {
					seen[log.AgentID] = true
				}
			case "type":
				if log.Type != "" {
					seen[log.Type] = true
				}
			case "status":
				if log.Status != "" {
					seen[log.Status] = true
				}
			case "vm":
				if log.TargetVM != "" {
					seen[log.TargetVM] = true
				}
			}
			return nil
		})
	})
	result := make([]string, 0, len(seen))
	for k := range seen {
		result = append(result, k)
	}
	return result, err
}
