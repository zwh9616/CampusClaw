// Package config loads and validates the API's server-side configuration.
//
// Every secret is read from the process environment. Validation errors name the
// offending variables but never echo their values, so a failure to start cannot
// leak a password into logs.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

// MaxPasswordBytes is bcrypt's input limit. Longer passwords are never hashed
// or compared; the login handler answers 401 instead.
const MaxPasswordBytes = 72

const (
	defaultPort      = "8081"
	defaultUploadDir = "/uploads"
)

// Config is the fully validated runtime configuration.
type Config struct {
	Port         string
	UploadDir    string
	PublicOrigin *url.URL
	// SessionCookieSecure must be true wherever the browser reaches the API
	// over HTTPS; it stays false for local HTTP development.
	SessionCookieSecure bool
	MySQL               MySQL
	Seed                Seed
}

// MySQL holds the database connection settings used by the API.
type MySQL struct {
	Host     string
	Port     string
	Name     string
	User     string
	Password string
}

// DSN builds the connection string used by the request-serving pool. It is
// only ever handed to the driver, never logged.
func (m MySQL) DSN() string {
	return m.dsn(false)
}

// MigrationDSN additionally allows several statements per Exec so a migration
// file can be sent as one unit. Only the startup migration pass uses it; the
// application pool keeps multiStatements off.
func (m MySQL) MigrationDSN() string {
	return m.dsn(true)
}

// dialTimeout bounds connection establishment. Without it a name that resolves
// but never answers — a flaky resolver or a wedged host — leaves the driver's
// connect blocked on the OS default, which can be minutes. That would hang
// start-up, the readiness probe and every request instead of failing fast.
const dialTimeout = 5 * time.Second

func (m MySQL) dsn(multiStatements bool) string {
	// Times are stored as UTC; loc=UTC makes the driver read them back as UTC
	// too. The connection collation is deliberately left at the server default
	// so each column's own collation governs its comparisons.
	return fmt.Sprintf(
		"%s:%s@tcp(%s:%s)/%s?parseTime=true&loc=UTC&charset=utf8mb4&timeout=%s&multiStatements=%t",
		m.User, m.Password, m.Host, m.Port, m.Name, dialTimeout, multiStatements,
	)
}

// Seed holds the initial passwords for the three seeded accounts.
type Seed struct {
	TeacherAPassword  string
	StudentA1Password string
	StudentB1Password string
}

// Load reads the environment, applies defaults and validates the result.
func Load() (*Config, error) {
	cfg := &Config{
		Port:      valueOr(os.Getenv("PORT"), defaultPort),
		UploadDir: valueOr(os.Getenv("UPLOAD_DIR"), defaultUploadDir),
		MySQL: MySQL{
			Host:     os.Getenv("MYSQL_HOST"),
			Port:     os.Getenv("MYSQL_PORT"),
			Name:     os.Getenv("MYSQL_DATABASE"),
			User:     os.Getenv("MYSQL_USER"),
			Password: os.Getenv("MYSQL_PASSWORD"),
		},
		Seed: Seed{
			TeacherAPassword:  os.Getenv("SEED_TEACHER_A_PASSWORD"),
			StudentA1Password: os.Getenv("SEED_STUDENT_A1_PASSWORD"),
			StudentB1Password: os.Getenv("SEED_STUDENT_B1_PASSWORD"),
		},
	}

	var problems []string

	requireAll(&problems, map[string]string{
		"MYSQL_HOST":               cfg.MySQL.Host,
		"MYSQL_PORT":               cfg.MySQL.Port,
		"MYSQL_DATABASE":           cfg.MySQL.Name,
		"MYSQL_USER":               cfg.MySQL.User,
		"MYSQL_PASSWORD":           cfg.MySQL.Password,
		"SEED_TEACHER_A_PASSWORD":  cfg.Seed.TeacherAPassword,
		"SEED_STUDENT_A1_PASSWORD": cfg.Seed.StudentA1Password,
		"SEED_STUDENT_B1_PASSWORD": cfg.Seed.StudentB1Password,
	})

	for _, password := range []struct {
		name  string
		value string
	}{
		{"SEED_TEACHER_A_PASSWORD", cfg.Seed.TeacherAPassword},
		{"SEED_STUDENT_A1_PASSWORD", cfg.Seed.StudentA1Password},
		{"SEED_STUDENT_B1_PASSWORD", cfg.Seed.StudentB1Password},
	} {
		if err := validatePassword(password.value); err != nil {
			problems = append(problems, fmt.Sprintf("%s %s", password.name, err))
		}
	}

	origin, err := parsePublicOrigin(os.Getenv("PUBLIC_ORIGIN"))
	if err != nil {
		problems = append(problems, fmt.Sprintf("PUBLIC_ORIGIN %s", err))
	}
	cfg.PublicOrigin = origin

	secure, err := parseBool("SESSION_COOKIE_SECURE", os.Getenv("SESSION_COOKIE_SECURE"))
	if err != nil {
		problems = append(problems, err.Error())
	}
	cfg.SessionCookieSecure = secure

	if len(problems) > 0 {
		return nil, fmt.Errorf("invalid configuration: %s", strings.Join(problems, "; "))
	}

	return cfg, nil
}

// validatePassword enforces the 1..72 byte window bcrypt can handle. The value
// is counted in bytes, matching how the hash function treats it.
func validatePassword(password string) error {
	switch {
	case password == "":
		return errors.New("must not be empty")
	case len([]byte(password)) > MaxPasswordBytes:
		return fmt.Errorf("must be at most %d bytes", MaxPasswordBytes)
	default:
		return nil
	}
}

// parsePublicOrigin accepts an absolute http(s) origin and normalises away any
// trailing slash so comparisons against request origins are exact.
func parsePublicOrigin(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, errors.New("must not be empty")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("must be a valid absolute URL")
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("must use http or https")
	}

	if parsed.Host == "" {
		return nil, errors.New("must include a host")
	}

	if parsed.Path != "" && parsed.Path != "/" {
		return nil, errors.New("must not include a path")
	}

	if parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return nil, errors.New("must be a bare origin without credentials, query or fragment")
	}

	parsed.Path = ""
	return parsed, nil
}

// parseBool accepts only the two literals documented in .env.example. Being
// strict here means a typo like "yes" or "1" fails the boot loudly instead of
// silently leaving cookies without the Secure attribute in production.
func parseBool(name, raw string) (bool, error) {
	switch raw {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be exactly true or false", name)
	}
}

// requireAll reports every empty variable at once so one failed start reveals
// the whole list instead of one name per attempt.
func requireAll(problems *[]string, values map[string]string) {
	missing := make([]string, 0, len(values))
	for name, value := range values {
		if value == "" {
			missing = append(missing, name)
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		*problems = append(*problems, "missing required environment variables: "+strings.Join(missing, ", "))
	}
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
