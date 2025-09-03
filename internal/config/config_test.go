package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nnikolov3/png-to-text-service/internal/config"
)

// testHelper provides common test utilities and reduces code duplication.
type testHelper struct {
	t *testing.T
}

// newTestHelper creates a new test helper instance.
func newTestHelper(t *testing.T) *testHelper {
	t.Helper()

	return &testHelper{t: t}
}

// createTempDir creates a temporary directory for testing.
func (h *testHelper) createTempDir(prefix string) (string, func()) {
	tmpDir, err := os.MkdirTemp("", prefix)
	if err != nil {
		h.t.Fatalf("Failed to create temp dir: %v", err)
	}

	cleanup := func() {
		removeErr := os.RemoveAll(tmpDir)
		if removeErr != nil {
			h.t.Logf("Failed to remove temp dir: %v", removeErr)
		}
	}

	return tmpDir, cleanup
}

// writeConfigFile writes content to a config file in the given directory.
func (h *testHelper) writeConfigFile(dir, content string) string {
	configPath := filepath.Join(dir, "project.toml")

	err := os.WriteFile(configPath, []byte(content), 0o600)
	if err != nil {
		h.t.Fatalf("Failed to write config file: %v", err)
	}

	return configPath
}

// validateDirectoryExists checks that a directory exists.
func (h *testHelper) validateDirectoryExists(dir string) {
	if _, err := os.Stat(dir); err != nil {
		h.t.Errorf("Directory %s was not created: %v", dir, err)
	}
}

// envTestHelper provides environment variable testing utilities.
type envTestHelper struct {
	t *testing.T
}

// newEnvTestHelper creates a new environment test helper.
func newEnvTestHelper(t *testing.T) *envTestHelper {
	t.Helper()

	return &envTestHelper{t: t}
}

// withEnvVar sets an environment variable for the duration of the test function.
func (h *envTestHelper) withEnvVar(key, value string, testFunc func()) {
	oldVal := os.Getenv(key)

	err := os.Setenv(key, value)
	if err != nil {
		h.t.Fatalf("Failed to set env var: %v", err)
	}

	defer func() {
		if oldVal == "" {
			err := os.Unsetenv(key)
			if err != nil {
				h.t.Logf("Failed to unset env var: %v", err)
			}
		} else {
			err := os.Setenv(key, oldVal)
			if err != nil {
				h.t.Logf("Failed to restore env var: %v", err)
			}
		}
	}()

	testFunc()
}

// configTestCase represents a test case for config loading.
type configTestCase struct {
	validate func(*testing.T, *config.Config)
	name     string
	content  string
	wantErr  bool
}

// newConfigTestCase creates a new config test case.
func newConfigTestCase(
	name, content string,
	wantErr bool,
	validate func(*testing.T, *config.Config),
) configTestCase {
	return configTestCase{
		name:     name,
		content:  content,
		wantErr:  wantErr,
		validate: validate,
	}
}

// runConfigLoadTest executes a config load test case.
func (tc configTestCase) runConfigLoadTest(t *testing.T) {
	t.Helper()

	helper := newTestHelper(t)

	tmpDir, cleanup := helper.createTempDir("config-test")
	defer cleanup()

	configPath := helper.writeConfigFile(tmpDir, tc.content)

	cfg, err := config.Load(configPath)
	if (err != nil) != tc.wantErr {
		t.Errorf("Load() error = %v, wantErr %v", err, tc.wantErr)

		return
	}

	if !tc.wantErr && tc.validate != nil {
		tc.validate(t, cfg)
	}
}

// Helper function to create a base test config.
func createBaseTestConfig() config.Config {
	return config.Config{
		Project: struct {
			Name        string `toml:"name"`
			Version     string `toml:"version"`
			Description string `toml:"description"`
		}{Name: "", Version: "", Description: ""},
		Paths: struct {
			InputDir  string `toml:"input_dir"`
			OutputDir string `toml:"output_dir"`
		}{InputDir: "", OutputDir: ""},
		Prompts: struct {
			Augmentation string `toml:"augmentation"`
		}{Augmentation: ""},
		Augmentation: struct {
			Type             string `toml:"type"`
			CustomPrompt     string `toml:"custom_prompt"`
			UsePromptBuilder bool   `toml:"use_prompt_builder"`
		}{Type: "", CustomPrompt: "", UsePromptBuilder: false},
		Logging: struct {
			Level                string `toml:"level"`
			Dir                  string `toml:"dir"`
			EnableFileLogging    bool   `toml:"enable_file_logging"`
			EnableConsoleLogging bool   `toml:"enable_console_logging"`
		}{Level: "", Dir: "", EnableFileLogging: false, EnableConsoleLogging: false},
		Tesseract: struct {
			Language       string `toml:"language"`
			OEM            int    `toml:"oem"`
			PSM            int    `toml:"psm"`
			DPI            int    `toml:"dpi"`
			TimeoutSeconds int    `toml:"timeout_seconds"`
		}{Language: "", OEM: 0, PSM: 0, DPI: 0, TimeoutSeconds: 0},
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
		}{APIKeyVariable: "", Models: nil, MaxRetries: 0, RetryDelaySeconds: 0, TimeoutSeconds: 0, Temperature: 0.0, TopK: 0, TopP: 0.0, MaxTokens: 0},
		Settings: struct {
			Workers            int  `toml:"workers"`
			TimeoutSeconds     int  `toml:"timeout_seconds"`
			EnableAugmentation bool `toml:"enable_augmentation"`
			SkipExisting       bool `toml:"skip_existing"`
		}{Workers: 0, TimeoutSeconds: 0, EnableAugmentation: false, SkipExisting: false},
	}
}

