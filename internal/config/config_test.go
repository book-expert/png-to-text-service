package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfig_Load(t *testing.T) {
	tests := []struct {
		validate func(*testing.T, *Config)
		name     string
		content  string
		wantErr  bool
	}{
		{
			name: "valid config with all settings",
			content: `[project]
name = "test-service"
version = "1.0.0"

[paths]
input_dir = "./input"
output_dir = "./output"

[settings]
workers = 8
enable_augmentation = true

[tesseract]
language = "eng+equ"
oem = 3
psm = 3
dpi = 300

[gemini]
api_key_variable = "TEST_API_KEY"
models = ["gemini-2.5-flash-lite"]

[logging]
level = "info"
dir = "./logs"
`,
			wantErr: false,
			validate: func(t *testing.T, cfg *Config) {
				if cfg.Project.Name != "test-service" {
					t.Errorf(
						"Project.Name = %s, want test-service",
						cfg.Project.Name,
					)
				}
				if cfg.Settings.Workers != 8 {
					t.Errorf(
						"Settings.Workers = %d, want 8",
						cfg.Settings.Workers,
					)
				}
				if !cfg.Settings.EnableAugmentation {
					t.Error(
						"Settings.EnableAugmentation = false, want true",
					)
				}
			},
		},
		{
			name: "missing required paths",
			content: `[project]
name = "test-service"`,
			wantErr: true,
		},
		{
			name: "invalid TOML",
			content: `[project
name = "invalid"`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary config file
			tmpDir, err := os.MkdirTemp("", "config-test")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			configPath := filepath.Join(tmpDir, "project.toml")
			if err := os.WriteFile(configPath, []byte(tt.content), 0o644); err != nil {
				t.Fatalf("Failed to write config file: %v", err)
			}

			cfg, err := Load(configPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("Load() error = %v, wantErr %v", err, tt.wantErr)

				return
			}

			if !tt.wantErr && tt.validate != nil {
				tt.validate(t, cfg)
			}
		})
	}
}

func TestConfig_validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				Paths: struct {
					InputDir  string `toml:"input_dir"`
					OutputDir string `toml:"output_dir"`
				}{
					InputDir:  "./input",
					OutputDir: "./output",
				},
			},
			wantErr: false,
		},
		{
			name: "missing input dir",
			config: Config{
				Paths: struct {
					InputDir  string `toml:"input_dir"`
					OutputDir string `toml:"output_dir"`
				}{
					OutputDir: "./output",
				},
			},
			wantErr: true,
		},
		{
			name: "missing output dir",
			config: Config{
				Paths: struct {
					InputDir  string `toml:"input_dir"`
					OutputDir string `toml:"output_dir"`
				}{
					InputDir: "./input",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.validate()
			if (err != nil) != tt.wantErr {
				t.Errorf(
					"Config.validate() error = %v, wantErr %v",
					err,
					tt.wantErr,
				)
			}
		})
	}
}

