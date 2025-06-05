package configs

import (
	"strings"
	"time"

	"github.com/chenxilol/gohub/pkg/bus/nats"
	"github.com/chenxilol/gohub/pkg/bus/redis"
	"github.com/chenxilol/gohub/pkg/hub"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"

	"log/slog"
)

type Server struct {
	Addr string     `mapstructure:"addr"`
	Hub  hub.Config `mapstructure:"hub"`
}

type Cluster struct {
	Enabled bool         `mapstructure:"enabled"`
	BusType string       `mapstructure:"bus_type"` // 消息总线类型: "nats", "redis", "noop"
	NATS    nats.Config  `mapstructure:"nats"`
	Redis   redis.Config `mapstructure:"redis"`
}

type Auth struct {
	Enabled        bool   `mapstructure:"enabled"`
	SecretKey      string `mapstructure:"secret_key"`
	Issuer         string `mapstructure:"issuer"`
	AllowAnonymous bool   `mapstructure:"allow_anonymous"`
}

type Log struct {
	Level string `mapstructure:"level"`
}

type Config struct {
	Server  `mapstructure:"server"`
	Cluster `mapstructure:"cluster"`
	Auth    `mapstructure:"auth"`
	Log     `mapstructure:"log"`
	Version string `mapstructure:"version"`
}

// NewDefaultConfig creates a new Config with default values
func NewDefaultConfig() Config {
	config := Config{}

	// 服务器默认配置
	config.Server.Addr = ":8080"
	config.Server.Hub.ReadTimeout = 60 * time.Second
	config.Server.Hub.WriteTimeout = 60 * time.Second
	config.Server.Hub.ReadBufferSize = 4 * 1024
	config.Server.Hub.WriteBufferSize = 4 * 1024
	config.Server.Hub.MessageBufferCap = 256

	// 集群默认配置
	config.Cluster.Enabled = true
	config.Cluster.BusType = "nats" // 默认使用 NATS

	// NATS默认配置
	config.Cluster.NATS.URLs = []string{"nats://localhost:4222"}
	config.Cluster.NATS.Name = "gohub-client"
	config.Cluster.NATS.ReconnectWait = 2 * time.Second
	config.Cluster.NATS.MaxReconnects = -1
	config.Cluster.NATS.ConnectTimeout = 5 * time.Second
	config.Cluster.NATS.OpTimeout = 10 * time.Millisecond
	config.Cluster.NATS.UseJetStream = false
	config.Cluster.NATS.StreamName = "GOHUB"
	config.Cluster.NATS.ConsumerName = "gohub-consumer"
	config.Cluster.NATS.MessageRetention = 1 * time.Hour

	// Redis默认配置
	config.Cluster.Redis = redis.DefaultConfig()

	// 认证默认配置
	config.Auth.Enabled = false
	config.Auth.SecretKey = "changeme"
	config.Auth.Issuer = "gohub"
	config.Auth.AllowAnonymous = true

	// 日志默认配置
	config.Log.Level = "info"

	// 版本默认配置
	config.Version = "dev"

	return config
}

// LoadConfig loads configuration from the specified file
func LoadConfig(configFile string) (Config, error) {
	v := viper.New()
	v.SetConfigFile(configFile)

	// 支持环境变量
	v.SetEnvPrefix("GOHUB")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		slog.Error("Failed to read config file, using default config", "error", err)
		return NewDefaultConfig(), nil
	}

	var config Config
	if err := v.Unmarshal(&config); err != nil {
		slog.Error("Failed to unmarshal config, using default config", "error", err)
		return NewDefaultConfig(), nil
	}

	// 设置配置文件热更新
	SetupConfigHotReload(v, &config)

	return config, nil
}

// SetupConfigHotReload sets up hot reload for the configuration file
func SetupConfigHotReload(v *viper.Viper, config *Config) {
	v.WatchConfig()
	v.OnConfigChange(func(e fsnotify.Event) {
		slog.Info("Config file changed")

		// 重新解析配置
		if err := v.Unmarshal(config); err != nil {
			slog.Error("Failed to unmarshal updated config", "error", err)
			return
		}

		slog.Info("Config reloaded successfully")
	})
}

// ParseLogLevel parses a string log level to slog.Level
func ParseLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