// Helper to create test config with API key.
func createConfigWithAPIKey(apiKeyVar string) config.Config {
	cfg := createBaseTestConfig()

	cfg.Gemini.APIKeyVariable = apiKeyVar

	return cfg
}

// Helper to create test config for directory tests.
func createConfigWithPaths(inputDir, outputDir, logDir string) config.Config {
	cfg := createBaseTestConfig()

	cfg.Paths.InputDir = inputDir
	cfg.Paths.OutputDir = outputDir
	cfg.Logging.Dir = logDir

	return cfg
}

// createValidConfigContent returns a valid TOML config string.
func createValidConfigContent() string {
	return `[paths]
input_dir = "./input"
output_dir = "./output"`
}

// createConfigContentMissingInputDir returns TOML config missing input_dir.
func createConfigContentMissingInputDir() string {
	return `[paths]
output_dir = "./output"`
}

// createConfigContentMissingOutputDir returns TOML config missing output_dir.
func createConfigContentMissingOutputDir() string {
	return `[paths]
input_dir = "./input"`
}

func TestConfig_Load(t *testing.T) {
	tests := []configTestCase{
		newConfigTestCase(
			"valid config with all settings",
			`[project]
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
			false,
			func(t *testing.T, cfg *config.Config) {
				t.Helper()

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
		),
		newConfigTestCase(
			"missing required paths",
			`[project]
name = "test-service"`,
			true,
			nil,
		),
		newConfigTestCase(
			"invalid TOML",
			`[project
name = "invalid"`,
			true,
			nil,
		),
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			testCase.runConfigLoadTest(t)
		})
	}
}

func TestConfig_validate(t *testing.T) {
	tests := []configTestCase{
		newConfigTestCase(
			"valid config",
			createValidConfigContent(),
			false,
			nil,
		),
		newConfigTestCase(
			"missing input dir",
			createConfigContentMissingInputDir(),
			true,
			nil,
		),
		newConfigTestCase(
			"missing output dir",
			createConfigContentMissingOutputDir(),
			true,
			nil,
		),
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			testCase.runConfigLoadTest(t)
		})
	}
}

func TestConfig_GetAPIKey(t *testing.T) {
	tests := []struct {
		name   string
		envKey string
		envVal string
		want   string
		config config.Config
	}{
		{
			name:   "valid API key",
			config: createConfigWithAPIKey("TEST_API_KEY"),
			envKey: "TEST_API_KEY",
			envVal: "secret-key-123",
			want:   "secret-key-123",
		},
		{
			name:   "empty variable name",
			config: createConfigWithAPIKey(""),
			envKey: "",
			envVal: "",
			want:   "",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if testCase.envKey != "" && testCase.envVal != "" {
				envHelper := newEnvTestHelper(t)
				envHelper.withEnvVar(
					testCase.envKey,
					testCase.envVal,
					func() {
						if got := testCase.config.GetAPIKey(); got != testCase.want {
							t.Errorf(
								"Config.GetAPIKey() = %v, want %v",
								got,
								testCase.want,
							)
						}
					},
				)
			} else {
				if got := testCase.config.GetAPIKey(); got != testCase.want {
					t.Errorf("Config.GetAPIKey() = %v, want %v", got, testCase.want)
				}
			}
		})
	}
}

func TestConfig_EnsureDirectories(t *testing.T) {
	t.Parallel()

	helper := newTestHelper(t)

	tmpDir, cleanup := helper.createTempDir("config-ensure-test")
	defer cleanup()

	cfg := createConfigWithPaths(
		filepath.Join(tmpDir, "input"),
		filepath.Join(tmpDir, "output"),
		filepath.Join(tmpDir, "logs"),
	)

	err := cfg.EnsureDirectories()
	if err != nil {
		t.Errorf("Config.EnsureDirectories() error = %v", err)
	}

	// Check that directories were created using helper
	dirs := []string{cfg.Paths.InputDir, cfg.Paths.OutputDir, cfg.Logging.Dir}
	for _, dir := range dirs {
		helper.validateDirectoryExists(dir)
	}
}

func TestConfig_GetLogFilePath(t *testing.T) {
	t.Parallel()

	cfg := createConfigWithPaths("", "", "/tmp/logs")

	filename := "test.log"
	expected := "/tmp/logs/test.log"

	if got := cfg.GetLogFilePath(filename); got != expected {
		t.Errorf("Config.GetLogFilePath() = %v, want %v", got, expected)
	}
}

func BenchmarkConfig_Load(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "config-bench")
	if err != nil {
		b.Fatalf("Failed to create temp dir: %v", err)
	}

	defer func() {
		removeErr := os.RemoveAll(tmpDir)
		if removeErr != nil {
			b.Logf("Failed to remove temp dir: %v", removeErr)
		}
	}()

	configPath := filepath.Join(tmpDir, "project.toml")
	if err := os.WriteFile(configPath, []byte(createValidConfigContent()), 0o600); err != nil {
		b.Fatalf("Failed to write config file: %v", err)
	}

	b.ResetTimer()

	for range b.N {
		_, err := config.Load(configPath)
		if err != nil {
			b.Errorf("config.Load() error = %v", err)
		}
	}
}
