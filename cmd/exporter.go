package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	obscollector "github.com/chaspy/agentctl/internal/observability/collector"
	obsexporter "github.com/chaspy/agentctl/internal/observability/exporter"
	obsmcp "github.com/chaspy/agentctl/internal/observability/mcp"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const exporterCollectionTimeout = 30 * time.Second

type exporterFileConfig struct {
	AgentctlDB        string `yaml:"agentctl_db"`
	CCUsageCache      string `yaml:"ccusage_cache"`
	CodexDB           string `yaml:"codex_db"`
	ClaudeProjectsDir string `yaml:"claude_projects_dir"`
	PrometheusPort    int    `yaml:"prometheus_port"`
	MCPPort           int    `yaml:"mcp_port"`
	CollectInterval   string `yaml:"collect_interval"`
}

type exporterConfig struct {
	AgentctlDB        string
	CCUsageCache      string
	CodexDB           string
	ClaudeProjectsDir string
	PrometheusPort    int
	MCPPort           int
	CollectInterval   time.Duration
}

type exporterCollectorRunner interface {
	Collect(ctx context.Context) error
}

var exporterConfigPath string

var exporterCmd = &cobra.Command{
	Use:           "exporter",
	Short:         "Run the integrated observability exporter",
	SilenceUsage:  true,
	SilenceErrors: true,
	Long: `Run the Prometheus and MCP observability surfaces that previously lived
in the standalone agent-exporter repository.

The exporter reads agentctl state, Codex thread usage, and Claude JSONL logs,
then exposes:

  - Prometheus metrics on /metrics
  - MCP HTTP tools on /mcp
  - a simple health endpoint on /health`,
	Example: `  agentctl exporter
  agentctl exporter --config /path/to/exporter.yaml`,
	RunE: runExporter,
}

func init() {
	rootCmd.AddCommand(exporterCmd)
	exporterCmd.Flags().StringVar(&exporterConfigPath, "config", "config.yaml", "Path to the exporter configuration file")
}

func runExporter(cmd *cobra.Command, _ []string) error {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := loadExporterConfig(exporterConfigPath)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	registry := obsexporter.NewRegistry()
	agentctlCollector, err := obscollector.NewAgentctlCollector(registry, cfg.AgentctlDB, cfg.CCUsageCache)
	if err != nil {
		return fmt.Errorf("initialize agentctl collector: %w", err)
	}

	codexCollector, err := obscollector.NewCodexCollector(registry, cfg.CodexDB)
	if err != nil {
		return fmt.Errorf("initialize codex collector: %w", err)
	}

	claudeCollector, err := obscollector.NewClaudeCollector(registry, cfg.ClaudeProjectsDir)
	if err != nil {
		return fmt.Errorf("initialize claude collector: %w", err)
	}

	collectors := []exporterCollectorRunner{
		agentctlCollector,
		codexCollector,
		claudeCollector,
	}

	if err := collectExporterOnce(ctx, collectors...); err != nil {
		logger.Warn("initial collection failed", "error", err)
	}

	metricsServer := obsexporter.NewServer(cfg.PrometheusPort, registry)
	mcpServer := obsmcp.NewServer(cfg.MCPPort, cfg.AgentctlDB, cfg.CodexDB)
	serverErrCh := make(chan error, 2)

	go func() {
		logger.Info(
			"starting integrated exporter",
			"metrics_addr", metricsServer.Addr,
			"mcp_addr", mcpServer.Addr,
			"agentctl_db", cfg.AgentctlDB,
			"ccusage_cache", cfg.CCUsageCache,
			"codex_db", cfg.CodexDB,
			"claude_projects_dir", cfg.ClaudeProjectsDir,
			"collect_interval", cfg.CollectInterval.String(),
		)

		if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrCh <- fmt.Errorf("metrics server failed: %w", err)
		}
	}()

	go func() {
		if err := mcpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrCh <- fmt.Errorf("mcp server failed: %w", err)
		}
	}()

	ticker := time.NewTicker(cfg.CollectInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return shutdownExporterServers(metricsServer, mcpServer, logger)
		case <-ticker.C:
			if err := collectExporterOnce(ctx, collectors...); err != nil {
				logger.Warn("periodic collection failed", "error", err)
			}
		case err := <-serverErrCh:
			stop()
			shutdownErr := shutdownExporterServers(metricsServer, mcpServer, logger)
			return errors.Join(err, shutdownErr)
		}
	}
}

