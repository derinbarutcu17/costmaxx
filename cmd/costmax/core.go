package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/derinbarutcu17/costmaxx/internal/adapters/codex"
	"github.com/derinbarutcu17/costmaxx/internal/artifacts"
	"github.com/derinbarutcu17/costmaxx/internal/config"
	"github.com/derinbarutcu17/costmaxx/internal/mcp"
	"github.com/derinbarutcu17/costmaxx/internal/store"
)

var cfg *config.Config
var artStore *artifacts.Store
var db *store.DB
var adapter *codex.Adapter

var mcpServer *mcp.Server
var mcpSpecFraming bool

func initCore() error {
	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, ".costmax")

	cfgPath := filepath.Join(dataDir, "config.toml")
	var err error
	cfg, err = config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	dirs := []string{cfg.Core.DataDir, cfg.Core.LogDir, cfg.Store.ArtifactDir}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0700); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}

	db, err = store.Open(cfg.Store.DBPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}

	artStore, err = artifacts.NewStore(cfg.Store.ArtifactDir, cfg.Store.MaxArtifactSize)
	if err != nil {
		return fmt.Errorf("artifact store: %w", err)
	}

	adapter = codex.New(cfg, artStore, db)
	return nil
}
