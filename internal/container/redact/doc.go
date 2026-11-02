// Package redact removes credentials from Git URLs, notification service URLs
// and the error text that quotes them.
//
// Watchtower stores redacted URLs on the containers it creates and in its
// reports. Error and log text gets the same treatment, so a credential in a Git
// remote does not reach a notification. Notification service URLs are logged
// with only their scheme, since their tokens can sit anywhere in the URL.
package redact