func shutdownExporterServers(metricsServer, mcpServer *http.Server, logger *slog.Logger) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var shutdownErrors []error
	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		shutdownErrors = append(shutdownErrors, fmt.Errorf("shutdown metrics server: %w", err))
	}
	if err := mcpServer.Shutdown(shutdownCtx); err != nil {
		shutdownErrors = append(shutdownErrors, fmt.Errorf("shutdown mcp server: %w", err))
	}
	if err := errors.Join(shutdownErrors...); err != nil {
		return err
	}

	logger.Info("integrated exporter stopped")
	return nil
}

func collectExporterOnce(parent context.Context, collectors ...exporterCollectorRunner) error {
	ctx, cancel := context.WithTimeout(parent, exporterCollectionTimeout)
	defer cancel()

	var collectErrors []error
	for _, collector := range collectors {
		if err := collector.Collect(ctx); err != nil {
			collectErrors = append(collectErrors, err)
		}
	}

	return errors.Join(collectErrors...)
}

func loadExporterConfig(path string) (exporterConfig, error) {
	raw := exporterFileConfig{
		AgentctlDB:        "${HOME}/.agentctl/manager.db",
		CCUsageCache:      "${HOME}/.agentctl/ccusage-cache.json",
		CodexDB:           "${HOME}/.codex/state_5.sqlite",
		ClaudeProjectsDir: "${HOME}/.claude/projects",
		PrometheusPort:    9100,
		MCPPort:           9101,
		CollectInterval:   "5m",
	}

	content, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return exporterConfig{}, fmt.Errorf("read config %q: %w", path, err)
		}
	} else if err := yaml.Unmarshal(content, &raw); err != nil {
		return exporterConfig{}, fmt.Errorf("parse config %q: %w", path, err)
	}

	agentctlDB, err := expandExporterPath(raw.AgentctlDB)
	if err != nil {
		return exporterConfig{}, fmt.Errorf("resolve agentctl_db: %w", err)
	}

	ccusageCache, err := expandExporterPath(raw.CCUsageCache)
	if err != nil {
		return exporterConfig{}, fmt.Errorf("resolve ccusage_cache: %w", err)
	}

	codexDB, err := expandExporterPath(raw.CodexDB)
	if err != nil {
		return exporterConfig{}, fmt.Errorf("resolve codex_db: %w", err)
	}

	claudeProjectsDir, err := expandExporterPath(raw.ClaudeProjectsDir)
	if err != nil {
		return exporterConfig{}, fmt.Errorf("resolve claude_projects_dir: %w", err)
	}

	interval, err := time.ParseDuration(raw.CollectInterval)
	if err != nil {
		return exporterConfig{}, fmt.Errorf("parse collect_interval: %w", err)
	}
	if interval <= 0 {
		return exporterConfig{}, fmt.Errorf("collect_interval must be positive")
	}
	if raw.PrometheusPort <= 0 || raw.PrometheusPort > 65535 {
		return exporterConfig{}, fmt.Errorf("prometheus_port must be between 1 and 65535")
	}
	if raw.MCPPort <= 0 || raw.MCPPort > 65535 {
		return exporterConfig{}, fmt.Errorf("mcp_port must be between 1 and 65535")
	}

	return exporterConfig{
		AgentctlDB:        agentctlDB,
		CCUsageCache:      ccusageCache,
		CodexDB:           codexDB,
		ClaudeProjectsDir: claudeProjectsDir,
		PrometheusPort:    raw.PrometheusPort,
		MCPPort:           raw.MCPPort,
		CollectInterval:   interval,
	}, nil
}

func expandExporterPath(path string) (string, error) {
	expanded := os.ExpandEnv(path)
	if expanded == "~" || strings.HasPrefix(expanded, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		if expanded == "~" {
			return home, nil
		}
		return home + expanded[1:], nil
	}
	return expanded, nil
}
