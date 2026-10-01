package config

import (
	"strconv"
	"strings"
	"testing"
)

// envVars is every variable Load reads. Tests start from all-empty so a stray
// value in the developer's shell cannot make a case pass or fail by accident.
var envVars = []string{
	"PORT",
	"UPLOAD_DIR",
	"MYSQL_HOST",
	"MYSQL_PORT",
	"MYSQL_DATABASE",
	"MYSQL_USER",
	"MYSQL_PASSWORD",
	"MYSQL_ROOT_PASSWORD",
	"SEED_TEACHER_A_PASSWORD",
	"SEED_STUDENT_A1_PASSWORD",
	"SEED_TEACHER_B_PASSWORD",
	"SEED_STUDENT_B1_PASSWORD",
	"PUBLIC_ORIGIN",
	"DEV_PUBLIC_ORIGIN",
	"QDRANT_URL",
	"QDRANT_COLLECTION",
	"EMBEDDING_BASE_URL",
	"EMBEDDING_MODEL",
	"EMBEDDING_DIMENSIONS",
	"EMBEDDING_API_KEY",
	"CHAT_BASE_URL",
	"CHAT_MODEL",
	"CHAT_API_KEY",
}

func setEnv(t *testing.T, overrides map[string]string) {
	t.Helper()

	for _, name := range envVars {
		t.Setenv(name, "")
	}
	for name, value := range overrides {
		t.Setenv(name, value)
	}
}

func validEnv(overrides map[string]string) map[string]string {
	env := map[string]string{
		"MYSQL_HOST":               "db",
		"MYSQL_PORT":               "3306",
		"MYSQL_DATABASE":           "campusclaw",
		"MYSQL_USER":               "campusclaw",
		"MYSQL_PASSWORD":           "db-password",
		"SEED_TEACHER_A_PASSWORD":  "teacher-password",
		"SEED_STUDENT_A1_PASSWORD": "student-a1-password",
		"SEED_TEACHER_B_PASSWORD":  "teacher-b-password",
		"SEED_STUDENT_B1_PASSWORD": "student-b1-password",
		"PUBLIC_ORIGIN":            "http://localhost:8080",
		"EMBEDDING_BASE_URL":       "http://embedding.internal/v1",
		"EMBEDDING_MODEL":          "text-embedding-3-small",
		"EMBEDDING_DIMENSIONS":     "1536",
		"EMBEDDING_API_KEY":        "embedding-key",
		"CHAT_BASE_URL":            "http://chat.internal/v1",
		"CHAT_MODEL":               "gpt-4o-mini",
		"CHAT_API_KEY":             "chat-key",
	}
	for name, value := range overrides {
		env[name] = value
	}
	return env
}

func TestLoadSucceedsWithCompleteEnvironment(t *testing.T) {
	setEnv(t, validEnv(nil))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Port != defaultPort {
		t.Errorf("Port = %q, want default %q", cfg.Port, defaultPort)
	}
	if cfg.UploadDir != defaultUploadDir {
		t.Errorf("UploadDir = %q, want default %q", cfg.UploadDir, defaultUploadDir)
	}
	if cfg.Qdrant.Collection != defaultQdrantCollection {
		t.Errorf("Qdrant.Collection = %q", cfg.Qdrant.Collection)
	}
	if cfg.PublicOrigin.String() != "http://localhost:8080" {
		t.Errorf("PublicOrigin = %q", cfg.PublicOrigin.String())
	}
}

func TestQdrantCollectionCanBeChangedSafely(t *testing.T) {
	setEnv(t, validEnv(map[string]string{"QDRANT_COLLECTION": "campusclaw_chunks_course_2048"}))
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Qdrant.Collection != "campusclaw_chunks_course_2048" {
		t.Errorf("Qdrant.Collection = %q", cfg.Qdrant.Collection)
	}

	setEnv(t, validEnv(map[string]string{"QDRANT_COLLECTION": "../other"}))
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "QDRANT_COLLECTION") {
		t.Errorf("invalid collection error = %v", err)
	}
}

func TestLoadReportsEveryMissingSecretByNameOnly(t *testing.T) {
	env := validEnv(nil)
	delete(env, "PUBLIC_ORIGIN")
	delete(env, "SEED_STUDENT_B1_PASSWORD")
	setEnv(t, env)

	cfg, err := Load()
	if err == nil {
		t.Fatalf("Load() = %+v, want error", cfg)
	}

	message := err.Error()
	for _, name := range []string{"PUBLIC_ORIGIN", "SEED_STUDENT_B1_PASSWORD"} {
		if !strings.Contains(message, name) {
			t.Errorf("error %q does not name %s", message, name)
		}
	}

	// The whole point of naming-only errors: no configured secret leaks out.
	for _, secret := range []string{"teacher-password", "student-a1-password", "db-password"} {
		if strings.Contains(message, secret) {
			t.Errorf("error %q leaked a secret value", message)
		}
	}
}

