package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Core   CoreConfig   `toml:"core"`
	Store  StoreConfig  `toml:"store"`
	Reduce ReduceConfig `toml:"reduce"`
	Mode   string       `toml:"mode"`
}

type CoreConfig struct {
	DataDir    string `toml:"data_dir"`
	LogDir     string `toml:"log_dir"`
	MaxLogSize int64  `toml:"max_log_size"`
}

type StoreConfig struct {
	DBPath          string `toml:"db_path"`
	ArtifactDir     string `toml:"artifact_dir"`
	MaxArtifactSize int64  `toml:"max_artifact_size"`
	RetentionDays   int    `toml:"retention_days"`
	ZstdLevel       int    `toml:"zstd_level"`
}

type ReduceConfig struct {
	Threshold   int                `toml:"threshold"`
	MaxCompact  int                `toml:"max_compact"`
	TokenBudget int                `toml:"token_budget"`
	Confidence  map[string]float64 `toml:"confidence"`
}

func Default() *Config {
	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, ".costmax")
	return &Config{
		Mode: "observe",
		Core: CoreConfig{
			DataDir:    dataDir,
			LogDir:     filepath.Join(dataDir, "logs"),
			MaxLogSize: 10 << 20,
		},
		Store: StoreConfig{
			DBPath:          filepath.Join(dataDir, "costmax.db"),
			ArtifactDir:     filepath.Join(dataDir, "artifacts"),
			MaxArtifactSize: 50 << 20,
			RetentionDays:   14,
			ZstdLevel:       3,
		},
		Reduce: ReduceConfig{
			Threshold:   4000,
			MaxCompact:  2000,
			TokenBudget: 4000,
			Confidence: map[string]float64{
				"test":     0.9,
				"build":    0.85,
				"diff":     0.8,
				"search":   0.8,
				"lint":     0.85,
				"terminal": 0.7,
				"json":     0.9,
				"generic":  0.6,
			},
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

func (c *Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	data, err := toml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}
