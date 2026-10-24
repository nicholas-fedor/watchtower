// Package redact removes credentials from Git URLs and from error text that
// quotes them.
//
// Watchtower stores redacted URLs on the containers it creates and in its
// reports. Error and log text gets the same treatment, so a credential in a Git
// remote does not reach a notification.
package redact
