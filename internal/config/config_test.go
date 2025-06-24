package config

import (
	"flag"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		envVars     map[string]string
		wantConfig  Config
		wantErr     bool
		errContains string
	}{
		{
			name: "default values",
			args: []string{},
			envVars: map[string]string{
				"GITHUB_ORG":   "testorg",
				"GITHUB_TOKEN": "test-token",
				"OUTPUT_DIR":   "./test-repos",
			},
			wantConfig: Config{
				OrgName:    "testorg",
				Token:      "test-token",
				OutputDir:  "./test-repos",
				Workers:    10,
				RetryCount: 5,
			},
			wantErr: false,
		},
		{
			name: "command line flags override defaults",
			args: []string{
				"-org", "flagorg",
				"-token", "flag-token",
				"-output", "./custom-path",
				"-workers", "10",
				"-retry", "5",
			},
			envVars: map[string]string{},
			wantConfig: Config{
				OrgName:    "flagorg",
				Token:      "flag-token",
				OutputDir:  "./custom-path",
				Workers:    10,
				RetryCount: 5,
			},
			wantErr: false,
		},
		{
			name: "environment variables override defaults",
			args: []string{},
			envVars: map[string]string{
				"GITHUB_ORG":   "envorg",
				"GITHUB_TOKEN": "env-token",
				"OUTPUT_DIR":   "./env-path",
				"WORKERS":      "8",
				"RETRY_COUNT":  "4",
			},
			wantConfig: Config{
				OrgName:    "envorg",
				Token:      "env-token",
				OutputDir:  "./env-path",
				Workers:    10,  // Environment vars for workers/retry are not read
				RetryCount: 5,   // Environment vars for workers/retry are not read
			},
			wantErr: false,
		},
		{
			name: "flags override environment variables",
			args: []string{
				"-org", "flagorg",
				"-workers", "15",
			},
			envVars: map[string]string{
				"GITHUB_ORG":   "envorg",
				"GITHUB_TOKEN": "env-token",
				"OUTPUT_DIR":   "./repos",
				"WORKERS":      "8",
			},
			wantConfig: Config{
				OrgName:    "flagorg",
				Token:      "env-token",
				OutputDir:  "./repos",
				Workers:    15,
				RetryCount: 5,
			},
			wantErr: false,
		},
		{
			name:    "missing required organization",
			args:    []string{"-token", "test-token", "-output", "./repos"},
			envVars: map[string]string{},
			wantErr: true,
			errContains: "org is required",
		},
		{
			name:    "missing required token",
			args:    []string{"-org", "testorg", "-output", "./repos"},
			envVars: map[string]string{},
			wantErr: true,
			errContains: "token is required",
		},
		{
			name:    "missing required output",
			args:    []string{"-org", "testorg", "-token", "test-token"},
			envVars: map[string]string{},
			wantErr: true,
			errContains: "output directory is required",
		},
		{
			name: "invalid workers count",
			args: []string{"-workers", "0", "-output", "./repos"},
			envVars: map[string]string{
				"GITHUB_ORG":   "testorg",
				"GITHUB_TOKEN": "test-token",
			},
			wantConfig: Config{
				OrgName:    "testorg",
				Token:      "test-token",
				OutputDir:  "./repos",
				Workers:    0,
				RetryCount: 5,
			},
			wantErr: false, // No validation for workers count in current implementation
		},
		{
			name: "negative retry count",
			args: []string{"-retry", "-1", "-output", "./repos"},
			envVars: map[string]string{
				"GITHUB_ORG":   "testorg",
				"GITHUB_TOKEN": "test-token",
			},
			wantConfig: Config{
				OrgName:    "testorg",
				Token:      "test-token",
				OutputDir:  "./repos",
				Workers:    10,
				RetryCount: -1,
			},
			wantErr: false, // No validation for retry count in current implementation
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory for test
			tempDir := t.TempDir()
			oldCwd, _ := os.Getwd()
			os.Chdir(tempDir)
			defer os.Chdir(oldCwd)

			// Clear environment
			oldEnv := os.Environ()
			os.Clearenv()
			defer func() {
				os.Clearenv()
				for _, env := range oldEnv {
					kv := splitEnv(env)
					if len(kv) == 2 {
						os.Setenv(kv[0], kv[1])
					}
				}
			}()

			// Set test environment variables
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			// Override os.Args for flag parsing
			oldArgs := os.Args
			os.Args = append([]string{"cmd"}, tt.args...)
			defer func() { 
				os.Args = oldArgs
				// Reset flag.CommandLine to avoid interference between tests
				flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			}()

			// Parse config
			cfg, err := Parse()

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantConfig.OrgName, cfg.OrgName)
			assert.Equal(t, tt.wantConfig.Token, cfg.Token)
			assert.Equal(t, tt.wantConfig.OutputDir, cfg.OutputDir)
			assert.Equal(t, tt.wantConfig.Workers, cfg.Workers)
			assert.Equal(t, tt.wantConfig.RetryCount, cfg.RetryCount)
		})
	}
}

func TestNewGitHubClient(t *testing.T) {
	token := "test-token"

	client, err := NewGitHubClient(token)
	require.NoError(t, err)
	assert.NotNil(t, client)
}

// Helper function to split environment variable
func splitEnv(env string) []string {
	for i := 0; i < len(env); i++ {
		if env[i] == '=' {
			return []string{env[:i], env[i+1:]}
		}
	}
	return []string{env}
}