func TestConfig_GetAPIKey(t *testing.T) {
	tests := []struct {
		name   string
		envKey string
		envVal string
		want   string
		config Config
	}{
		{
			name: "valid API key",
			config: Config{
				Gemini: struct {
					APIKeyVariable    string   `toml:"api_key_variable"`
					Models            []string `toml:"models"`
					MaxRetries        int      `toml:"max_retries"`
					RetryDelaySeconds int      `toml:"retry_delay_seconds"`
					TimeoutSeconds    int      `toml:"timeout_seconds"`
					Temperature       float64  `toml:"temperature"`
					TopK              int      `toml:"top_k"`
					TopP              float64  `toml:"top_p"`
					MaxTokens         int      `toml:"max_tokens"`
				}{
					APIKeyVariable: "TEST_API_KEY",
				},
			},
			envKey: "TEST_API_KEY",
			envVal: "secret-key-123",
			want:   "secret-key-123",
		},
		{
			name: "empty variable name",
			config: Config{
				Gemini: struct {
					APIKeyVariable    string   `toml:"api_key_variable"`
					Models            []string `toml:"models"`
					MaxRetries        int      `toml:"max_retries"`
					RetryDelaySeconds int      `toml:"retry_delay_seconds"`
					TimeoutSeconds    int      `toml:"timeout_seconds"`
					Temperature       float64  `toml:"temperature"`
					TopK              int      `toml:"top_k"`
					TopP              float64  `toml:"top_p"`
					MaxTokens         int      `toml:"max_tokens"`
				}{},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variable if needed
			if tt.envKey != "" && tt.envVal != "" {
				oldVal := os.Getenv(tt.envKey)
				os.Setenv(tt.envKey, tt.envVal)

				defer func() {
					if oldVal == "" {
						os.Unsetenv(tt.envKey)
					} else {
						os.Setenv(tt.envKey, oldVal)
					}
				}()
			}

			if got := tt.config.GetAPIKey(); got != tt.want {
				t.Errorf("Config.GetAPIKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfig_EnsureDirectories(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-ensure-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &Config{
		Paths: struct {
			InputDir  string `toml:"input_dir"`
			OutputDir string `toml:"output_dir"`
		}{
			InputDir:  filepath.Join(tmpDir, "input"),
			OutputDir: filepath.Join(tmpDir, "output"),
		},
		Logging: struct {
			Level                string `toml:"level"`
			Dir                  string `toml:"dir"`
			EnableFileLogging    bool   `toml:"enable_file_logging"`
			EnableConsoleLogging bool   `toml:"enable_console_logging"`
		}{
			Dir: filepath.Join(tmpDir, "logs"),
		},
	}

	if err := cfg.EnsureDirectories(); err != nil {
		t.Errorf("Config.EnsureDirectories() error = %v", err)
	}

	// Check that directories were created
	dirs := []string{cfg.Paths.InputDir, cfg.Paths.OutputDir, cfg.Logging.Dir}
	for _, dir := range dirs {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("Directory %s was not created: %v", dir, err)
		}
	}
}

func TestConfig_GetLogFilePath(t *testing.T) {
	cfg := &Config{
		Logging: struct {
			Level                string `toml:"level"`
			Dir                  string `toml:"dir"`
			EnableFileLogging    bool   `toml:"enable_file_logging"`
			EnableConsoleLogging bool   `toml:"enable_console_logging"`
		}{
			Dir: "/tmp/logs",
		},
	}

	filename := "test.log"
	expected := "/tmp/logs/test.log"

	if got := cfg.GetLogFilePath(filename); got != expected {
		t.Errorf("Config.GetLogFilePath() = %v, want %v", got, expected)
	}
}

func BenchmarkConfig_validate(b *testing.B) {
	cfg := Config{
		Paths: struct {
			InputDir  string `toml:"input_dir"`
			OutputDir string `toml:"output_dir"`
		}{
			InputDir:  "./input",
			OutputDir: "./output",
		},
		Settings: struct {
			Workers            int  `toml:"workers"`
			TimeoutSeconds     int  `toml:"timeout_seconds"`
			EnableAugmentation bool `toml:"enable_augmentation"`
			SkipExisting       bool `toml:"skip_existing"`
		}{
			EnableAugmentation: true,
		},
		Gemini: struct {
			APIKeyVariable    string   `toml:"api_key_variable"`
			Models            []string `toml:"models"`
			MaxRetries        int      `toml:"max_retries"`
			RetryDelaySeconds int      `toml:"retry_delay_seconds"`
			TimeoutSeconds    int      `toml:"timeout_seconds"`
			Temperature       float64  `toml:"temperature"`
			TopK              int      `toml:"top_k"`
			TopP              float64  `toml:"top_p"`
			MaxTokens         int      `toml:"max_tokens"`
		}{
			APIKeyVariable: "TEST_API_KEY",
		},
	}

	b.ResetTimer()

	for range b.N {
		_ = cfg.validate()
	}
}
