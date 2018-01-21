package main

import (
	"fmt"
	"log"
	"bufio"
	"os"
)

type AirplaneIndex struct {
	mapping map[string]string
}

func NewAirplaneIndex() *AirplaneIndex {
	idx := AirplaneIndex { make(map[string]string) }

	inFile, err := os.Open("processed_master.csv")
	if err != nil {
		log.Fatal("couldn't open file")
	}
  	defer inFile.Close()

	scanner := bufio.NewScanner(inFile)
	skipped := false
	count := 0
	var txt string
	for scanner.Scan() {
		if !skipped {
			skipped = true
		} else {
			count++
			txt = scanner.Text()
			idx.mapping[txt[6:12]] = "N"+txt[0:5]
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "reading standard input:", err)
	}

	return &idx
}

func FindAirplaneByHex(hex string, index *AirplaneIndex) string {
	return index.mapping[hex]
}

/*
func main() {

	idx := NewAirplaneIndex()

	fmt.Printf("%v = %v\n", "A405E8", FindAirplaneByHex("A405E8", idx))	
}
*/