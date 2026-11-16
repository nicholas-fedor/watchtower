package settings

import (
	"bufio"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/rs/zerolog"
	"github.com/spf13/pflag"

	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
	"github.com/nicholas-fedor/watchtower/internal/settings/flags/schedule"
)

// Errors for flag and environment configuration.
var (
	// errInvalidLogFormat indicates an invalid log format was specified in configuration.
	errInvalidLogFormat = errors.New("invalid log format specified")
	// errInvalidLogLevel indicates an invalid log level was specified in configuration.
	errInvalidLogLevel = errors.New("invalid log level specified")
	// errOpenFileFailed indicates a failure to open a file when reading secrets.
	errOpenFileFailed = errors.New("failed to open secret file")
	// errReplaceSliceFailed indicates a failure to replace a slice value in a flag.
	errReplaceSliceFailed = errors.New("failed to replace slice value in flag")
	// errReadFileFailed indicates a failure to read a file's contents for secrets.
	errReadFileFailed = errors.New("failed to read secret file")
	// errInvalidSecretURL indicates an invalid URL was found in a secret file.
	errInvalidSecretURL = errors.New("invalid notification URL in secret file")
	// errSetFlagFailed indicates a failure to set a flag's value during configuration.
	errSetFlagFailed = errors.New("failed to set flag value")
	// errInvalidFlagName indicates an invalid flag name was provided for modification.
	errInvalidFlagName = errors.New("invalid flag name provided")
	// errNotSliceValue indicates a flag does not support slice values for appending.
	errNotSliceValue = errors.New("flag does not support slice values")
	// errUnknownPorcelainVersion indicates a porcelain version other than v1 or json.
	errUnknownPorcelainVersion = errors.New("unknown porcelain version, supported: v1, json")
	// errIntervalAndSchedule indicates that both an interval and a schedule were set.
	errIntervalAndSchedule = errors.New("cannot define both interval and schedule")
)

// prepareFlags readies the parsed root command flags in flagSet for loading
// and returns the logger to use from then on.
//
// It applies environment variables to unset flags and sets the log format and
// level. It then applies the alias flags, which can force a debug or trace
// level, and sets the logging up again so that level applies. Last, it reads
// secret files into their flags. Setting the format before the aliases makes
// their errors use the chosen format.
//
// Parameters:
//   - log: Logger to reconfigure.
//   - flagSet: The root command's persistent flags, registered and parsed.
//
// Returns:
//   - *zerolog.Logger: The logger with the configured format and level. On a
//     failure it is the last logger set up, for reporting the failure.
//   - error: Non-nil when a step fails, naming the step.
func prepareFlags(log *zerolog.Logger, flagSet *pflag.FlagSet) (*zerolog.Logger, error) {
	err := applyEnvToFlags(flagSet, AllSpecs())
	if err != nil {
		return log, fmt.Errorf("apply environment configuration: %w", err)
	}

	log, err = setupLogging(log, flagSet)
	if err != nil {
		return log, fmt.Errorf("set up logging: %w", err)
	}

	err = processFlagAliases(log, flagSet)
	if err != nil {
		return log, fmt.Errorf("process flag aliases: %w", err)
	}

	log, err = setupLogging(log, flagSet)
	if err != nil {
		return log, fmt.Errorf("set up logging: %w", err)
	}

	err = getSecretsFromFiles(log, flagSet)
	if err != nil {
		return log, fmt.Errorf("load secrets from files: %w", err)
	}

	return log, nil
}

// getSecretsFromFiles updates flags with file contents for secrets.
//
// Parameters:
//   - log: Logger for secret-loading diagnostics.
//   - flags: The root command's persistent flags.
//
// Returns:
//   - error: Non-nil when a secret file cannot be loaded, naming its flag.
//
//nolint:godox
func getSecretsFromFiles(log *zerolog.Logger, flags *pflag.FlagSet) error {
	secrets := []string{
		// TODO: Remove just before v2 Release.
		"notification-email-server-password",
		// TODO: Remove just before v2 Release.
		"notification-slack-hook-url",
		// TODO: Remove just before v2 Release.
		"notification-msteams-hook",
		// TODO: Remove just before v2 Release.
		"notification-gotify-token",
		"notification-url",
		"http-api-token",
		"http-api-events-token",
		"git-auth-token",
		"git-password",
	}

	// Process each secret flag.
	for _, secret := range secrets {
		err := getSecretFromFile(log, flags, secret)
		if err != nil {
			return fmt.Errorf("%s: %w", secret, err)
		}
	}

	return nil
}

