// Package labels names the container labels that configure Watchtower and the
// label values with a fixed meaning, such as the scope of a container without
// one.
//
// The label names are part of Watchtower's user-facing surface. Users set them
// on their containers, and Watchtower sets some of them on the containers it
// creates.
package labels
