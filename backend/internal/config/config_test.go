package config

import (
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
	"SESSION_COOKIE_SECURE",
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
		"SESSION_COOKIE_SECURE":    "false",
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
	if cfg.PublicOrigin.String() != "http://localhost:8080" {
		t.Errorf("PublicOrigin = %q", cfg.PublicOrigin.String())
	}
	if cfg.SessionCookieSecure {
		t.Error("SessionCookieSecure = true, want false")
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
		"secure cookie":          {"DEV_PUBLIC_ORIGIN": "http://localhost:5173", "SESSION_COOKIE_SECURE": "true"},
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

func TestLoadRequiresExplicitCookieSecureFlag(t *testing.T) {
	for _, value := range []string{"", "yes", "1", "TRUE!"} {
		setEnv(t, validEnv(map[string]string{"SESSION_COOKIE_SECURE": value}))

		if _, err := Load(); err == nil {
			t.Errorf("SESSION_COOKIE_SECURE=%q: Load() succeeded, want error", value)
		}
	}

	setEnv(t, validEnv(map[string]string{"SESSION_COOKIE_SECURE": "true"}))
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.SessionCookieSecure {
		t.Error("SessionCookieSecure = false, want true")
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