// getSecretFromFile reads file contents into a flag if applicable.
//
// Parameters:
//   - log: Logger for secret-loading diagnostics.
//   - flags: Flag set.
//   - secret: Flag name.
//
// Returns:
//   - error: Non-nil if file ops fail, nil on success or skip.
func getSecretFromFile(log *zerolog.Logger, flags *pflag.FlagSet, secret string) error {
	flag := flags.Lookup(secret)

	// Handle slice flags.
	if sliceValue, ok := flag.Value.(pflag.SliceValue); ok {
		oldValues := sliceValue.GetSlice()
		values := make([]string, 0, len(oldValues))

		for _, value := range oldValues {
			if value != "" && isFilePath(value) {
				lines, err := readSecretFileLines(log, secret, value)
				if err != nil {
					return err
				}

				values = append(values, lines...)
			} else {
				values = append(values, value)
			}
		}

		err := sliceValue.Replace(values)
		if err != nil {
			log.Debug().
				Err(err).
				Str("flag", secret).
				Msg("Failed to replace slice value in flag")

			return fmt.Errorf("%w: %w", errReplaceSliceFailed, err)
		}

		return nil
	}

	// Handle string flags.
	value := flag.Value.String()
	if value != "" && isFilePath(value) {
		content, err := os.ReadFile(value)
		if err != nil {
			log.Debug().
				Err(err).
				Str("flag", secret).
				Str("file", value).
				Msg("Failed to read secret file")

			return fmt.Errorf("%w: %w", errReadFileFailed, err)
		}

		err = flags.Set(secret, strings.TrimSpace(string(content)))
		if err != nil {
			log.Debug().
				Err(err).
				Str("flag", secret).
				Msg("Failed to set flag from file contents")

			return fmt.Errorf("%w: %w", errSetFlagFailed, err)
		}

		log.Debug().
			Str("flag", secret).
			Str("file", value).
			Msg("Set flag from file contents")
	}

	return nil
}

// readSecretFileLines reads the values of a list secret from a file.
//
// Blank lines and lines starting with "#" are skipped. The file is closed
// before returning, so a list secret holds at most one file open at a time.
//
// Parameters:
//   - log: Logger for secret-loading diagnostics.
//   - secret: Flag name. Lines of the notification-url secret must be valid notification URLs.
//   - path: Path of the secret file.
//
// Returns:
//   - []string: Values read from the file.
//   - error: Non-nil if the file cannot be opened or read, or holds an invalid notification URL.
func readSecretFileLines(log *zerolog.Logger, secret, path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		log.Debug().
			Err(err).
			Str("flag", secret).
			Str("file", path).
			Msg("Failed to open secret file")

		return nil, fmt.Errorf("%w: %w", errOpenFileFailed, err)
	}

	defer func() { _ = file.Close() }()

	var lines []string

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if secret == "notification-url" {
			if !strings.Contains(line, "://") {
				return nil, errInvalidSecretURL
			}

			parsedURL, err := url.Parse(line)
			if err != nil || parsedURL.Scheme == "" {
				return nil, errInvalidSecretURL
			}

			if parsedURL.Opaque == "" && parsedURL.Host == "" && parsedURL.Path == "" {
				if parsedURL.Scheme != "logger" && parsedURL.Scheme != "mock" {
					return nil, errInvalidSecretURL
				}
			}
		}

		lines = append(lines, line)
	}

	err = scanner.Err()
	if err != nil {
		log.Debug().
			Err(err).
			Str("flag", secret).
			Str("file", path).
			Msg("Failed to read secret file")

		return nil, fmt.Errorf("%w: %w", errReadFileFailed, err)
	}

	log.Debug().
		Str("flag", secret).
		Str("file", path).
		Msg("Read secret from file into slice")

	return lines, nil
}

// isFilePath checks if a string is likely a file path.
//
// Parameters:
//   - path: String to check.
//
// Returns:
//   - bool: True if likely a file path, false otherwise.
func isFilePath(path string) bool {
	firstColon := strings.IndexRune(path, ':')
	if firstColon != 1 && firstColon != -1 {
		// If ':' exists but isn't the second character, it's likely not a file path (e.g., URLs).
		return false
	}

	//nolint:gosec // G703: Path traversal via taint analysis - validating user-provided path exists
	_, err := os.Stat(path)

	return !errors.Is(err, os.ErrNotExist)
}

