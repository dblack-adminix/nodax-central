package store

import (
	"encoding/json"
	"fmt"
	"nodax-central/internal/models"
	"time"

	"go.etcd.io/bbolt"
	"golang.org/x/crypto/bcrypt"
)

func (s *Store) SaveUser(user *models.User) error {
	raw, err := json.Marshal(user)
	if err != nil {
		return err
	}
	err = s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketUsers))
		return b.Put([]byte(user.ID), raw)
	})
	if err != nil {
		return err
	}
	if s.sqlDB != nil {
		_, _ = s.sqlDB.Exec(`INSERT INTO users(id, username, password, role, created_at, data) VALUES(?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET username=excluded.username, password=excluded.password, role=excluded.role, created_at=excluded.created_at, data=excluded.data`, user.ID, user.Username, user.Password, user.Role, user.CreatedAt.UTC().Format(time.RFC3339Nano), string(raw))
	}
	return nil
}

func (s *Store) GetUserByID(id string) (*models.User, error) {
	if s.readFromSQLite && s.sqlDB != nil {
		var raw string
		err := s.sqlDB.QueryRow(`SELECT data FROM users WHERE id=?`, id).Scan(&raw)
		if err == nil {
			var u models.User
			if json.Unmarshal([]byte(raw), &u) == nil {
				return &u, nil
			}
		}
	}
	var u models.User
	err := s.db.View(func(tx *bbolt.Tx) error {
		data := tx.Bucket([]byte(BucketUsers)).Get([]byte(id))
		if data == nil {
			return fmt.Errorf("not found")
		}
		return json.Unmarshal(data, &u)
	})
	return &u, err
}

func (s *Store) GetUserByUsername(username string) (*models.User, error) {
	if s.readFromSQLite && s.sqlDB != nil {
		var raw string
		err := s.sqlDB.QueryRow(`SELECT data FROM users WHERE username=? LIMIT 1`, username).Scan(&raw)
		if err == nil {
			var u models.User
			if json.Unmarshal([]byte(raw), &u) == nil {
				return &u, nil
			}
		}
	}
	var found *models.User
	s.db.View(func(tx *bbolt.Tx) error {
		tx.Bucket([]byte(BucketUsers)).ForEach(func(k, v []byte) error {
			var u models.User
			if json.Unmarshal(v, &u) == nil && u.Username == username {
				found = &u
			}
			return nil
		})
		return nil
	})
	if found == nil {
		return nil, fmt.Errorf("user not found")
	}
	return found, nil
}

func (s *Store) GetAllUsers() ([]models.User, error) {
	if s.readFromSQLite && s.sqlDB != nil {
		rows, err := s.sqlDB.Query(`SELECT data FROM users ORDER BY created_at ASC`)
		if err == nil {
			defer rows.Close()
			users := []models.User{}
			for rows.Next() {
				var raw string
				if rows.Scan(&raw) != nil {
					continue
				}
				var u models.User
				if json.Unmarshal([]byte(raw), &u) == nil {
					users = append(users, u)
				}
			}
			return users, nil
		}
	}
	var users []models.User
	s.db.View(func(tx *bbolt.Tx) error {
		tx.Bucket([]byte(BucketUsers)).ForEach(func(k, v []byte) error {
			var u models.User
			if json.Unmarshal(v, &u) == nil {
				users = append(users, u)
			}
			return nil
		})
		return nil
	})
	return users, nil
}

func (s *Store) DeleteUser(id string) error {
	err := s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket([]byte(BucketUsers)).Delete([]byte(id))
	})
	if err != nil {
		return err
	}
	if s.sqlDB != nil {
		_, _ = s.sqlDB.Exec(`DELETE FROM users WHERE id=?`, id)
	}
	return nil
}

func (s *Store) UserCount() int {
	if s.readFromSQLite && s.sqlDB != nil {
		var n int
		err := s.sqlDB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
		if err == nil {
			return n
		}
	}
	count := 0
	s.db.View(func(tx *bbolt.Tx) error {
		count = tx.Bucket([]byte(BucketUsers)).Stats().KeyN
		return nil
	})
	return count
}

func (s *Store) CreateUser(username, password, role string, hostPermissions []models.UserHostPermission) (*models.User, error) {
	if _, err := s.GetUserByUsername(username); err == nil {
		return nil, fmt.Errorf("user already exists")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	u := &models.User{
		ID:              fmt.Sprintf("u_%d", time.Now().UnixNano()),
		Username:        username,
		Password:        string(hash),
		Role:            role,
		HostPermissions: hostPermissions,
		CreatedAt:       time.Now(),
	}
	return u, s.SaveUser(u)
}

func (s *Store) CheckPassword(user *models.User, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)) == nil
}
