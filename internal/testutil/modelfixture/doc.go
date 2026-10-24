// Package modelfixture builds container models from containerfixture data for
// tests.
//
// It sits apart from containerfixture because it imports the container model.
// The model's own tests build their containers locally, since importing this
// package from them would form an import cycle.
package modelfixture
