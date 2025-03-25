package main

import (
	"fmt"
	"math/rand"
	"time"
)

const (
	numAccesses    = 1000000
	arraySizeBytes = 1024 * 1024 // 1MB
	stride         = 16
)

func measureMemoryLatency() time.Duration {

	numElements := arraySizeBytes / 4
	buffer := make([]int32, numElements)

	// Initialize buffer
	for i := 0; i < numElements; i++ {
		buffer[i] = int32(i)
	}
	// Use a random seed
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	var total time.Duration

	for i := 0; i < numAccesses; i++ {
		index := (r.Intn(numElements) * stride) % numElements

		// Start timing
		start := time.Now()

		// Read access
		value := buffer[index]
		_ = value // prevent "value is never used" wining

		// End timing
		elapsed := time.Since(start)
		total += elapsed
	}

	avg := total / numAccesses
	return avg
}

func main() {
	for {
		latency := measureMemoryLatency()
		fmt.Printf("Average Memory Latency: %v\n", latency)
		// time.Sleep(time.Second * 1)
	}
}