func TestLoadRejectsPasswordOutsideBcryptWindow(t *testing.T) {
	cases := map[string]string{
		"empty":               "",
		"seventy-three bytes": strings.Repeat("x", 73),
	}

	for name, password := range cases {
		setEnv(t, validEnv(map[string]string{"SEED_TEACHER_A_PASSWORD": password}))

		if _, err := Load(); err == nil {
			t.Errorf("%s: Load() succeeded, want error", name)
		}
	}
}

func TestLoadRejectsMissingOrOverlongTeacherBPassword(t *testing.T) {
	for name, password := range map[string]string{
		"missing":  "",
		"overlong": strings.Repeat("x", 73),
	} {
		t.Run(name, func(t *testing.T) {
			setEnv(t, validEnv(map[string]string{"SEED_TEACHER_B_PASSWORD": password}))
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), "SEED_TEACHER_B_PASSWORD") {
				t.Fatalf("Load() error = %v, want variable name", err)
			}
			if strings.Contains(err.Error(), "teacher-b-password") {
				t.Fatalf("Load() leaked a secret: %v", err)
			}
		})
	}
}

func TestLoadAcceptsPasswordAtBcryptLimit(t *testing.T) {
	setEnv(t, validEnv(map[string]string{"SEED_TEACHER_A_PASSWORD": strings.Repeat("x", 72)}))

	if _, err := Load(); err != nil {
		t.Errorf("Load() error = %v", err)
	}
}

func TestLoadRejectsMalformedPublicOrigin(t *testing.T) {
	cases := map[string]string{
		"no scheme":   "localhost:8080",
		"unsupported": "ftp://localhost",
		"no host":     "http://",
		"with path":   "http://localhost:8080/app",
		"with query":  "http://localhost:8080/?a=1",
		"credentials": "http://user:pass@localhost:8080",
	}

	for name, origin := range cases {
		setEnv(t, validEnv(map[string]string{"PUBLIC_ORIGIN": origin}))

		if _, err := Load(); err == nil {
			t.Errorf("%s (%q): Load() succeeded, want error", name, origin)
		}
	}
}

func TestLoadNormalisesTrailingSlash(t *testing.T) {
	setEnv(t, validEnv(map[string]string{"PUBLIC_ORIGIN": "https://campus.example.edu/"}))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got := cfg.PublicOrigin.String(); got != "https://campus.example.edu" {
		t.Errorf("PublicOrigin = %q, want no trailing slash", got)
	}
}

func TestLoadAllowsOnlyExplicitLocalViteOrigin(t *testing.T) {
	setEnv(t, validEnv(map[string]string{"DEV_PUBLIC_ORIGIN": "http://localhost:5173"}))
	cfg, err := Load()
	if err != nil {
		t.Fatalf("local Vite origin: %v", err)
	}
	if cfg.DevPublicOrigin == nil || cfg.DevPublicOrigin.String() != "http://localhost:5173" {
		t.Fatalf("DevPublicOrigin = %v, want local Vite origin", cfg.DevPublicOrigin)
	}

	for name, overrides := range map[string]map[string]string{
		"other dev port":         {"DEV_PUBLIC_ORIGIN": "http://localhost:5174"},
		"external dev host":      {"DEV_PUBLIC_ORIGIN": "http://evil.example"},
		"nonlocal public origin": {"DEV_PUBLIC_ORIGIN": "http://localhost:5173", "PUBLIC_ORIGIN": "https://school.example"},
	} {
		t.Run(name, func(t *testing.T) {
			setEnv(t, validEnv(overrides))
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), "DEV_PUBLIC_ORIGIN") {
				t.Fatalf("Load() error = %v, want DEV_PUBLIC_ORIGIN rejection", err)
			}
		})
	}
}

func TestLoadReadsVectorAndGatewayConfiguration(t *testing.T) {
	setEnv(t, validEnv(nil))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got := cfg.Qdrant.URL.String(); got != defaultQdrantURL {
		t.Errorf("Qdrant.URL = %q, want the internal default %q", got, defaultQdrantURL)
	}

	// A gateway is commonly mounted under a path, so the path must survive.
	if got := cfg.Embedding.BaseURL.String(); got != "http://embedding.internal/v1" {
		t.Errorf("Embedding.BaseURL = %q, want the configured gateway", got)
	}
	if cfg.Embedding.Model != "text-embedding-3-small" {
		t.Errorf("Embedding.Model = %q", cfg.Embedding.Model)
	}
	if cfg.Embedding.Dimensions != 1536 {
		t.Errorf("Embedding.Dimensions = %d, want 1536", cfg.Embedding.Dimensions)
	}

	if got := cfg.Chat.BaseURL.String(); got != "http://chat.internal/v1" {
		t.Errorf("Chat.BaseURL = %q", got)
	}
	if cfg.Chat.Model != "gpt-4o-mini" {
		t.Errorf("Chat.Model = %q", cfg.Chat.Model)
	}
}

