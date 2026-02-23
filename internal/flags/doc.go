// Package flags manages command-line flags and environment variables for Watchtower configuration.
// It configures Docker connections, system behavior, and notifications via Cobra and Viper.
//
// Key components:
//   - RegisterDockerFlags: Adds Docker API client flags.
//   - RegisterSystemFlags: Adds operational control flags.
//   - RegisterNotificationFlags: Adds notification settings.
//   - SetupLogging: Configures logrus based on flags.
//
// Usage example:
//
//	cmd := &cobra.Command{}
//	flags.RegisterSystemFlags(cmd)
//	flags.SetDefaults()
//	err := flags.SetupLogging(cmd.PersistentFlags())
//	if err != nil {
//	    logrus.WithError(err).Fatal("Logging setup failed")
//	}
//
// The package integrates with Cobra for flag parsing, Viper for environment variable binding,
// and logrus for logging configuration errors.
package flags
