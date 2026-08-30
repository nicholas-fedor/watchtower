// Package host discovers CPU, memory, and disk budgets for the DinD pool.
//
// It never writes Docker daemon configuration. RecommendedWorkers is a cap,
// not a guarantee the machine is idle.
package host
