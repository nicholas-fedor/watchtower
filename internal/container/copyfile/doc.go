// Package copyfile carries labeled files from a container into its
// replacement.
//
// Before the source container is removed, the files its copy-file label names
// are read into memory and held until the replacement exists. They are then
// written into the replacement before it starts.
package copyfile
