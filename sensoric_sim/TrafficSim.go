package main

import (
	"fmt"
	"math/rand"
	"os"
	"time"
)

func main() {
	filePath := "sensoric_sim/counters.txt"

	const baselineRead = 100
	const stdDev = 2
	readCounter := 0
	writeCounter := 0

	i := 0
	for {
		// Simulate read/writes deviations.
		fluctuation := int(rand.NormFloat64() * stdDev)
		readCounter = baselineRead + fluctuation

		writeCounter = int(rand.NormFloat64() * 2)
		if writeCounter < 0 {
			writeCounter = 0
		}

		// Create content
		content := fmt.Sprintf("read/s_count: %d write/s_count: %d\n", readCounter, writeCounter)

		// Write content to file
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			fmt.Println("Error writing counters:", err)
		}
		if i%10 == 0 {
			fmt.Println("Writing to file i:", i)
		}
		i++
		time.Sleep(1 * time.Second)
	}
}
