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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// MaxPasswordBytes is bcrypt's input limit. Longer passwords are never hashed
// or compared; the login handler answers 401 instead.
const MaxPasswordBytes = 72

const (
	defaultPort      = "8081"
	defaultUploadDir = "/uploads"
	// defaultQdrantURL is the Compose service address. Nothing outside the
	// Compose network can reach it, so the default is safe to ship.
	defaultQdrantURL        = "http://qdrant:6333"
	defaultQdrantCollection = "campusclaw_chunks"
	// maxEmbeddingDimensions bounds the collection size, so a mistyped
	// dimension fails at startup rather than at the first vector write.
	maxEmbeddingDimensions = 8192
)

// Config is the fully validated runtime configuration.
type Config struct {
	Port            string
	UploadDir       string
	PublicOrigin    *url.URL
	DevPublicOrigin *url.URL
	MySQL           MySQL
	Seed            Seed
	Qdrant          Qdrant
	Embedding       EmbeddingGateway
	Chat            ChatGateway
}

// Qdrant locates the private vector store.
type Qdrant struct {
	URL        *url.URL
	Collection string
}

// EmbeddingGateway describes the OpenAI-compatible embedding endpoint the API
// calls to turn text into vectors.
//
// APIKey is a server-side secret: it is handed to the gateway client and never
// logged, echoed in an error, or encoded into a response.
type EmbeddingGateway struct {
	BaseURL    *url.URL
	Model      string
	Dimensions int
	APIKey     string
}

// ChatGateway describes the OpenAI-compatible chat endpoint that writes the
// short answer. Its key is held to the same rule as the embedding one.
type ChatGateway struct {
	BaseURL *url.URL
	Model   string
	APIKey  string
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

// Seed holds the initial passwords for the four seeded accounts.
type Seed struct {
	TeacherAPassword  string
	StudentA1Password string
	TeacherBPassword  string
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
			TeacherBPassword:  os.Getenv("SEED_TEACHER_B_PASSWORD"),
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
		"SEED_TEACHER_B_PASSWORD":  cfg.Seed.TeacherBPassword,
		"SEED_STUDENT_B1_PASSWORD": cfg.Seed.StudentB1Password,
		"EMBEDDING_MODEL":          os.Getenv("EMBEDDING_MODEL"),
		"EMBEDDING_API_KEY":        os.Getenv("EMBEDDING_API_KEY"),
		"CHAT_MODEL":               os.Getenv("CHAT_MODEL"),
		"CHAT_API_KEY":             os.Getenv("CHAT_API_KEY"),
	})

	for _, password := range []struct {
		name  string
		value string
	}{
		{"SEED_TEACHER_A_PASSWORD", cfg.Seed.TeacherAPassword},
		{"SEED_STUDENT_A1_PASSWORD", cfg.Seed.StudentA1Password},
		{"SEED_TEACHER_B_PASSWORD", cfg.Seed.TeacherBPassword},
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

	if devOrigin := os.Getenv("DEV_PUBLIC_ORIGIN"); devOrigin != "" {
		if devOrigin != "http://localhost:5173" || cfg.PublicOrigin == nil ||
			cfg.PublicOrigin.String() != "http://localhost:8080" {
			problems = append(problems, "DEV_PUBLIC_ORIGIN is only allowed for local HTTP development")
		} else {
			cfg.DevPublicOrigin, _ = url.Parse(devOrigin)
		}
	}

	qdrantURL, err := parseGatewayBaseURL(valueOr(os.Getenv("QDRANT_URL"), defaultQdrantURL))
	if err != nil {
		problems = append(problems, fmt.Sprintf("QDRANT_URL %s", err))
	}
	collection := valueOr(os.Getenv("QDRANT_COLLECTION"), defaultQdrantCollection)
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`).MatchString(collection) {
		problems = append(problems, "QDRANT_COLLECTION must use 1..64 letters, digits, underscores or hyphens")
	}
	cfg.Qdrant = Qdrant{URL: qdrantURL, Collection: collection}

	embeddingURL, err := parseGatewayBaseURL(os.Getenv("EMBEDDING_BASE_URL"))
	if err != nil {
		problems = append(problems, fmt.Sprintf("EMBEDDING_BASE_URL %s", err))
	}

	dimensions, err := parseDimensions(os.Getenv("EMBEDDING_DIMENSIONS"))
	if err != nil {
		problems = append(problems, fmt.Sprintf("EMBEDDING_DIMENSIONS %s", err))
	}

	cfg.Embedding = EmbeddingGateway{
		BaseURL:    embeddingURL,
		Model:      os.Getenv("EMBEDDING_MODEL"),
		Dimensions: dimensions,
		APIKey:     os.Getenv("EMBEDDING_API_KEY"),
	}

	chatURL, err := parseGatewayBaseURL(os.Getenv("CHAT_BASE_URL"))
	if err != nil {
		problems = append(problems, fmt.Sprintf("CHAT_BASE_URL %s", err))
	}

	cfg.Chat = ChatGateway{
		BaseURL: chatURL,
		Model:   os.Getenv("CHAT_MODEL"),
		APIKey:  os.Getenv("CHAT_API_KEY"),
	}

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

// parseGatewayBaseURL accepts an absolute http(s) URL for a service the API
// calls. Unlike PUBLIC_ORIGIN it may carry a path, because an
// OpenAI-compatible gateway is commonly mounted under /v1.
func parseGatewayBaseURL(raw string) (*url.URL, error) {
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

	if parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return nil, errors.New("must not include credentials, query or fragment")
	}

	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	return parsed, nil
}

// parseDimensions reads the embedding width as a whole number. The message
// names the variable and the bounds, never the value that was supplied.
func parseDimensions(raw string) (int, error) {
	if raw == "" {
		return 0, errors.New("must not be empty")
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New("must be a whole number")
	}

	if value < 1 || value > maxEmbeddingDimensions {
		return 0, fmt.Errorf("must be between 1 and %d", maxEmbeddingDimensions)
	}

	return value, nil
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