// processFlagAliases applies the alias flags: porcelain mode, an interval as a
// schedule, and the debug and trace log levels.
//
// The environment must already be applied to the flags.
//
// Parameters:
//   - log: Logger for alias diagnostics.
//   - flags: Parsed persistent flag set.
//
// Returns:
//   - error: Non-nil when the flags hold an invalid configuration.
func processFlagAliases(log *zerolog.Logger, flags *pflag.FlagSet) error {
	// Handle porcelain mode.
	porcelain, err := flags.GetString("porcelain")
	if err != nil {
		return fmt.Errorf("read porcelain flag: %w", err)
	}

	if porcelain != "" {
		switch porcelain {
		case "v1", "json":
		default:
			return fmt.Errorf("%w: %q", errUnknownPorcelainVersion, porcelain)
		}

		err := appendFlagValue(log, flags, "notification-url", "logger://")
		if err != nil {
			log.Debug().Err(err).Msg("Failed to append notification-url")
		}

		setFlagIfDefault(log, flags, "notification-log-stdout", "true")
		setFlagIfDefault(log, flags, "notification-report", "true")

		var tpl string
		if porcelain == "v1" {
			tpl = "porcelain.v1.summary-no-log"
		} else {
			tpl = "porcelain.json"
		}

		setFlagIfDefault(log, flags, "notification-template", tpl)
		log.Debug().
			Str("porcelain", porcelain).
			Msg("Configured porcelain mode")
	}

	// Handle interval vs. schedule conflicts.
	scheduleChanged := flags.Changed("schedule")
	intervalChanged := flags.Changed("interval")

	if val, _ := flags.GetString("schedule"); val != "" {
		scheduleChanged = true
	}

	if val, _ := flags.GetInt("interval"); val != schedule.DefaultPollIntervalSeconds {
		intervalChanged = true
	}

	if intervalChanged && scheduleChanged {
		return errIntervalAndSchedule
	}

	// Update schedule to match interval or default if needed.
	if intervalChanged || !scheduleChanged {
		interval, _ := flags.GetInt("interval")

		scheduleValue := fmt.Sprintf("@every %ds", interval)

		err := flags.Set("schedule", scheduleValue)
		if err != nil {
			log.Debug().
				Err(err).
				Int("interval", interval).
				Msg("Failed to set schedule from interval")
		} else {
			log.Debug().
				Int("interval", interval).
				Str("schedule", scheduleValue).
				Msg("Set default schedule from interval")
		}
	}

	// Adjust log level for debug/trace.
	debug, err := flags.GetBool("debug")
	if err != nil {
		return fmt.Errorf("read debug flag: %w", err)
	}

	if debug {
		err := flags.Set("log-level", "debug")
		if err != nil {
			log.Debug().Err(err).Msg("Failed to set debug log level")
		}
	}

	trace, err := flags.GetBool("trace")
	if err != nil {
		return fmt.Errorf("read trace flag: %w", err)
	}

	if trace {
		err := flags.Set("log-level", "trace")
		if err != nil {
			log.Debug().Err(err).Msg("Failed to set trace log level")
		}
	}

	return nil
}

