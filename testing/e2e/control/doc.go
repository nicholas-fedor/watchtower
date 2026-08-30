// Package control owns the sitting queue, the single active run, and live pool state.
//
// HTTP handlers and the CLI talk to Service. Docker work is injected so unit
// tests do not start DinD.
package control
