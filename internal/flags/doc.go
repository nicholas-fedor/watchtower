// Package flags manages command-line flags and environment variables for Watchtower configuration.
// It configures Docker connections, operational behavior, and notifications via Cobra and Viper.
//
// Layout:
//   - internal/flags/spec — FlagSpec metadata (static defaults, env keys, list parse kind)
//   - internal/flags/<domain> — Specs() and/or Register per subsystem
//   - internal/flags/utils — list parsers, env helpers, deprecation
//   - RegisterAll / BindAll / AllSpecs — parent façade
//
// Domain packages match the config taxonomy:
// docker, client, schedule, mode, update, lifecycle, filter, registry, compat,
// api, notify, logging.
//
// Key components:
//   - RegisterAll: Registers every domain's flags on the root command.
//   - RegisterDockerFlags / RegisterSystemFlags / RegisterNotificationFlags: Domain-group helpers for tests.
//   - BindAll: Applies Viper defaults, BindPFlag, and BindEnv from FlagSpec rows.
//   - SetupLogging: Configures logrus based on flags.
//   - ProcessFlagAliases / GetSecretsFromFiles: Pre-load transforms (porcelain, interval→schedule, secrets).
//   - EnvConfig: Maps Docker client flags into DOCKER_* process environment variables.
//
// Domains that expose FlagSpec use static pflag defaults and BindAll (flag > env >
// default). Remaining domains still register with env-baked defaults until converted.
//
// Usage example:
//
//	cmd := &cobra.Command{}
//	flags.SetDefaults()
//	flags.RegisterAll(cmd)
//	err := flags.SetupLogging(cmd.PersistentFlags())
//	if err != nil {
//	    logrus.WithError(err).Fatal("Logging setup failed")
//	}
//
// Resolved process policy belongs in internal/config via config.Load. This package
// only declares and binds flags; it does not own update policy DTOs.
//
// The package integrates with Cobra for flag parsing, Viper for environment variable binding,
// and logrus for logging configuration errors.
package flags