func TestLoadNormalisesTrailingSlashOnAGatewayBaseURL(t *testing.T) {
	setEnv(t, validEnv(map[string]string{"CHAT_BASE_URL": "https://chat.example.edu/v1/"}))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got := cfg.Chat.BaseURL.String(); got != "https://chat.example.edu/v1" {
		t.Errorf("Chat.BaseURL = %q, want no trailing slash", got)
	}
}

// A width the collection cannot be created with must fail at startup, and the
// message must name the variable without echoing what was supplied.
func TestLoadRejectsInvalidEmbeddingDimensions(t *testing.T) {
	cases := map[string]string{
		"empty":        "",
		"not a number": "many",
		"zero":         "0",
		"negative":     "-1",
		"fraction":     "1536.5",
		"too large":    strconv.Itoa(maxEmbeddingDimensions + 1),
	}

	for name, dimensions := range cases {
		t.Run(name, func(t *testing.T) {
			setEnv(t, validEnv(map[string]string{"EMBEDDING_DIMENSIONS": dimensions}))

			_, err := Load()
			if err == nil {
				t.Fatalf("Load() succeeded with EMBEDDING_DIMENSIONS=%q", dimensions)
			}
			if !strings.Contains(err.Error(), "EMBEDDING_DIMENSIONS") {
				t.Errorf("error %q does not name EMBEDDING_DIMENSIONS", err)
			}
			if dimensions != "" && strings.Contains(err.Error(), dimensions) {
				t.Errorf("error %q echoed the supplied dimension", err)
			}
		})
	}
}

func TestLoadAcceptsTheDimensionBounds(t *testing.T) {
	for _, dimensions := range []int{1, maxEmbeddingDimensions} {
		setEnv(t, validEnv(map[string]string{"EMBEDDING_DIMENSIONS": strconv.Itoa(dimensions)}))

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v, want %d to be accepted", err, dimensions)
		}
		if cfg.Embedding.Dimensions != dimensions {
			t.Errorf("Embedding.Dimensions = %d, want %d", cfg.Embedding.Dimensions, dimensions)
		}
	}
}

func TestLoadRejectsMalformedServiceURLs(t *testing.T) {
	cases := []struct {
		name     string
		variable string
		value    string
	}{
		{"embedding without scheme", "EMBEDDING_BASE_URL", "embedding.internal/v1"},
		{"embedding unsupported scheme", "EMBEDDING_BASE_URL", "ftp://embedding.internal"},
		{"embedding without host", "EMBEDDING_BASE_URL", "http://"},
		{"embedding with credentials", "EMBEDDING_BASE_URL", "http://user:pass@embedding.internal"},
		{"chat without scheme", "CHAT_BASE_URL", "chat.internal"},
		{"qdrant without host", "QDRANT_URL", "http://"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			setEnv(t, validEnv(map[string]string{testCase.variable: testCase.value}))

			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), testCase.variable) {
				t.Fatalf("Load() error = %v, want a %s rejection", err, testCase.variable)
			}
		})
	}
}

// The gateway keys are server-side secrets: a failed start names the missing
// variable but must never print a key that was supplied.
func TestLoadNeverEchoesGatewayKeys(t *testing.T) {
	env := validEnv(nil)
	delete(env, "EMBEDDING_BASE_URL")
	delete(env, "CHAT_MODEL")
	setEnv(t, env)

	_, err := Load()
	if err == nil {
		t.Fatal("Load() succeeded with an incomplete environment")
	}

	for _, secret := range []string{"embedding-key", "chat-key"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error %q leaked a gateway key", err)
		}
	}

	for _, name := range []string{"EMBEDDING_BASE_URL", "CHAT_MODEL"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name %s", err, name)
		}
	}
}

func TestDSNCarriesConnectionSettings(t *testing.T) {
	setEnv(t, validEnv(nil))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	dsn := cfg.MySQL.DSN()
	for _, fragment := range []string{
		"campusclaw:db-password@tcp(db:3306)/campusclaw",
		"parseTime=true",
		"loc=UTC",
		"utf8mb4",
		// Without a dial timeout an unreachable host hangs the request
		// instead of failing.
		"timeout=5s",
	} {
		if !strings.Contains(dsn, fragment) {
			t.Errorf("DSN %q missing %q", dsn, fragment)
		}
	}

	if strings.Contains(dsn, "multiStatements=true") {
		t.Error("the application pool must not enable multiStatements")
	}

	if !strings.Contains(cfg.MySQL.MigrationDSN(), "multiStatements=true") {
		t.Error("the migration connection needs multiStatements so a migration file is one Exec")
	}
}
