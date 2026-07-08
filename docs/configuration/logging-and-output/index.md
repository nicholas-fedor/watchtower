# Logging and Output

## Debug

Enables debug mode with verbose logging.

```text
            Argument: --debug, -d
Environment Variable: WATCHTOWER_DEBUG
                Type: Boolean
             Default: false
```

!!! Note
    Equivalent to `--log-level debug`.
    As an argument, it does not accept a value (e.g., `--debug true` is invalid).

    See [Maximum Log Level](#maximum_log_level).

## Trace

Enables trace mode with highly verbose logging, including sensitive information like credentials.

```text
            Argument: --trace
Environment Variable: WATCHTOWER_TRACE
                Type: Boolean
             Default: false
```

!!! Note
    Equivalent to `--log-level trace`.
    As an argument, does not accept a value (e.g., `--trace true` is invalid).

    See [Maximum Log Level](#maximum_log_level).

!!! Warning
    Use with caution due to credential exposure.

## Maximum Log Level

Sets the maximum log level output to STDERR, visible in `docker logs` when running Watchtower in a container.

```text
            Argument: --log-level
Environment Variable: WATCHTOWER_LOG_LEVEL
     Possible Values: panic, fatal, error, warn, info, debug, trace
             Default: info
```

## Logging Format

Specifies the format for console log output.

```text
            Argument: --log-format, -l
Environment Variable: WATCHTOWER_LOG_FORMAT
     Possible Values: Auto, LogFmt, Pretty, JSON
             Default: Auto
```

## Disable ANSI Colors

Disables ANSI color escape codes in log output for plain text logs.

```text
            Argument: --no-color
Environment Variable: NO_COLOR
                Type: Boolean
             Default: false
```

## Programmatic Output (Porcelain)

Outputs session results in a machine-readable format (version specified by `VERSION`).

```text
            Argument: --porcelain, -P
Environment Variable: WATCHTOWER_PORCELAIN
     Possible Values: v1
             Default: None
```

!!! Note
    Equivalent to:
    ```text
    --notification-url logger://
    --notification-log-stdout
    --notification-report
    --notification-template porcelain.VERSION.summary-no-log
    ```
