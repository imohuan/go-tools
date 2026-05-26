package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
)

const defaultPort = 17890

type Profile struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Processes []string `json:"processes"`
}

type Config struct {
	Port            int       `json:"port"`
	Profiles        []Profile `json:"profiles"`
	ActiveProfileID string    `json:"activeProfileId"`
}

type Store struct {
	mu   sync.RWMutex
	path string
	cfg  Config
}

func Default() Config {
	id := uuid.New().String()
	return Config{
		Port: defaultPort,
		Profiles: []Profile{
			{
				ID:        id,
				Name:      "默认配置",
				Processes: []string{},
			},
		},
		ActiveProfileID: id,
	}
}

func NewStore(path string) (*Store, error) {
	s := &Store{path: path, cfg: Default()}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if saveErr := s.saveUnlocked(); saveErr != nil {
				return nil, saveErr
			}
			return s, nil
		}
		return nil, err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &s.cfg); err != nil {
			return nil, err
		}
	}
	if len(s.cfg.Profiles) == 0 {
		s.cfg = Default()
		if err := s.saveUnlocked(); err != nil {
			return nil, err
		}
	}
	if s.cfg.Port == 0 {
		s.cfg.Port = defaultPort
	}
	return s, nil
}

func (s *Store) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *Store) Replace(cfg Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cfg.Port == 0 {
		cfg.Port = defaultPort
	}
	s.cfg = cfg
	return s.saveUnlocked()
}

func (s *Store) ProfileByID(id string) (Profile, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.cfg.Profiles {
		if p.ID == id {
			return p, true
		}
	}
	return Profile{}, false
}

func (s *Store) saveUnlocked() error {
	data, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func ConfigPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(exe)
	return filepath.Join(dir, "kills-config.json"), nil
}