// setupLogging configures format and level on the provided logger.
//
// Parameters:
//   - log: Logger to reconfigure (typically from logging.New at process start).
//   - flags: Flag set.
//
// Returns:
//   - *zerolog.Logger: Logger with format writer and level applied.
//   - error: Non-nil if config fails, nil on success.
func setupLogging(log *zerolog.Logger, flags *pflag.FlagSet) (*zerolog.Logger, error) {
	logFormat, err := flags.GetString("log-format")
	if err != nil {
		log.Debug().
			Err(err).
			Str("flag", "log-format").
			Msg("Failed to get log-format flag")

		return log, fmt.Errorf("%w: %w", errSetFlagFailed, err)
	}

	// Default to "auto" when neither the flag nor WATCHTOWER_LOG_FORMAT is set.
	// This prevents ConfigureWriter from failing on empty strings, which is the
	// case when running the ephemeral orchestrator container without
	// WATCHTOWER_LOG_FORMAT in its environment.
	if logFormat == "" {
		logFormat = "auto"
	}

	noColor, err := flags.GetBool("no-color")
	if err != nil {
		log.Debug().
			Err(err).
			Str("flag", "no-color").
			Msg("Failed to get no-color flag")

		return log, fmt.Errorf("%w: %w", errSetFlagFailed, err)
	}

	writer, err := logging.ConfigureWriter(logFormat, noColor)
	if err != nil {
		log.Debug().
			Err(err).
			Str("format", logFormat).
			Msg("Invalid log format specified")

		return log, fmt.Errorf("%w: %w", errInvalidLogFormat, err)
	}

	// Rebuild with the format writer, preserving the current level until
	// --log-level (including debug/trace aliases) is applied below.
	rebuilt := zerolog.New(writer).Level(log.GetLevel()).With().Timestamp().Logger()
	log = &rebuilt

	// Set log level only when explicitly specified.
	rawLogLevel, err := flags.GetString("log-level")
	if err != nil {
		log.Debug().
			Err(err).
			Str("flag", "log-level").
			Msg("Failed to get log-level flag")

		return log, fmt.Errorf("%w: %w", errSetFlagFailed, err)
	}

	// Parse and apply log level when non-empty.
	// Under normal registration the flag default is "info" (see flags/logging Specs),
	// so GetString is rarely empty. The empty branch is defensive for tests or
	// partial flag sets (for example orchestrator paths that never registered logging
	// flags) and preserves the level already on log (typically InfoLevel from main).
	// processFlagAliases may have already forced debug/trace onto the log-level flag.
	// Invalid levels fail fast.
	if rawLogLevel != "" {
		level, parseErr := logging.ParseLevel(rawLogLevel)
		if parseErr != nil {
			log.Debug().
				Err(parseErr).
				Str("log_level", rawLogLevel).
				Msg("Invalid log level specified")

			return log, fmt.Errorf("%w: %w", errInvalidLogLevel, parseErr)
		}

		leveled := log.Level(level)
		log = &leveled
	}

	log.Debug().
		Str("format", logFormat).
		Str("log_level", log.GetLevel().String()).
		Bool("no_color", noColor).
		Msg("Configured logging settings")

	return log, nil
}

// appendFlagValue appends values to a slice flag.
//
// Parameters:
//   - log: Logger for append diagnostics.
//   - flags: Flag set.
//   - name: Flag name.
//   - values: Values to append.
//
// Returns:
//   - error: Non-nil if append fails, nil on success.
func appendFlagValue(log *zerolog.Logger, flags *pflag.FlagSet, name string, values ...string) error {
	flag := flags.Lookup(name)
	if flag == nil {
		log.Debug().
			Str("flag", name).
			Msg("Invalid flag name provided")

		return fmt.Errorf("%w: %q", errInvalidFlagName, name)
	}

	if flagValues, ok := flag.Value.(pflag.SliceValue); ok {
		for _, value := range values {
			err := flagValues.Append(value)
			if err != nil {
				log.Debug().
					Err(err).
					Str("flag", name).
					Str("value", value).
					Msg("Failed to append value to flag")
			}
		}
	} else {
		log.Debug().
			Str("flag", name).
			Msg("Flag does not support slice values")

		return fmt.Errorf("%w: %q", errNotSliceValue, name)
	}

	return nil
}

// setFlagIfDefault sets a flag's value unless it was set by a flag or by an
// environment variable. applyEnvToFlags leaves environment values unmarked, so
// they are checked separately.
//
// Parameters:
//   - log: Logger for set diagnostics.
//   - flags: Flag set.
//   - name: Flag name.
//   - value: Value to set.
func setFlagIfDefault(log *zerolog.Logger, flags *pflag.FlagSet, name, value string) {
	if flags.Changed(name) || setByEnv(name) {
		return
	}

	err := flags.Set(name, value)
	if err != nil {
		log.Debug().
			Err(err).
			Str("flag", name).
			Str("value", value).
			Msg("Failed to set default flag value")
	} else {
		log.Debug().
			Str("flag", name).
			Str("value", value).
			Msg("Set default flag value")
	}
}

// setByEnv reports whether an environment variable supplies the flag's value,
// using the same rule as applyEnvToFlags.
//
// Parameters:
//   - name: Flag name.
//
// Returns:
//   - bool: True if one of the flag's environment variables is set.
func setByEnv(name string) bool {
	flagSpec, ok := specNamed(name)
	if !ok {
		return false
	}

	_, ok = firstEnv(flagSpec.EnvKeys)

	return ok
}
